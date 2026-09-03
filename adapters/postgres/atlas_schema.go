package postgres

import (
	"fmt"
	"strconv"
	"strings"

	atlaspostgres "ariga.io/atlas/sql/postgres"
	atlasschema "ariga.io/atlas/sql/schema"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const atlasVersionV1 = "1.0.0"

// AtlasVersion is Ridu's embedded PostgreSQL planning contract version. It is
// persisted in every migration artifact so semantic planner upgrades are
// explicit and reviewable.
const AtlasVersion = "1.1.0"

type atlasPlannerContract struct {
	version               string
	canonicalAuthIdentity bool
}

func currentAtlasPlannerContract() atlasPlannerContract {
	return atlasPlannerContract{version: AtlasVersion, canonicalAuthIdentity: true}
}

func atlasPlannerContractFor(version string) (atlasPlannerContract, bool) {
	switch version {
	case atlasVersionV1:
		return atlasPlannerContract{version: atlasVersionV1}, true
	case AtlasVersion:
		return currentAtlasPlannerContract(), true
	default:
		return atlasPlannerContract{}, false
	}
}

func atlasPlanner() ridumigration.Planner {
	return atlasPlannerForContract(currentAtlasPlannerContract())
}

func atlasPlannerForContract(contract atlasPlannerContract) ridumigration.Planner {
	return ridumigration.Planner{Name: "atlas", Version: contract.version}
}

type atlasIdentityMap struct {
	collections map[schema.StableID]schema.StableID
	fields      map[string]schema.StableID
}

func emptyAtlasSchema() *atlasschema.Schema {
	return atlasschema.New("public")
}

func (mapping atlasIdentityMap) collection(id schema.StableID) schema.StableID {
	if mapped := mapping.collections[id]; mapped != "" {
		return mapped
	}
	return id
}

func (mapping atlasIdentityMap) field(collectionID, fieldID schema.StableID) schema.StableID {
	if mapped := mapping.fields[statementFieldKey(collectionID, fieldID)]; mapped != "" {
		return mapped
	}
	return fieldID
}

func atlasSchema(manifest schema.Manifest, mapping atlasIdentityMap) *atlasschema.Schema {
	return atlasSchemaForContract(manifest, mapping, currentAtlasPlannerContract())
}

func atlasSchemaForContract(manifest schema.Manifest, mapping atlasIdentityMap, contract atlasPlannerContract) *atlasschema.Schema {
	physical := atlasschema.New("public")
	snapshot := manifest.Snapshot()
	var locales []schema.LocaleCode
	if snapshot.Application.Localization != nil {
		locales = append([]schema.LocaleCode(nil), snapshot.Application.Localization.LocaleCodes()...)
	}
	resources := append(append([]schema.Collection(nil), snapshot.Collections...), snapshot.Globals...)
	collections := make(map[schema.StableID]*atlasschema.Table, len(resources))
	for _, collection := range resources {
		id := mapping.collection(collection.ID)
		table := collectionAtlasTable(collection, id, mapping, locales, true, contract.canonicalAuthIdentity)
		collections[collection.ID] = table
		physical.AddTables(table)
	}
	for _, collection := range resources {
		table := collections[collection.ID]
		for _, field := range collection.Fields {
			if !hasForeignKey(field) {
				continue
			}
			var target schema.StableID
			if field.Upload != nil {
				target = field.Upload.CollectionID
			} else {
				target = field.Relationship.CollectionID
			}
			refTable := collections[target]
			if refTable == nil {
				continue
			}
			fieldID := mapping.field(collection.ID, field.ID)
			columnNames := []string{fieldColumn(fieldID)}
			if field.Localized {
				columnNames = columnNames[:0]
				for _, locale := range locales {
					columnNames = append(columnNames, localizedFieldColumn(fieldID, locale))
				}
			}
			refColumn, _ := refTable.Column("id")
			for index, columnName := range columnNames {
				column, _ := table.Column(columnName)
				constraintKey := string(mapping.collection(collection.ID)) + ":" + string(fieldID)
				if field.Localized {
					constraintKey += ":" + string(locales[index])
				}
				constraint := "z_fk_" + identifierHash(constraintKey)
				table.AddForeignKeys(atlasschema.NewForeignKey(constraint).SetTable(table).AddColumns(column).SetRefTable(refTable).AddRefColumns(refColumn))
			}
		}
	}
	if hasAuthCollections(snapshot.Collections) {
		physical.AddTables(authCredentialsAtlasTable(), authSessionsAtlasTable(true), authTokensAtlasTable(), authAPIKeysAtlasTable(true), authRateLimitsAtlasTable(), preferencesAtlasTable())
	}
	if hasVersionCollections(resources) {
		physical.AddTables(versionsAtlasTable(true))
	}
	if hasDocumentLockCollections(snapshot.Collections) {
		physical.AddTables(documentLocksAtlasTable(true))
	}
	physical.AddTables(documentReferencesAtlasTable(), tasksAtlasTable())
	return physical
}

func documentReferencesAtlasTable() *atlasschema.Table {
	table := atlasschema.NewTable("ridu_document_references")
	ownerCollection := atlasschema.NewStringColumn("owner_collection_id", atlaspostgres.TypeText)
	ownerDocument := atlasschema.NewStringColumn("owner_document_id", atlaspostgres.TypeText)
	field := atlasschema.NewStringColumn("field_id", atlaspostgres.TypeText)
	targetCollection := atlasschema.NewStringColumn("target_collection_id", atlaspostgres.TypeText)
	targetDocument := atlasschema.NewStringColumn("target_document_id", atlaspostgres.TypeText)
	locale := atlasschema.NewStringColumn("locale", atlaspostgres.TypeText).SetDefault(&atlasschema.Literal{V: ""})
	occurrence := atlasschema.NewIntColumn("occurrence", atlaspostgres.TypeInteger)
	table.AddColumns(ownerCollection, ownerDocument, field, targetCollection, targetDocument, locale, occurrence)
	table.SetPrimaryKey(atlasschema.NewPrimaryKey(ownerCollection, ownerDocument, field, targetCollection, targetDocument, locale, occurrence))
	table.AddIndexes(
		atlasschema.NewIndex("ridu_document_references_target_idx").AddColumns(targetCollection, targetDocument),
		atlasschema.NewIndex("ridu_document_references_owner_idx").AddColumns(ownerCollection, ownerDocument),
	)
	return table
}

func preferencesAtlasTable() *atlasschema.Table {
	table := atlasschema.NewTable("ridu_preferences")
	collection := atlasschema.NewStringColumn("collection_id", atlaspostgres.TypeText)
	user := atlasschema.NewStringColumn("user_id", atlaspostgres.TypeText)
	key := atlasschema.NewStringColumn("preference_key", atlaspostgres.TypeText)
	table.AddColumns(
		collection,
		user,
		key,
		atlasschema.NewJSONColumn("value", atlaspostgres.TypeJSONB),
		atlasschema.NewTimeColumn("updated_at", atlaspostgres.TypeTimestampTZ).SetDefault(&atlasschema.RawExpr{X: "now()"}),
	)
	return table.SetPrimaryKey(atlasschema.NewPrimaryKey(collection, user, key))
}

func collectionAtlasTable(collection schema.Collection, id schema.StableID, mapping atlasIdentityMap, locales []schema.LocaleCode, uploadReferenceIndexes, canonicalAuthIdentity bool) *atlasschema.Table {
	table := atlasschema.NewTable(collectionTable(id))
	idColumn := atlasschema.NewStringColumn("id", atlaspostgres.TypeText)
	table.AddColumns(
		idColumn,
		atlasschema.NewTimeColumn("created_at", atlaspostgres.TypeTimestampTZ).SetDefault(&atlasschema.RawExpr{X: "now()"}),
		atlasschema.NewTimeColumn("updated_at", atlaspostgres.TypeTimestampTZ).SetDefault(&atlasschema.RawExpr{X: "now()"}),
		atlasschema.NewNullTimeColumn("deleted_at", atlaspostgres.TypeTimestampTZ),
	)
	table.SetPrimaryKey(atlasschema.NewPrimaryKey(idColumn))
	if collection.Versions != nil {
		status := "draft"
		if !collection.Versions.Drafts {
			status = "published"
		}
		table.AddColumns(
			atlasschema.NewStringColumn("_status", atlaspostgres.TypeText).SetDefault(&atlasschema.Literal{V: status}),
			atlasschema.NewIntColumn("_revision", atlaspostgres.TypeInteger).SetDefault(&atlasschema.Literal{V: "1"}),
		)
	}
	for _, field := range collection.Fields {
		if field.Category == schema.FieldCategoryPresentation {
			continue
		}
		fieldID := mapping.field(collection.ID, field.ID)
		columns := []*atlasschema.Column{atlasFieldColumn(field, fieldID, "")}
		if field.Localized {
			columns = columns[:0]
			for _, locale := range locales {
				columns = append(columns, atlasFieldColumn(field, fieldID, locale))
			}
		}
		for index, column := range columns {
			table.AddColumns(column)
			if field.Unique {
				indexKey := string(id) + ":" + string(fieldID)
				if field.Localized {
					indexKey += ":" + string(locales[index])
				}
				indexName := "z_u_" + identifierHash(indexKey)
				unique := atlasschema.NewUniqueIndex(indexName)
				if collection.Auth != nil && field.Name == collection.Auth.IdentityField && !canonicalAuthIdentity {
					unique.AddExprs(&atlasschema.RawExpr{X: fmt.Sprintf("lower(%s)", column.Name)})
				} else {
					unique.AddColumns(column)
				}
				if collection.Capabilities.Trash {
					unique.AddAttrs(&atlaspostgres.IndexPredicate{P: "deleted_at IS NULL"})
				}
				table.AddIndexes(unique)
			}
		}
	}
	for _, specification := range collectionAtlasIndexes(collection) {
		indexLocales := []schema.LocaleCode{""}
		if specification.localized {
			indexLocales = locales
		}
		for _, locale := range indexLocales {
			keyParts := []string{string(id)}
			for _, chain := range specification.fields {
				for _, candidate := range chain {
					keyParts = append(keyParts, string(mapping.field(collection.ID, candidate.ID)))
				}
				keyParts = append(keyParts, "|")
			}
			keyParts = append(keyParts, string(locale))
			prefix := "z_i_"
			var index *atlasschema.Index
			if specification.unique {
				prefix = "z_cu_"
				index = atlasschema.NewUniqueIndex(prefix + identifierHash(strings.Join(keyParts, "\x00")))
			} else {
				index = atlasschema.NewIndex(prefix + identifierHash(strings.Join(keyParts, "\x00")))
			}
			for _, chain := range specification.fields {
				if len(chain) == 1 {
					fieldID := mapping.field(collection.ID, chain[0].ID)
					columnName := fieldColumn(fieldID)
					if chain[0].Localized {
						columnName = localizedFieldColumn(fieldID, locale)
					}
					column, _ := table.Column(columnName)
					index.AddColumns(column)
					continue
				}
				index.AddExprs(&atlasschema.RawExpr{X: atlasNestedIndexExpression(collection.ID, chain, locale, mapping)})
			}
			if specification.unique && collection.Capabilities.Trash {
				index.AddAttrs(&atlaspostgres.IndexPredicate{P: "deleted_at IS NULL"})
			}
			table.AddIndexes(index)
		}
	}
	if uploadReferenceIndexes && collection.Upload != nil {
		for _, field := range collection.Fields {
			if field.Name != "sizes" || field.Category != schema.FieldCategoryUpload {
				continue
			}
			// Generated physical identifiers are deliberately restricted to
			// unquoted-safe characters. Match PostgreSQL's inspected canonical
			// expression so schema assertions do not replace an identical index.
			column := fieldColumn(mapping.field(collection.ID, field.ID))
			index := atlasschema.NewIndex("z_ui_" + identifierHash(string(id)+":upload-size-object-keys"))
			index.AddExprs(&atlasschema.RawExpr{X: fmt.Sprintf("jsonb_path_query_array(%s, '$.*.\"objectKey\"'::jsonpath)", column)})
			index.AddAttrs(&atlaspostgres.IndexType{T: atlaspostgres.IndexTypeGIN})
			table.AddIndexes(index)
			break
		}
	}
	return table
}

type atlasIndexSpecification struct {
	fields    [][]schema.Field
	unique    bool
	localized bool
}

func collectionAtlasIndexes(collection schema.Collection) []atlasIndexSpecification {
	var specifications []atlasIndexSpecification
	var inspect func([]schema.Field, []schema.Field)
	inspect = func(fields []schema.Field, parents []schema.Field) {
		for _, candidate := range fields {
			chain := append(append([]schema.Field(nil), parents...), candidate)
			if candidate.Index && !candidate.Unique {
				specifications = append(specifications, atlasIndexSpecification{fields: [][]schema.Field{chain}, localized: atlasIndexChainLocalized(chain)})
			}
			if candidate.Type == schema.FieldTypeGroup && candidate.Nested != nil {
				inspect(candidate.Nested.Fields, chain)
			}
		}
	}
	inspect(collection.Fields, nil)
	for _, candidate := range collection.Indexes {
		specification := atlasIndexSpecification{unique: candidate.Unique, fields: make([][]schema.Field, 0, len(candidate.Fields))}
		for _, path := range candidate.Fields {
			chain := atlasIndexFieldChain(collection.Fields, path.Segments())
			specification.fields = append(specification.fields, chain)
			specification.localized = specification.localized || atlasIndexChainLocalized(chain)
		}
		specifications = append(specifications, specification)
	}
	return specifications
}

func atlasIndexFieldChain(fields []schema.Field, segments []string) []schema.Field {
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
		return append(chain, atlasIndexFieldChain(candidate.Nested.Fields, segments[1:])...)
	}
	return nil
}

func atlasIndexChainLocalized(chain []schema.Field) bool {
	for _, candidate := range chain {
		if candidate.Localized {
			return true
		}
	}
	return false
}

func atlasNestedIndexExpression(collectionID schema.StableID, chain []schema.Field, locale schema.LocaleCode, mapping atlasIdentityMap) string {
	rootID := mapping.field(collectionID, chain[0].ID)
	column := fieldColumn(rootID)
	if chain[0].Localized {
		column = localizedFieldColumn(rootID, locale)
	}
	segments := make([]string, 0, len(chain))
	for _, candidate := range chain[1:] {
		segments = append(segments, candidate.Name)
		if candidate.Localized {
			segments = append(segments, string(locale))
		}
	}
	// PostgreSQL's inspector returns expression index parts in a canonical form
	// with an explicit text[] cast and two outer parentheses. Model that exact
	// representation up front so Atlas does not continuously replace an
	// otherwise identical expression index. Generated physical identifiers are
	// deliberately restricted to unquoted-safe characters.
	expression := "((" + column + " #>> '{" + strings.Join(segments, ",") + "}'::text[]))"
	terminal := chain[len(chain)-1]
	switch terminal.Type {
	case schema.FieldTypeNumber:
		return "(" + expression + "::double precision)"
	case schema.FieldTypeCheckbox:
		return "(" + expression + "::boolean)"
	default:
		return expression
	}
}

func atlasFieldColumn(field schema.Field, id schema.StableID, locale schema.LocaleCode) *atlasschema.Column {
	name := fieldColumn(id)
	if locale != "" {
		name = localizedFieldColumn(id, locale)
	}
	var column *atlasschema.Column
	switch columnType(field) {
	case "jsonb":
		column = atlasschema.NewJSONColumn(name, atlaspostgres.TypeJSONB)
	case "double precision":
		column = atlasschema.NewFloatColumn(name, atlaspostgres.TypeDouble)
	case "boolean":
		column = atlasschema.NewBoolColumn(name, atlaspostgres.TypeBoolean)
	default:
		column = atlasschema.NewStringColumn(name, atlaspostgres.TypeText)
	}
	column.SetNull(!field.Required || locale != "")
	if field.Default != nil && locale == "" {
		value := *field.Default
		if field.Type == schema.FieldTypeNumber {
			if _, err := strconv.ParseFloat(value, 64); err == nil {
				column.SetDefault(&atlasschema.Literal{V: value})
			}
		} else if field.Type == schema.FieldTypeCheckbox {
			column.SetDefault(&atlasschema.Literal{V: value})
		} else {
			column.SetDefault(&atlasschema.Literal{V: value})
		}
	}
	return column
}

func hasAuthCollections(collections []schema.Collection) bool {
	for _, collection := range collections {
		if collection.Auth != nil {
			return true
		}
	}
	return false
}

func hasVersionCollections(collections []schema.Collection) bool {
	for _, collection := range collections {
		if collection.Versions != nil {
			return true
		}
	}
	return false
}

func hasDocumentLockCollections(collections []schema.Collection) bool {
	for _, collection := range collections {
		if collection.DocumentLock != nil {
			return true
		}
	}
	return false
}

func documentLocksAtlasTable(lifecycleIndexes bool) *atlasschema.Table {
	table := atlasschema.NewTable("ridu_document_locks")
	collection := atlasschema.NewStringColumn("collection_id", atlaspostgres.TypeText)
	document := atlasschema.NewStringColumn("document_id", atlaspostgres.TypeText)
	ownerCollection := atlasschema.NewStringColumn("owner_collection_id", atlaspostgres.TypeText)
	owner := atlasschema.NewStringColumn("owner_id", atlaspostgres.TypeText)
	expires := atlasschema.NewTimeColumn("expires_at", atlaspostgres.TypeTimestampTZ)
	table.AddColumns(
		collection,
		document,
		ownerCollection,
		owner,
		atlasschema.NewStringColumn("owner_label", atlaspostgres.TypeText),
		atlasschema.NewTimeColumn("created_at", atlaspostgres.TypeTimestampTZ),
		atlasschema.NewTimeColumn("updated_at", atlaspostgres.TypeTimestampTZ),
		expires,
	)
	table.SetPrimaryKey(atlasschema.NewPrimaryKey(collection, document)).AddIndexes(
		atlasschema.NewIndex("ridu_document_locks_expires_idx").AddColumns(expires),
	)
	if lifecycleIndexes {
		table.AddIndexes(atlasschema.NewIndex("ridu_document_locks_owner_idx").AddColumns(ownerCollection, owner))
	}
	return table
}

func authCredentialsAtlasTable() *atlasschema.Table {
	table := atlasschema.NewTable("ridu_auth_credentials")
	collection := atlasschema.NewStringColumn("collection_id", atlaspostgres.TypeText)
	user := atlasschema.NewStringColumn("user_id", atlaspostgres.TypeText)
	table.AddColumns(
		collection,
		user,
		atlasschema.NewBinaryColumn("password_hash", atlaspostgres.TypeBytea),
	)
	table.AddColumns(
		atlasschema.NewIntColumn("failed_login_attempts", atlaspostgres.TypeInteger).SetDefault(&atlasschema.Literal{V: "0"}),
		atlasschema.NewNullTimeColumn("locked_until", atlaspostgres.TypeTimestampTZ),
		atlasschema.NewBoolColumn("verified", atlaspostgres.TypeBoolean).SetDefault(&atlasschema.Literal{V: "true"}),
	)
	return table.SetPrimaryKey(atlasschema.NewPrimaryKey(collection, user))
}

func authTokensAtlasTable() *atlasschema.Table {
	table := atlasschema.NewTable("ridu_auth_tokens")
	token := atlasschema.NewStringColumn("token_hash", atlaspostgres.TypeText)
	collection := atlasschema.NewStringColumn("collection_id", atlaspostgres.TypeText)
	user := atlasschema.NewStringColumn("user_id", atlaspostgres.TypeText)
	purpose := atlasschema.NewStringColumn("purpose", atlaspostgres.TypeText)
	expires := atlasschema.NewTimeColumn("expires_at", atlaspostgres.TypeTimestampTZ)
	table.AddColumns(
		token, collection, user, purpose, expires,
		atlasschema.NewTimeColumn("created_at", atlaspostgres.TypeTimestampTZ).SetDefault(&atlasschema.RawExpr{X: "now()"}),
	)
	table.AddIndexes(
		atlasschema.NewUniqueIndex("ridu_auth_tokens_user_purpose_unique").AddColumns(collection, user, purpose),
		atlasschema.NewIndex("ridu_auth_tokens_expiry_idx").AddColumns(expires),
	)
	return table.SetPrimaryKey(atlasschema.NewPrimaryKey(token))
}

func authAPIKeysAtlasTable(lifecycleIndex bool) *atlasschema.Table {
	table := atlasschema.NewTable("ridu_auth_api_keys")
	id := atlasschema.NewStringColumn("id", atlaspostgres.TypeText)
	token := atlasschema.NewStringColumn("token_hash", atlaspostgres.TypeText)
	collection := atlasschema.NewStringColumn("collection_id", atlaspostgres.TypeText)
	user := atlasschema.NewStringColumn("user_id", atlaspostgres.TypeText)
	expires := atlasschema.NewNullTimeColumn("expires_at", atlaspostgres.TypeTimestampTZ)
	table.AddColumns(
		id, token, collection, user,
		atlasschema.NewStringColumn("name", atlaspostgres.TypeText),
		atlasschema.NewTimeColumn("created_at", atlaspostgres.TypeTimestampTZ).SetDefault(&atlasschema.RawExpr{X: "now()"}),
		atlasschema.NewNullTimeColumn("last_used_at", atlaspostgres.TypeTimestampTZ),
		expires,
	)
	table.AddIndexes(
		atlasschema.NewUniqueIndex("ridu_auth_api_keys_token_unique").AddColumns(token),
		atlasschema.NewIndex("ridu_auth_api_keys_user_idx").AddColumns(collection, user),
	)
	if lifecycleIndex {
		table.AddIndexes(
			atlasschema.NewIndex("ridu_auth_api_keys_expiry_idx").AddColumns(expires, id).
				AddAttrs(&atlaspostgres.IndexPredicate{P: "expires_at IS NOT NULL"}),
		)
	}
	return table.SetPrimaryKey(atlasschema.NewPrimaryKey(id))
}

func authRateLimitsAtlasTable() *atlasschema.Table {
	table := atlasschema.NewTable("ridu_auth_rate_limits")
	key := atlasschema.NewStringColumn("key_hash", atlaspostgres.TypeText)
	expires := atlasschema.NewTimeColumn("expires_at", atlaspostgres.TypeTimestampTZ)
	table.AddColumns(
		key,
		atlasschema.NewIntColumn("attempts", atlaspostgres.TypeInteger),
		atlasschema.NewTimeColumn("window_started_at", atlaspostgres.TypeTimestampTZ),
		expires,
	)
	table.AddIndexes(atlasschema.NewIndex("ridu_auth_rate_limits_expiry_idx").AddColumns(expires))
	return table.SetPrimaryKey(atlasschema.NewPrimaryKey(key))
}

func authSessionsAtlasTable(lifecycleIndex bool) *atlasschema.Table {
	table := atlasschema.NewTable("ridu_auth_sessions")
	// The default is required for a safe in-place migration of installations
	// that already have active sessions. New sessions always provide a stronger
	// framework-generated opaque ID explicitly.
	id := atlasschema.NewColumn("id").SetType(&atlasschema.UUIDType{T: atlaspostgres.TypeUUID}).
		SetDefault(&atlasschema.RawExpr{X: "gen_random_uuid()"})
	token := atlasschema.NewStringColumn("token_hash", atlaspostgres.TypeText)
	collection := atlasschema.NewStringColumn("collection_id", atlaspostgres.TypeText)
	user := atlasschema.NewStringColumn("user_id", atlaspostgres.TypeText)
	expires := atlasschema.NewTimeColumn("expires_at", atlaspostgres.TypeTimestampTZ)
	table.AddColumns(
		id,
		token,
		collection,
		user,
		expires,
		atlasschema.NewTimeColumn("created_at", atlaspostgres.TypeTimestampTZ).SetDefault(&atlasschema.RawExpr{X: "now()"}),
		atlasschema.NewTimeColumn("last_seen_at", atlaspostgres.TypeTimestampTZ).SetDefault(&atlasschema.RawExpr{X: "now()"}),
		atlasschema.NewStringColumn("ip_address", atlaspostgres.TypeText).SetDefault(&atlasschema.Literal{V: "''"}),
		atlasschema.NewStringColumn("user_agent", atlaspostgres.TypeText).SetDefault(&atlasschema.Literal{V: "''"}),
	)
	table.AddIndexes(
		atlasschema.NewUniqueIndex("ridu_auth_sessions_id_unique").AddColumns(id),
		atlasschema.NewIndex("ridu_auth_sessions_user_idx").AddColumns(collection, user),
	)
	if lifecycleIndex {
		table.AddIndexes(atlasschema.NewIndex("ridu_auth_sessions_expiry_idx").AddColumns(expires, token))
	}
	return table.SetPrimaryKey(atlasschema.NewPrimaryKey(token))
}

func versionsAtlasTable(uploadReferenceIndexes bool) *atlasschema.Table {
	table := atlasschema.NewTable("ridu_versions")
	collection := atlasschema.NewStringColumn("collection_id", atlaspostgres.TypeText)
	document := atlasschema.NewStringColumn("document_id", atlaspostgres.TypeText)
	revision := atlasschema.NewIntColumn("revision", atlaspostgres.TypeInteger)
	snapshot := atlasschema.NewJSONColumn("snapshot", atlaspostgres.TypeJSONB)
	table.AddColumns(
		collection, document, revision,
		atlasschema.NewStringColumn("status", atlaspostgres.TypeText),
		snapshot,
		atlasschema.NewTimeColumn("created_at", atlaspostgres.TypeTimestampTZ).SetDefault(&atlasschema.RawExpr{X: "now()"}),
	)
	table.SetPrimaryKey(atlasschema.NewPrimaryKey(collection, document, revision))
	if !uploadReferenceIndexes {
		return table
	}
	objectKey := atlasschema.NewIndex("ridu_versions_upload_object_key_idx").AddColumns(collection)
	objectKey.AddExprs(&atlasschema.RawExpr{X: "((snapshot #>> '{Values,objectKey}'::text[]))"})
	objectKey.AddAttrs(&atlaspostgres.IndexPredicate{P: "((snapshot #>> '{Values,objectKey}'::text[]) IS NOT NULL)"})
	sizes := atlasschema.NewIndex("ridu_versions_upload_size_keys_idx")
	sizes.AddExprs(&atlasschema.RawExpr{X: "jsonb_path_query_array((snapshot #> '{Values,sizes}'::text[]), '$.*.\"objectKey\"'::jsonpath)"})
	sizes.AddAttrs(
		&atlaspostgres.IndexType{T: atlaspostgres.IndexTypeGIN},
		&atlaspostgres.IndexPredicate{P: "((snapshot #> '{Values,sizes}'::text[]) IS NOT NULL)"},
	)
	return table.AddIndexes(objectKey, sizes)
}

func tasksAtlasTable() *atlasschema.Table {
	table := atlasschema.NewTable("ridu_tasks")
	id := atlasschema.NewStringColumn("id", atlaspostgres.TypeText)
	taskSlug := atlasschema.NewStringColumn("task_slug", atlaspostgres.TypeText)
	queue := atlasschema.NewStringColumn("queue", atlaspostgres.TypeText)
	concurrencyKey := atlasschema.NewNullStringColumn("concurrency_key", atlaspostgres.TypeText)
	state := atlasschema.NewStringColumn("state", atlaspostgres.TypeText)
	runAt := atlasschema.NewTimeColumn("run_at", atlaspostgres.TypeTimestampTZ)
	createdAt := atlasschema.NewTimeColumn("created_at", atlaspostgres.TypeTimestampTZ).SetDefault(&atlasschema.RawExpr{X: "now()"})
	leaseExpiresAt := atlasschema.NewNullTimeColumn("lease_expires_at", atlaspostgres.TypeTimestampTZ)
	targetCollection := atlasschema.NewNullStringColumn("target_collection_id", atlaspostgres.TypeText)
	targetDocument := atlasschema.NewNullStringColumn("target_document_id", atlaspostgres.TypeText)
	requesterCollection := atlasschema.NewNullStringColumn("requested_by_collection_id", atlaspostgres.TypeText)
	requesterDocument := atlasschema.NewNullStringColumn("requested_by_document_id", atlaspostgres.TypeText)
	retainUntil := atlasschema.NewNullTimeColumn("retain_until", atlaspostgres.TypeTimestampTZ)

	table.AddColumns(
		id,
		taskSlug,
		queue,
		concurrencyKey,
		atlasschema.NewJSONColumn("input", atlaspostgres.TypeJSONB),
		atlasschema.NewNullJSONColumn("output", atlaspostgres.TypeJSONB),
		state,
		runAt,
		atlasschema.NewIntColumn("attempts", atlaspostgres.TypeInteger).SetDefault(&atlasschema.Literal{V: "0"}),
		atlasschema.NewIntColumn("max_attempts", atlaspostgres.TypeInteger),
		atlasschema.NewIntColumn("retry_delay_ms", atlaspostgres.TypeBigInt),
		atlasschema.NewIntColumn("max_retry_delay_ms", atlaspostgres.TypeBigInt),
		atlasschema.NewStringColumn("backoff", atlaspostgres.TypeText),
		atlasschema.NewIntColumn("timeout_ms", atlaspostgres.TypeBigInt),
		atlasschema.NewIntColumn("retention_ms", atlaspostgres.TypeBigInt),
		atlasschema.NewNullStringColumn("lease_token", atlaspostgres.TypeText),
		leaseExpiresAt,
		targetCollection,
		targetDocument,
		requesterCollection,
		requesterDocument,
		atlasschema.NewNullStringColumn("last_error_code", atlaspostgres.TypeText),
		atlasschema.NewNullStringColumn("last_error", atlaspostgres.TypeText),
		createdAt,
		atlasschema.NewTimeColumn("updated_at", atlaspostgres.TypeTimestampTZ).SetDefault(&atlasschema.RawExpr{X: "now()"}),
		atlasschema.NewNullTimeColumn("completed_at", atlaspostgres.TypeTimestampTZ),
		retainUntil,
	)
	table.SetPrimaryKey(atlasschema.NewPrimaryKey(id))
	table.AddChecks(
		atlasschema.NewCheck().SetName("ridu_tasks_state_check").SetExpr("state IN ('queued', 'running', 'succeeded', 'failed', 'canceled')"),
		atlasschema.NewCheck().SetName("ridu_tasks_backoff_check").SetExpr("backoff IN ('fixed', 'linear', 'exponential')"),
		atlasschema.NewCheck().SetName("ridu_tasks_attempts_check").SetExpr("attempts >= 0"),
		atlasschema.NewCheck().SetName("ridu_tasks_max_attempts_check").SetExpr("max_attempts BETWEEN 1 AND 100"),
		atlasschema.NewCheck().SetName("ridu_tasks_retry_check").SetExpr(fmt.Sprintf("retry_delay_ms BETWEEN 1 AND %d AND max_retry_delay_ms BETWEEN retry_delay_ms AND %d", store.MaxTaskRetryDelay.Milliseconds(), store.MaxTaskRetryDelay.Milliseconds())),
		atlasschema.NewCheck().SetName("ridu_tasks_timeout_retention_check").SetExpr(fmt.Sprintf("timeout_ms BETWEEN 1 AND %d AND retention_ms BETWEEN %d AND %d", store.MaxTaskTimeout.Milliseconds(), store.MinTaskRetention.Milliseconds(), store.MaxTaskRetention.Milliseconds())),
		atlasschema.NewCheck().SetName("ridu_tasks_identifier_bounds_check").SetExpr(fmt.Sprintf("octet_length(id) BETWEEN 1 AND 256 AND octet_length(task_slug) BETWEEN 1 AND 128 AND task_slug ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$' AND octet_length(queue) BETWEEN 1 AND 64 AND queue ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$' AND (concurrency_key IS NULL OR (octet_length(concurrency_key) BETWEEN 1 AND %d AND btrim(concurrency_key) = concurrency_key)) AND (lease_token IS NULL OR octet_length(lease_token) BETWEEN 1 AND 256)", store.MaxTaskConcurrencyKeyBytes)),
		atlasschema.NewCheck().SetName("ridu_tasks_payload_bounds_check").SetExpr(fmt.Sprintf("octet_length(input::text) BETWEEN 1 AND %d AND (output IS NULL OR octet_length(output::text) BETWEEN 1 AND %d)", store.MaxTaskPayloadBytes, store.MaxTaskPayloadBytes)),
		atlasschema.NewCheck().SetName("ridu_tasks_error_bounds_check").SetExpr(fmt.Sprintf("(last_error_code IS NULL OR (octet_length(last_error_code) BETWEEN 1 AND 128 AND last_error_code ~ '^[a-z][a-z0-9_-]*$')) AND (last_error IS NULL OR octet_length(last_error) <= %d)", store.MaxTaskErrorBytes)),
		atlasschema.NewCheck().SetName("ridu_tasks_reference_bounds_check").SetExpr(fmt.Sprintf("(target_collection_id IS NULL OR target_collection_id ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$') AND (requested_by_collection_id IS NULL OR requested_by_collection_id ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$') AND (target_document_id IS NULL OR octet_length(target_document_id) BETWEEN 1 AND %d) AND (requested_by_document_id IS NULL OR octet_length(requested_by_document_id) BETWEEN 1 AND %d)", store.MaxTaskReferenceIDBytes, store.MaxTaskReferenceIDBytes)),
		atlasschema.NewCheck().SetName("ridu_tasks_target_pair_check").SetExpr("(target_collection_id IS NULL) = (target_document_id IS NULL)"),
		atlasschema.NewCheck().SetName("ridu_tasks_requester_pair_check").SetExpr("(requested_by_collection_id IS NULL) = (requested_by_document_id IS NULL)"),
		atlasschema.NewCheck().SetName("ridu_tasks_lease_check").SetExpr("(state = 'running' AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL) OR (state <> 'running' AND lease_token IS NULL AND lease_expires_at IS NULL)"),
		atlasschema.NewCheck().SetName("ridu_tasks_terminal_check").SetExpr("(state IN ('succeeded', 'failed', 'canceled') AND completed_at IS NOT NULL AND retain_until IS NOT NULL) OR (state NOT IN ('succeeded', 'failed', 'canceled') AND completed_at IS NULL AND retain_until IS NULL)"),
		atlasschema.NewCheck().SetName("ridu_tasks_output_state_check").SetExpr("(state = 'succeeded' AND output IS NOT NULL) OR (state <> 'succeeded' AND output IS NULL)"),
	)
	table.AddIndexes(
		atlasschema.NewIndex("ridu_tasks_due_idx").AddColumns(state, runAt, createdAt, id).
			AddAttrs(&atlaspostgres.IndexPredicate{P: "(state = ANY (ARRAY['queued'::text, 'running'::text]))"}),
		atlasschema.NewIndex("ridu_tasks_lease_idx").AddColumns(leaseExpiresAt).
			AddAttrs(&atlaspostgres.IndexPredicate{P: "(state = 'running'::text)"}),
		atlasschema.NewIndex("ridu_tasks_concurrency_idx").AddColumns(queue, concurrencyKey, state, leaseExpiresAt).
			AddAttrs(&atlaspostgres.IndexPredicate{P: "concurrency_key IS NOT NULL"}),
		atlasschema.NewIndex("ridu_tasks_target_idx").AddColumns(taskSlug, targetCollection, targetDocument, state, runAt).
			AddAttrs(&atlaspostgres.IndexPredicate{P: "target_collection_id IS NOT NULL"}),
		atlasschema.NewIndex("ridu_tasks_requester_idx").AddColumns(requesterCollection, requesterDocument).
			AddAttrs(&atlaspostgres.IndexPredicate{P: "requested_by_collection_id IS NOT NULL"}),
		atlasschema.NewIndex("ridu_tasks_retention_idx").AddColumns(retainUntil, id).
			AddAttrs(&atlaspostgres.IndexPredicate{P: "retain_until IS NOT NULL"}),
	)
	return table
}
