package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/riducms/ridu/internal/primitivefield"
	"github.com/riducms/ridu/internal/referenceindex"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Migrate atomically installs SQLite's adapter-owned tables and records the
// exact manifest digest. It is an explicit development/bootstrap operation;
// Open never calls it and production callers should apply reviewed immutable
// artifacts once the SQLite artifact runner is configured.
func (backend *Store) Migrate(ctx context.Context, manifest schema.Manifest) error {
	if err := requireSQLitePluginSchema(manifest); err != nil {
		return err
	}
	digest, err := manifestDigest(manifest)
	if err != nil {
		return fmt.Errorf("digest SQLite manifest: %w", err)
	}
	encoded, err := manifest.Bytes()
	if err != nil {
		return fmt.Errorf("encode SQLite manifest: %w", err)
	}
	contract := currentSQLitePlannerContract()
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		immutable, err := sqliteArtifactLedgerExists(ctx, connection)
		if err != nil {
			return err
		}
		if immutable {
			return fmt.Errorf("SQLite development migration cannot modify a database with immutable migration history")
		}
		before, err := readSQLiteDevelopmentManifest(ctx, connection)
		if err != nil {
			return err
		}
		pluginSteps, pluginRisks, err := sqlitePluginMigrationSteps(before, manifest)
		if err != nil {
			return err
		}
		if err := requireSQLiteDestructiveApproval(pluginRisks, false); err != nil {
			return fmt.Errorf("SQLite development migration cannot remove private plugin schema; create an immutable artifact with explicit destructive approval: %w", err)
		}
		if err := installSchema(ctx, connection); err != nil {
			return err
		}
		boundary, err := newSQLitePluginSchemaBoundary(ctx, connection, before, &manifest)
		if err != nil {
			return fmt.Errorf("prepare SQLite development plugin boundary: %w", err)
		}
		if err := executeSQLitePluginOperations(ctx, connection, pluginSteps, func() error {
			return boundary.validateIntermediate(ctx, connection)
		}); err != nil {
			return fmt.Errorf("apply SQLite development plugin migrations: %w", err)
		}
		if err := boundary.validateFinal(ctx, connection); err != nil {
			return fmt.Errorf("verify SQLite development plugin schema: %w", err)
		}
		if err := contract.reconcileIndexes(ctx, connection, manifest); err != nil {
			return err
		}
		if err := rebuildDocumentReferences(ctx, connection, manifest); err != nil {
			return err
		}
		if err := contract.rebuildUniqueness(ctx, connection, manifest); err != nil {
			return err
		}
		if err := assertSQLitePhysicalSchema(ctx, connection, manifest, false, contract); err != nil {
			return fmt.Errorf("verify SQLite development schema: %w", err)
		}
		_, err = connection.ExecContext(ctx, `INSERT INTO ridu_sqlite_schema
	  (singleton, manifest_digest, expected_artifact_digest, manifest_json, applied_at)
	VALUES (1, ?, '', ?, ?)
	ON CONFLICT(singleton) DO UPDATE SET
	  manifest_digest = excluded.manifest_digest,
	  expected_artifact_digest = excluded.expected_artifact_digest,
	  manifest_json = excluded.manifest_json,
	  applied_at = excluded.applied_at`, digest, string(encoded), encodeTime(backend.now().UTC()))
		return translateError(err)
	})
}

const documentIndexPrefix = "ridu_json_"

// reconcileDocumentIndexes fulfills the complete manifest index contract for
// every supported field declaration, collection index, and locale selection.
// The expressions are shared with the query compiler below.
func reconcileDocumentIndexes(ctx context.Context, runner sqlRunner, manifest schema.Manifest) error {
	desired, err := sqliteDocumentIndexes(manifest)
	if err != nil {
		return err
	}
	return reconcileDocumentIndexSet(ctx, runner, desired)
}

func reconcileDocumentIndexSet(ctx context.Context, runner sqlRunner, desired map[string]string) error {
	rows, err := runner.QueryContext(ctx, `SELECT name, tbl_name, sql FROM sqlite_schema
WHERE type = 'index' AND name LIKE ? ORDER BY name`, documentIndexPrefix+"%")
	if err != nil {
		return fmt.Errorf("inspect SQLite document indexes: %w", translateError(err))
	}
	type existingIndex struct {
		name      string
		table     string
		statement string
	}
	var existing []existingIndex
	for rows.Next() {
		var index existingIndex
		if err := rows.Scan(&index.name, &index.table, &index.statement); err != nil {
			rows.Close()
			return fmt.Errorf("scan SQLite document index: %w", translateError(err))
		}
		existing = append(existing, index)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("inspect SQLite document indexes: %w", translateError(err))
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("inspect SQLite document indexes: %w", translateError(err))
	}

	for _, index := range existing {
		if _, wanted := desired[index.name]; wanted && index.table != "ridu_documents" {
			return fmt.Errorf("create SQLite document index %q: name is already used by table %q", index.name, index.table)
		}
	}
	for _, index := range existing {
		if index.table != "ridu_documents" {
			continue
		}
		if statement, keep := desired[index.name]; keep {
			expected := strings.Replace(statement, "CREATE INDEX IF NOT EXISTS", "CREATE INDEX", 1)
			if normalizeSQLiteSchemaSQL(index.statement) == normalizeSQLiteSchemaSQL(expected) {
				continue
			}
		}
		if _, err := runner.ExecContext(ctx, "DROP INDEX "+quoteSQLiteIdentifier(index.name)); err != nil {
			return fmt.Errorf("drop stale SQLite document index %q: %w", index.name, translateError(err))
		}
	}
	desiredNames := make([]string, 0, len(desired))
	for name := range desired {
		desiredNames = append(desiredNames, name)
	}
	sort.Strings(desiredNames)
	for _, name := range desiredNames {
		if _, err := runner.ExecContext(ctx, desired[name]); err != nil {
			return fmt.Errorf("create SQLite document index %q: %w", name, translateError(err))
		}
	}
	return nil
}

type sqliteIndexPath struct {
	path  string
	chain []schema.Field
}

func sqliteDocumentIndexes(manifest schema.Manifest) (map[string]string, error) {
	if err := primitivefield.ValidateManifestIndexes(manifest); err != nil {
		return nil, err
	}
	snapshot := manifest.Snapshot()
	desired := make(map[string]string)
	resources := append(append([]schema.Collection(nil), snapshot.Collections...), snapshot.Globals...)
	for _, collection := range resources {
		paths := sqliteDeclaredIndexPaths(collection.Fields)
		for _, indexed := range paths {
			chains, err := sqliteIndexLocaleChains(snapshot.Application.Localization, indexed.chain)
			if err != nil {
				return nil, fmt.Errorf("build SQLite index for %s.%s: %w", collection.ID, indexed.path, err)
			}
			for _, locales := range chains {
				expression, _, _, supported := sqliteDocumentPathExpression(indexed.chain, locales)
				if !supported {
					return nil, fmt.Errorf("build SQLite index for %s.%s: unsupported manifest index path", collection.ID, indexed.path)
				}
				key := "field:" + indexed.path + ":" + sqliteLocaleChainKey(locales)
				name := documentIndexName(collection.ID, key)
				desired[name] = sqliteCreateDocumentIndex(name, []string{expression})
			}
		}

		for index, definition := range collection.Indexes {
			indexed := make([]sqliteIndexPath, len(definition.Fields))
			combined := make([]schema.Field, 0)
			for fieldIndex, path := range definition.Fields {
				chain, supported := sqliteQueryableFieldChain(collection.Fields, path.Segments())
				if !supported {
					return nil, fmt.Errorf("build SQLite compound index %s[%d]: unsupported manifest path %q", collection.ID, index, path.String())
				}
				indexed[fieldIndex] = sqliteIndexPath{path: path.String(), chain: chain}
				combined = append(combined, chain...)
			}
			chains, err := sqliteIndexLocaleChains(snapshot.Application.Localization, combined)
			if err != nil {
				return nil, fmt.Errorf("build SQLite compound index %s[%d]: %w", collection.ID, index, err)
			}
			for _, locales := range chains {
				expressions := make([]string, len(indexed))
				parts := make([]string, len(indexed))
				for fieldIndex, item := range indexed {
					expression, _, _, supported := sqliteDocumentPathExpression(item.chain, locales)
					if !supported {
						return nil, fmt.Errorf("build SQLite compound index %s[%d]: unsupported manifest path %q", collection.ID, index, item.path)
					}
					expressions[fieldIndex] = expression
					parts[fieldIndex] = item.path
				}
				key := "compound:" + strings.Join(parts, "\x00") + ":" + sqliteLocaleChainKey(locales)
				name := documentIndexName(collection.ID, key)
				desired[name] = sqliteCreateDocumentIndex(name, expressions)
			}
		}
	}
	return desired, nil
}

func sqliteDeclaredIndexPaths(fields []schema.Field) []sqliteIndexPath {
	var result []sqliteIndexPath
	var visit func([]schema.Field, []string)
	visit = func(candidates []schema.Field, prefix []string) {
		for _, candidate := range candidates {
			segments := append(append([]string(nil), prefix...), candidate.Name)
			if candidate.Index || candidate.Unique {
				if chain, supported := sqliteQueryableFieldChain(fields, segments); supported {
					result = append(result, sqliteIndexPath{path: strings.Join(segments, "."), chain: chain})
				}
			}
			if candidate.Type == schema.FieldTypeGroup && candidate.Nested != nil {
				visit(candidate.Nested.ResolvedFields(), segments)
			}
		}
	}
	visit(fields, nil)
	return result
}

func sqliteCreateDocumentIndex(name string, expressions []string) string {
	keys := append([]string{"collection_id"}, expressions...)
	keys = append(keys, "id COLLATE BINARY")
	return fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON ridu_documents (%s)", quoteSQLiteIdentifier(name), strings.Join(keys, ", "))
}

func documentIndexName(collectionID schema.StableID, declaration string) string {
	digest := sha256.Sum256([]byte("document-index:" + string(collectionID) + ":" + declaration))
	return documentIndexPrefix + hex.EncodeToString(digest[:8])
}

func documentFieldIndexName(collectionID schema.StableID, path string, locales []schema.LocaleCode) string {
	return documentIndexName(collectionID, "field:"+path+":"+sqliteLocaleChainKey(locales))
}

func documentCompoundIndexName(collectionID schema.StableID, paths []string, locales []schema.LocaleCode) string {
	return documentIndexName(collectionID, "compound:"+strings.Join(paths, "\x00")+":"+sqliteLocaleChainKey(locales))
}

func sqliteLocaleChainKey(locales []schema.LocaleCode) string {
	parts := make([]string, len(locales))
	for index, locale := range locales {
		parts[index] = string(locale)
	}
	return strings.Join(parts, "\x00")
}

func sqliteIndexLocaleChains(settings *schema.LocalizationSettings, fields []schema.Field) ([][]schema.LocaleCode, error) {
	localized := false
	for _, candidate := range fields {
		localized = localized || candidate.Localized
	}
	if !localized {
		return [][]schema.LocaleCode{nil}, nil
	}
	if settings == nil || len(settings.Locales) == 0 {
		return nil, fmt.Errorf("localized index requires application localization")
	}
	seen := make(map[string]struct{})
	var result [][]schema.LocaleCode
	appendChain := func(chain []schema.LocaleCode) {
		key := sqliteLocaleChainKey(chain)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		result = append(result, append([]schema.LocaleCode(nil), chain...))
	}
	for _, locale := range settings.Locales {
		appendChain([]schema.LocaleCode{locale.Code})
		chain := []schema.LocaleCode{locale.Code}
		for _, fallback := range locale.FallbackLocales {
			chain = appendSQLiteUniqueLocale(chain, fallback)
		}
		if settings.Fallback {
			chain = appendSQLiteUniqueLocale(chain, settings.DefaultLocale)
		}
		appendChain(chain)
	}
	return result, nil
}

func appendSQLiteUniqueLocale(locales []schema.LocaleCode, locale schema.LocaleCode) []schema.LocaleCode {
	for _, existing := range locales {
		if existing == locale {
			return locales
		}
	}
	return append(locales, locale)
}

func sqliteQueryableFieldChain(fields []schema.Field, segments []string) ([]schema.Field, bool) {
	if len(segments) == 0 {
		return nil, false
	}
	chain := make([]schema.Field, 0, len(segments))
	candidates := fields
	for index, segment := range segments {
		var current *schema.Field
		for candidateIndex := range candidates {
			if candidates[candidateIndex].Name == segment {
				current = &candidates[candidateIndex]
				break
			}
		}
		if current == nil {
			return nil, false
		}
		chain = append(chain, *current)
		if index == len(segments)-1 {
			_, supported := sqliteFieldValueKind(*current)
			return chain, supported
		}
		if current.Type != schema.FieldTypeGroup || current.Nested == nil {
			return nil, false
		}
		candidates = current.Nested.ResolvedFields()
	}
	return nil, false
}

func sqliteFieldValueKind(field schema.Field) (sqliteDocumentValueKind, bool) {
	switch field.Type {
	case schema.FieldTypeText, schema.FieldTypeTextarea, schema.FieldTypeCode,
		schema.FieldTypeEmail, schema.FieldTypeDate, schema.FieldTypeRadio:
		return sqliteStringValue, field.Category == schema.FieldCategoryScalar || field.Category == schema.FieldCategoryUpload
	case schema.FieldTypeSelect:
		return sqliteStringValue, field.Category == schema.FieldCategoryScalar && (field.Select == nil || !field.Select.HasMany)
	case schema.FieldTypeNumber:
		return sqliteNumberValue, field.Category == schema.FieldCategoryScalar || field.Category == schema.FieldCategoryUpload
	case schema.FieldTypeCheckbox:
		return sqliteBooleanValue, field.Category == schema.FieldCategoryScalar
	case schema.FieldTypeRelationship:
		return sqliteStringValue, field.Category == schema.FieldCategoryRelationship && field.Relationship != nil && !field.Relationship.HasMany && !field.Relationship.Polymorphic
	case schema.FieldTypeUpload:
		return sqliteStringValue, field.Category == schema.FieldCategoryUpload && field.Upload != nil && !field.Upload.HasMany
	default:
		return 0, false
	}
}

// sqliteDocumentPathExpression resolves one non-repeated manifest path using
// the same locale projection rules as the operation engine. It returns a
// scalar value expression and its underlying JSON type expression. Callers
// must supply the effective request locale chain for localized paths.
func sqliteDocumentPathExpression(chain []schema.Field, locales []schema.LocaleCode) (string, string, sqliteDocumentValueKind, bool) {
	if len(chain) == 0 {
		return "", "", 0, false
	}
	kind, supported := sqliteFieldValueKind(chain[len(chain)-1])
	if !supported {
		return "", "", 0, false
	}
	base := "values_json"
	pending := make([]string, 0, len(chain))
	for index, candidate := range chain {
		pending = append(pending, candidate.Name)
		if !candidate.Localized {
			continue
		}
		if len(locales) == 0 {
			return "", "", 0, false
		}
		terminal := index == len(chain)-1
		values := make([]string, len(locales))
		types := make([]string, len(locales))
		conditions := make([]string, len(locales))
		for localeIndex, locale := range locales {
			segments := append(append([]string(nil), pending...), string(locale))
			values[localeIndex] = sqliteJSONExtractFrom(base, segments...)
			types[localeIndex] = sqliteJSONTypeFrom(base, segments...)
			conditions[localeIndex] = values[localeIndex] + " IS NOT NULL"
			if terminal && kind == sqliteStringValue && localeIndex < len(locales)-1 {
				conditions[localeIndex] += " AND " + values[localeIndex] + " <> ''"
			}
		}
		base = sqliteLocaleCase(values, conditions)
		if terminal {
			value := sqliteCollatedDocumentValue(base, kind)
			return value, sqliteLocaleCase(types, conditions), kind, true
		}
		pending = pending[:0]
	}
	value := sqliteJSONExtractFrom(base, pending...)
	typeExpression := sqliteJSONTypeFrom(base, pending...)
	return sqliteCollatedDocumentValue(value, kind), typeExpression, kind, true
}

func sqliteLocaleCase(values, conditions []string) string {
	if len(values) == 1 {
		return values[0]
	}
	var expression strings.Builder
	expression.WriteString("CASE")
	for index := 0; index < len(values)-1; index++ {
		expression.WriteString(" WHEN ")
		expression.WriteString(conditions[index])
		expression.WriteString(" THEN ")
		expression.WriteString(values[index])
	}
	expression.WriteString(" ELSE ")
	expression.WriteString(values[len(values)-1])
	expression.WriteString(" END")
	return expression.String()
}

func sqliteCollatedDocumentValue(value string, kind sqliteDocumentValueKind) string {
	switch kind {
	case sqliteStringValue:
		return "(" + value + ") COLLATE BINARY"
	case sqliteNumberValue:
		return "CAST(" + value + " AS REAL)"
	default:
		return value
	}
}

func sqliteJSONExtractFrom(base string, segments ...string) string {
	return "json_extract(" + base + ", " + quoteSQLiteLiteral(sqliteJSONPath(segments...)) + ")"
}

func sqliteJSONTypeFrom(base string, segments ...string) string {
	return "json_type(" + base + ", " + quoteSQLiteLiteral(sqliteJSONPath(segments...)) + ")"
}

func sqliteJSONExtract(segments ...string) string {
	return "json_extract(values_json, " + quoteSQLiteLiteral(sqliteJSONPath(segments...)) + ") COLLATE BINARY"
}

func sqliteJSONReal(segments ...string) string {
	return "CAST(" + sqliteJSONExtract(segments...) + " AS REAL)"
}

func sqliteJSONType(segments ...string) string {
	return "json_type(values_json, " + quoteSQLiteLiteral(sqliteJSONPath(segments...)) + ")"
}

func sqliteJSONPath(segments ...string) string {
	path := "$"
	for _, segment := range segments {
		encoded, _ := json.Marshal(segment)
		path += "." + string(encoded)
	}
	return path
}

func quoteSQLiteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quoteSQLiteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func manifestDigest(manifest schema.Manifest) (string, error) {
	encoded, err := manifest.Bytes()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func requireSQLitePluginSchema(manifest schema.Manifest) error {
	for _, plugin := range manifest.Snapshot().Plugins {
		if !plugin.HasDatabaseContributions() {
			continue
		}
		contribution, supported := plugin.DatabaseContribution(schema.PluginDatabaseAdapterSQLite)
		if !supported {
			return fmt.Errorf("plugin %q has private database schema but does not support sqlite; contribute ordinary collections for portable plugin data or add a sqlite database contribution", plugin.Key)
		}
		for _, pluginMigration := range contribution.Migrations {
			for _, statement := range append(append([]string(nil), pluginMigration.UpSQL...), pluginMigration.DownSQL...) {
				if !schema.IsValidPluginMigrationSQL(schema.PluginDatabaseAdapterSQLite, statement) {
					return fmt.Errorf("plugin %q sqlite migration %d contains invalid SQL or transaction/connection control", plugin.Key, pluginMigration.Version)
				}
			}
		}
	}
	return nil
}

func readSQLiteDevelopmentManifest(ctx context.Context, runner sqlRunner) (*schema.Manifest, error) {
	var tableCount int
	if err := runner.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'ridu_sqlite_schema'`).Scan(&tableCount); err != nil {
		return nil, translateError(err)
	}
	if tableCount == 0 {
		return nil, nil
	}
	var encoded string
	err := runner.QueryRowContext(ctx, `SELECT manifest_json FROM ridu_sqlite_schema WHERE singleton = 1`).Scan(&encoded)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, translateError(err)
	}
	manifest, err := schema.Parse([]byte(encoded))
	if err != nil {
		return nil, fmt.Errorf("decode previous SQLite development manifest: %w", err)
	}
	return &manifest, nil
}

func installSchema(ctx context.Context, runner sqlRunner) error {
	for _, statement := range currentSQLitePlannerContract().freshSchemaStatements {
		if _, err := runner.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("install SQLite schema: %w", translateError(err))
		}
	}
	return nil
}

var sqliteSchemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS ridu_sqlite_schema (
  singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
  manifest_digest TEXT NOT NULL,
  expected_artifact_digest TEXT NOT NULL,
  manifest_json TEXT NOT NULL CHECK (json_valid(manifest_json)),
  applied_at INTEGER NOT NULL
) STRICT`,
	`CREATE TABLE IF NOT EXISTS ridu_documents (
  collection_id TEXT NOT NULL,
  id TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  deleted_at INTEGER,
  status TEXT NOT NULL DEFAULT '',
  revision INTEGER NOT NULL DEFAULT 0,
  values_json TEXT NOT NULL CHECK (json_valid(values_json)),
  PRIMARY KEY (collection_id, id)
) STRICT`,
	`CREATE INDEX IF NOT EXISTS ridu_documents_active
  ON ridu_documents (collection_id, deleted_at, id)`,
	`CREATE INDEX IF NOT EXISTS ridu_documents_status
  ON ridu_documents (collection_id, status, deleted_at, id)`,
	`CREATE TABLE IF NOT EXISTS ridu_unique_values (
  collection_id TEXT NOT NULL,
  index_key TEXT NOT NULL,
  value_key TEXT NOT NULL,
  document_id TEXT NOT NULL,
  PRIMARY KEY (collection_id, index_key, value_key),
  FOREIGN KEY (collection_id, document_id)
    REFERENCES ridu_documents (collection_id, id) ON DELETE CASCADE
) STRICT`,
	`CREATE INDEX IF NOT EXISTS ridu_unique_values_document
  ON ridu_unique_values (collection_id, document_id)`,
	`CREATE TABLE IF NOT EXISTS ridu_document_references (
  owner_collection_id TEXT NOT NULL,
  owner_document_id TEXT NOT NULL,
  field_id TEXT NOT NULL,
  target_collection_id TEXT NOT NULL,
  target_document_id TEXT NOT NULL,
  locale TEXT NOT NULL DEFAULT '',
  occurrence INTEGER NOT NULL,
  PRIMARY KEY (
    owner_collection_id, owner_document_id, field_id,
    target_collection_id, target_document_id, locale, occurrence
  ),
  FOREIGN KEY (owner_collection_id, owner_document_id)
    REFERENCES ridu_documents (collection_id, id) ON DELETE CASCADE,
  FOREIGN KEY (target_collection_id, target_document_id)
    REFERENCES ridu_documents (collection_id, id) ON DELETE RESTRICT
) STRICT`,
	`CREATE INDEX IF NOT EXISTS ridu_document_references_target
  ON ridu_document_references (target_collection_id, target_document_id)`,
	`CREATE TABLE IF NOT EXISTS ridu_versions (
	  id TEXT NOT NULL,
	  collection_id TEXT NOT NULL,
	  document_id TEXT NOT NULL,
	  revision INTEGER NOT NULL,
	  status TEXT NOT NULL,
	  snapshot_json TEXT NOT NULL CHECK (json_valid(snapshot_json)),
	  created_at INTEGER NOT NULL,
	  PRIMARY KEY (collection_id, document_id, revision)
	) STRICT`,
	`CREATE INDEX IF NOT EXISTS ridu_versions_document
  ON ridu_versions (collection_id, document_id, revision DESC)`,
	`CREATE TABLE IF NOT EXISTS ridu_auth_credentials (
  collection_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  password_hash BLOB NOT NULL,
  failed_login_attempts INTEGER NOT NULL DEFAULT 0,
  locked_until INTEGER,
  verified INTEGER NOT NULL DEFAULT 0 CHECK (verified IN (0, 1)),
  PRIMARY KEY (collection_id, user_id),
  FOREIGN KEY (collection_id, user_id)
    REFERENCES ridu_documents (collection_id, id) ON DELETE CASCADE
) STRICT`,
	`CREATE TABLE IF NOT EXISTS ridu_auth_sessions (
  token_hash TEXT PRIMARY KEY,
  id TEXT NOT NULL UNIQUE,
  collection_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  ip_address TEXT NOT NULL DEFAULT '',
  user_agent TEXT NOT NULL DEFAULT '',
  FOREIGN KEY (collection_id, user_id)
    REFERENCES ridu_auth_credentials (collection_id, user_id) ON DELETE CASCADE
) STRICT`,
	`CREATE INDEX IF NOT EXISTS ridu_auth_sessions_user
  ON ridu_auth_sessions (collection_id, user_id, expires_at)`,
	`CREATE INDEX IF NOT EXISTS ridu_auth_sessions_expiry_idx
  ON ridu_auth_sessions (expires_at, token_hash)`,
	`CREATE TABLE IF NOT EXISTS ridu_auth_tokens (
  token_hash TEXT PRIMARY KEY,
  purpose TEXT NOT NULL,
  collection_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  FOREIGN KEY (collection_id, user_id)
    REFERENCES ridu_auth_credentials (collection_id, user_id) ON DELETE CASCADE
) STRICT`,
	`CREATE TABLE IF NOT EXISTS ridu_auth_api_keys (
  id TEXT PRIMARY KEY,
  token_hash TEXT NOT NULL UNIQUE,
  collection_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  name TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  last_used_at INTEGER,
  expires_at INTEGER,
  FOREIGN KEY (collection_id, user_id)
    REFERENCES ridu_auth_credentials (collection_id, user_id) ON DELETE CASCADE
) STRICT`,
	`CREATE INDEX IF NOT EXISTS ridu_auth_api_keys_user
  ON ridu_auth_api_keys (collection_id, user_id, created_at)`,
	`CREATE INDEX IF NOT EXISTS ridu_auth_api_keys_expiry_idx
  ON ridu_auth_api_keys (expires_at, id) WHERE expires_at IS NOT NULL`,
	`CREATE TABLE IF NOT EXISTS ridu_auth_rate_limits (
  key_hash TEXT PRIMARY KEY,
  attempts INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
) STRICT`,
	`CREATE INDEX IF NOT EXISTS ridu_auth_rate_limits_expiry_idx
  ON ridu_auth_rate_limits (expires_at, key_hash)`,
	`CREATE TABLE IF NOT EXISTS ridu_preferences (
  collection_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  key TEXT NOT NULL,
  value_json TEXT NOT NULL CHECK (json_valid(value_json)),
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (collection_id, user_id, key),
  FOREIGN KEY (collection_id, user_id)
    REFERENCES ridu_documents (collection_id, id) ON DELETE CASCADE
) STRICT`,
	`CREATE TABLE IF NOT EXISTS ridu_document_locks (
  collection_id TEXT NOT NULL,
  document_id TEXT NOT NULL,
  owner_collection_id TEXT NOT NULL,
  owner_id TEXT NOT NULL,
  owner_label TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  PRIMARY KEY (collection_id, document_id),
  FOREIGN KEY (collection_id, document_id)
    REFERENCES ridu_documents (collection_id, id) ON DELETE CASCADE,
  FOREIGN KEY (owner_collection_id, owner_id)
    REFERENCES ridu_documents (collection_id, id) ON DELETE CASCADE
) STRICT`,
	`CREATE TABLE IF NOT EXISTS ridu_tasks (
  id TEXT PRIMARY KEY,
  slug TEXT NOT NULL,
  queue TEXT NOT NULL,
  concurrency_key TEXT NOT NULL DEFAULT '',
  input_json TEXT NOT NULL CHECK (json_valid(input_json)),
  output_json TEXT CHECK (output_json IS NULL OR json_valid(output_json)),
  state TEXT NOT NULL,
  run_at INTEGER NOT NULL,
  attempts INTEGER NOT NULL,
  max_attempts INTEGER NOT NULL,
  retry_delay_ns INTEGER NOT NULL,
  max_retry_delay_ns INTEGER NOT NULL,
  backoff TEXT NOT NULL,
  timeout_ns INTEGER NOT NULL,
  retention_ns INTEGER NOT NULL,
  lease_token TEXT NOT NULL DEFAULT '',
  lease_expires_at INTEGER,
  target_collection_id TEXT,
  target_document_id TEXT,
  requested_by_collection_id TEXT,
  requested_by_document_id TEXT,
  last_error_code TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  completed_at INTEGER,
  retain_until INTEGER,
  CHECK ((target_collection_id IS NULL) = (target_document_id IS NULL)),
  CHECK ((requested_by_collection_id IS NULL) = (requested_by_document_id IS NULL)),
  FOREIGN KEY (target_collection_id, target_document_id)
    REFERENCES ridu_documents (collection_id, id) ON DELETE CASCADE,
  FOREIGN KEY (requested_by_collection_id, requested_by_document_id)
    REFERENCES ridu_documents (collection_id, id) ON DELETE CASCADE
) STRICT`,
	`CREATE INDEX IF NOT EXISTS ridu_tasks_due
  ON ridu_tasks (state, run_at, queue, id)`,
	`CREATE INDEX IF NOT EXISTS ridu_tasks_active_concurrency
  ON ridu_tasks (queue, concurrency_key, state, lease_expires_at)`,
	`CREATE INDEX IF NOT EXISTS ridu_tasks_target
  ON ridu_tasks (target_collection_id, target_document_id, slug, created_at)`,
	`CREATE INDEX IF NOT EXISTS ridu_tasks_retention
  ON ridu_tasks (retain_until, id)`,
}

func encodeTime(value time.Time) int64 { return value.UTC().UnixNano() }

func decodeTime(encoded int64) time.Time { return time.Unix(0, encoded).UTC() }

func encodeOptionalTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return encodeTime(value)
}

func decodeOptionalTime(encoded sql.NullInt64) time.Time {
	if !encoded.Valid {
		return time.Time{}
	}
	return decodeTime(encoded.Int64)
}

func timePointer(encoded sql.NullInt64) *time.Time {
	if !encoded.Valid {
		return nil
	}
	value := decodeTime(encoded.Int64)
	return &value
}

func rebuildUniqueValues(ctx context.Context, runner sqlRunner, manifest schema.Manifest) error {
	return rebuildUniqueValuesWithAuthKey(ctx, runner, manifest, store.CanonicalAuthIdentity)
}

// rebuildLegacyAuthUniqueValues freezes planner v1's strings.EqualFold-style
// SimpleFold key. Pending immutable v1 artifacts must replay their published
// uniqueness semantics; only the v1.1 transition may adopt the canonical key.
func rebuildLegacyAuthUniqueValues(ctx context.Context, runner sqlRunner, manifest schema.Manifest) error {
	return rebuildUniqueValuesWithAuthKey(ctx, runner, manifest, legacyAuthIdentityKey)
}

func rebuildUniqueValuesWithAuthKey(ctx context.Context, runner sqlRunner, manifest schema.Manifest, authIdentityKey func(string) string) error {
	if _, err := runner.ExecContext(ctx, `DELETE FROM ridu_unique_values`); err != nil {
		return translateError(err)
	}
	resources := append(append([]schema.Collection(nil), manifest.Snapshot().Collections...), manifest.Snapshot().Globals...)
	for _, collection := range resources {
		rows, err := runner.QueryContext(ctx, `SELECT id, values_json FROM ridu_documents
WHERE collection_id = ? AND deleted_at IS NULL`, string(collection.ID))
		if err != nil {
			return translateError(err)
		}
		for rows.Next() {
			var id, encoded string
			if err := rows.Scan(&id, &encoded); err != nil {
				rows.Close()
				return err
			}
			var values map[string]json.RawMessage
			if err := json.Unmarshal([]byte(encoded), &values); err != nil {
				rows.Close()
				return fmt.Errorf("decode stored values for uniqueness: %w", err)
			}
			if err := insertRawUniqueValuesWithAuthKey(ctx, runner, collection, id, values, authIdentityKey); err != nil {
				rows.Close()
				return err
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}
	return nil
}

func rebuildDocumentReferences(ctx context.Context, runner sqlRunner, manifest schema.Manifest) error {
	if _, err := runner.ExecContext(ctx, `DELETE FROM ridu_document_references`); err != nil {
		return translateError(err)
	}
	resources := append(append([]schema.Collection(nil), manifest.Snapshot().Collections...), manifest.Snapshot().Globals...)
	for _, collection := range resources {
		rows, err := runner.QueryContext(ctx, `SELECT id, values_json FROM ridu_documents WHERE collection_id = ?`, string(collection.ID))
		if err != nil {
			return translateError(err)
		}
		var documents []store.Document
		for rows.Next() {
			var document store.Document
			var encoded string
			if err := rows.Scan(&document.ID, &encoded); err != nil {
				rows.Close()
				return translateError(err)
			}
			if err := json.Unmarshal([]byte(encoded), &document.Values); err != nil {
				rows.Close()
				return fmt.Errorf("decode stored values for references: %w", err)
			}
			documents = append(documents, document)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return translateError(err)
		}
		rows.Close()
		for _, document := range documents {
			entries, referenceErr := referenceindex.Collect(collection, document)
			if referenceErr != nil {
				return referenceErr
			}
			for _, entry := range entries {
				_, err := runner.ExecContext(ctx, `INSERT INTO ridu_document_references (
  owner_collection_id, owner_document_id, field_id,
  target_collection_id, target_document_id, locale, occurrence
) VALUES (?, ?, ?, ?, ?, ?, ?)`,
					string(entry.Owner.CollectionID), entry.Owner.DocumentID, string(entry.FieldID),
					string(entry.Target.CollectionID), entry.Target.DocumentID, string(entry.Locale), entry.Occurrence,
				)
				if err != nil {
					return fmt.Errorf("rebuild SQLite reference %s/%s field %s: %w", entry.Owner.CollectionID, entry.Owner.DocumentID, entry.FieldID, translateError(err))
				}
			}
		}
	}
	return nil
}

func insertRawUniqueValues(ctx context.Context, runner sqlRunner, collection schema.Collection, documentID string, values map[string]json.RawMessage) error {
	return insertRawUniqueValuesWithAuthKey(ctx, runner, collection, documentID, values, store.CanonicalAuthIdentity)
}

func insertRawUniqueValuesWithAuthKey(ctx context.Context, runner sqlRunner, collection schema.Collection, documentID string, values map[string]json.RawMessage, authIdentityKey func(string) string) error {
	for _, field := range collection.Fields {
		if !field.Unique {
			continue
		}
		raw, exists := values[field.Name]
		if !exists || strings.TrimSpace(string(raw)) == "null" {
			continue
		}
		if field.Localized {
			var locales map[string]json.RawMessage
			if err := json.Unmarshal(raw, &locales); err != nil {
				return fmt.Errorf("decode localized unique field %q: %w", field.Path.String(), err)
			}
			for locale, localized := range locales {
				if strings.TrimSpace(string(localized)) == "null" {
					continue
				}
				if err := insertUniqueValue(ctx, runner, collection.ID, "field:"+string(field.ID)+":"+locale, canonicalJSON(localized), documentID); err != nil {
					return err
				}
			}
			continue
		}
		valueKey := canonicalJSON(raw)
		if collection.Auth != nil && field.Name == collection.Auth.IdentityField {
			var identity string
			if err := json.Unmarshal(raw, &identity); err != nil {
				return fmt.Errorf("decode auth identity field %q: %w", field.Path.String(), err)
			}
			normalized, _ := json.Marshal(authIdentityKey(identity))
			valueKey = string(normalized)
		}
		if err := insertUniqueValue(ctx, runner, collection.ID, "field:"+string(field.ID), valueKey, documentID); err != nil {
			return err
		}
	}
	var documentValues store.Values
	for index, definition := range collection.Indexes {
		if !definition.Unique {
			continue
		}
		if documentValues == nil {
			encodedValues, err := json.Marshal(values)
			if err != nil {
				return fmt.Errorf("encode values for compound uniqueness: %w", err)
			}
			if err := json.Unmarshal(encodedValues, &documentValues); err != nil {
				return fmt.Errorf("decode values for compound uniqueness: %w", err)
			}
		}
		chains := make([][]schema.Field, len(definition.Fields))
		localized := false
		locales := make(map[string]struct{})
		for fieldIndex, path := range definition.Fields {
			chains[fieldIndex] = sqliteIndexedFieldChain(collection.Fields, path.Segments())
			if len(chains[fieldIndex]) != len(path.Segments()) {
				return fmt.Errorf("resolve compound unique field %q", path.String())
			}
			localized = localized || sqliteIndexedChainLocalized(chains[fieldIndex])
			sqliteCollectIndexedLocales(documentValues, chains[fieldIndex], 0, locales)
		}
		if !localized {
			locales[""] = struct{}{}
		}
		orderedLocales := make([]string, 0, len(locales))
		for locale := range locales {
			orderedLocales = append(orderedLocales, locale)
		}
		sort.Strings(orderedLocales)
		for _, locale := range orderedLocales {
			tuple, complete := sqliteIndexedTuple(documentValues, chains, locale)
			if !complete {
				continue
			}
			encoded, err := json.Marshal(tuple)
			if err != nil {
				return fmt.Errorf("encode compound unique index %d: %w", index, err)
			}
			indexKey := fmt.Sprintf("compound:%d", index)
			if localized {
				indexKey += ":" + locale
			}
			if err := insertUniqueValue(ctx, runner, collection.ID, indexKey, canonicalJSON(encoded), documentID); err != nil {
				return err
			}
		}
	}
	return nil
}

func legacyAuthIdentityKey(value string) string {
	var folded strings.Builder
	folded.Grow(len(value))
	for _, current := range value {
		canonical := current
		for candidate := unicode.SimpleFold(current); candidate != current; candidate = unicode.SimpleFold(candidate) {
			if candidate < canonical {
				canonical = candidate
			}
		}
		folded.WriteRune(canonical)
	}
	return folded.String()
}

func sqliteIndexedFieldChain(fields []schema.Field, segments []string) []schema.Field {
	if len(segments) == 0 {
		return nil
	}
	for _, candidate := range fields {
		if candidate.Name != segments[0] {
			continue
		}
		chain := []schema.Field{candidate}
		if len(segments) == 1 {
			return chain
		}
		if candidate.Nested == nil {
			return nil
		}
		return append(chain, sqliteIndexedFieldChain(candidate.Nested.ResolvedFields(), segments[1:])...)
	}
	return nil
}

func sqliteIndexedChainLocalized(chain []schema.Field) bool {
	for _, candidate := range chain {
		if candidate.Localized {
			return true
		}
	}
	return false
}

func sqliteCollectIndexedLocales(values store.Values, chain []schema.Field, position int, locales map[string]struct{}) {
	if position >= len(chain) {
		return
	}
	value, exists := values[chain[position].Name]
	if !exists || value.Kind() == store.ValueNull {
		return
	}
	if chain[position].Localized {
		localized, valid := value.CopyObject()
		if !valid {
			return
		}
		for locale, localizedValue := range localized {
			locales[locale] = struct{}{}
			if position+1 < len(chain) {
				object, valid := localizedValue.CopyObject()
				if valid {
					sqliteCollectIndexedLocales(object, chain, position+1, locales)
				}
			}
		}
		return
	}
	if position+1 < len(chain) {
		object, valid := value.CopyObject()
		if valid {
			sqliteCollectIndexedLocales(object, chain, position+1, locales)
		}
	}
}

func sqliteIndexedTuple(values store.Values, chains [][]schema.Field, locale string) ([]store.Value, bool) {
	tuple := make([]store.Value, len(chains))
	for index, chain := range chains {
		value, exists := sqliteIndexedValue(values, chain, locale)
		if !exists || value.Kind() == store.ValueNull {
			return nil, false
		}
		tuple[index] = value
	}
	return tuple, true
}

func sqliteIndexedValue(values store.Values, chain []schema.Field, locale string) (store.Value, bool) {
	for position, candidate := range chain {
		value, exists := values[candidate.Name]
		if !exists {
			return store.Value{}, false
		}
		if candidate.Localized {
			localized, valid := value.CopyObject()
			if !valid {
				return store.Value{}, false
			}
			value, exists = localized[locale]
			if !exists {
				return store.Value{}, false
			}
		}
		if position == len(chain)-1 {
			return value, true
		}
		object, valid := value.CopyObject()
		if !valid {
			return store.Value{}, false
		}
		values = object
	}
	return store.Value{}, false
}

func insertUniqueValue(ctx context.Context, runner sqlRunner, collectionID schema.StableID, indexKey, valueKey, documentID string) error {
	_, err := runner.ExecContext(ctx, `INSERT INTO ridu_unique_values
  (collection_id, index_key, value_key, document_id) VALUES (?, ?, ?, ?)`, string(collectionID), indexKey, valueKey, documentID)
	return translateError(err)
}

func canonicalJSON(raw []byte) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return string(raw)
	}
	value = normalizeUniqueJSONNumbers(value)
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func normalizeUniqueJSONNumbers(value any) any {
	switch typed := value.(type) {
	case float64:
		if typed == 0 {
			return float64(0)
		}
	case []any:
		for index, item := range typed {
			typed[index] = normalizeUniqueJSONNumbers(item)
		}
	case map[string]any:
		for key, item := range typed {
			typed[key] = normalizeUniqueJSONNumbers(item)
		}
	}
	return value
}

func rawValueAt(values map[string]json.RawMessage, segments []string) (json.RawMessage, bool) {
	if len(segments) == 0 {
		return nil, false
	}
	raw, exists := values[segments[0]]
	for _, segment := range segments[1:] {
		if !exists {
			return nil, false
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil {
			return nil, false
		}
		raw, exists = object[segment]
	}
	return raw, exists
}
