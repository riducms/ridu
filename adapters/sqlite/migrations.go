package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/riducms/ridu/internal/blockgraph"
	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/primitivefield"
	"github.com/riducms/ridu/internal/requiredfield"
	"github.com/riducms/ridu/internal/schemadiff"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

// The SQLite planner accepts only artifacts recorded by this exact planner.
// Change the version whenever the planner can emit a different artifact for
// the same input, so stale artifacts fail before replay instead of replanning
// into a digest mismatch.
const (
	sqlitePlannerName    = "ridu-sqlite"
	sqlitePlannerVersion = "1.3.0"
)

// sqliteUnsupportedPlannerVersion explains that an artifact or ledger row was
// recorded by another planner contract, which this release cannot replay.
func sqliteUnsupportedPlannerVersion(subject, version string) error {
	return fmt.Errorf("%s uses unsupported planner version %q; this Ridu release supports only %s %q, so create a new migration history with ridu migrate create and apply it to a new database", subject, version, sqlitePlannerName, sqlitePlannerVersion)
}

// MigrationStatus describes one immutable SQLite artifact relative to the
// database ledger. SQLite artifacts are atomic, so phases and steps are either
// wholly pending or wholly complete and do not need a checkpoint vocabulary.
type MigrationStatus struct {
	Name     string                 `json:"name"`
	Checksum string                 `json:"checksum"`
	Version  uint32                 `json:"version"`
	Applied  bool                   `json:"applied"`
	Phases   []MigrationPhaseStatus `json:"phases"`
}

// MigrationPhaseStatus reports one transaction phase in an immutable artifact.
type MigrationPhaseStatus struct {
	ID    string                  `json:"id"`
	Mode  ridumigration.PhaseMode `json:"mode"`
	State string                  `json:"state"`
	Steps []MigrationStepStatus   `json:"steps"`
}

// MigrationStepStatus reports one SQLite artifact step.
type MigrationStepStatus struct {
	ID    string                 `json:"id"`
	Kind  ridumigration.StepKind `json:"kind"`
	State string                 `json:"state"`
}

// SafetyError reports a valid SQLite transition that requires explicit
// destructive approval before an immutable artifact can be created.
type SafetyError struct {
	Risks []ridumigration.Risk
}

func (err *SafetyError) Error() string {
	messages := make([]string, len(err.Risks))
	for index, risk := range err.Risks {
		messages[index] = risk.Message
	}
	return "migration requires explicit safety resolution: " + strings.Join(messages, "; ")
}

// CreatedArtifact is the stable filesystem identity of one newly committed
// immutable SQLite migration. It deliberately does not expose Ridu's internal
// artifact-file representation.
type CreatedArtifact struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Checksum string `json:"checksum"`
	Version  uint32 `json:"version"`
}

// CreateArtifact plans and atomically creates one immutable SQLite migration
// file. Existing files are never overwritten and the new artifact must
// continue the latest committed manifest in directory. The before manifest is
// derived from that history so applications never need to decode private
// artifact files themselves.
func CreateArtifact(ctx context.Context, directory, name string, after schema.Manifest, now time.Time, allowDestructive bool, transforms ...ridumigration.DataTransformDescriptor) (CreatedArtifact, error) {
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return CreatedArtifact{}, err
	}
	if err := preflightSQLiteArtifacts(ctx, files); err != nil {
		return CreatedArtifact{}, err
	}
	if err := validateSQLiteDataTransformIdentities(files, transforms); err != nil {
		return CreatedArtifact{}, err
	}
	var before *schema.Manifest
	if len(files) != 0 {
		latest, err := files[len(files)-1].Artifact.AfterManifest()
		if err != nil {
			return CreatedArtifact{}, err
		}
		before = &latest
	}
	artifact, err := buildSQLiteArtifactWithDataTransformDescriptors(ctx, name, before, after, transforms, len(transforms) != 0)
	if err != nil {
		return CreatedArtifact{}, err
	}
	if err := requireSQLiteDestructiveApproval(artifact.Risks, allowDestructive); err != nil {
		return CreatedArtifact{}, err
	}
	file, err := migrationartifact.Create(directory, name, artifact, now)
	if err != nil {
		return CreatedArtifact{}, err
	}
	return CreatedArtifact{
		Path: file.Path, Name: file.Name, Checksum: file.Digest, Version: file.Artifact.Version,
	}, nil
}

// planArtifact creates an unpublished deterministic SQLite migration plan.
// The generic JSON document layout makes adding resources and optional fields
// manifest-only changes. Initial history installs the fixed adapter schema.
// Transitions that need content rewrites or destructive interpretation are
// rejected until SQLite has a typed executor for them. Ordinary CreateArtifact
// is the author-facing API: it derives adapter history and binds the plan to
// the exact predecessor before publishing it.
func planArtifact(ctx context.Context, name string, before *schema.Manifest, after schema.Manifest, allowDestructive bool, transforms ...ridumigration.DataTransformDescriptor) (ridumigration.Artifact, error) {
	artifact, err := buildSQLiteArtifactWithDataTransformDescriptors(ctx, name, before, after, transforms, len(transforms) != 0)
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	if err := requireSQLiteDestructiveApproval(artifact.Risks, allowDestructive); err != nil {
		return ridumigration.Artifact{}, err
	}
	return artifact, nil
}

func requireSQLiteDestructiveApproval(risks []ridumigration.Risk, allowDestructive bool) error {
	if allowDestructive {
		return nil
	}
	var blocked []ridumigration.Risk
	for _, risk := range risks {
		if risk.Level == ridumigration.RiskDestructive {
			blocked = append(blocked, risk)
		}
	}
	if len(blocked) != 0 {
		return &SafetyError{Risks: blocked}
	}
	return nil
}

func buildSQLiteArtifactWithValidation(ctx context.Context, name string, before *schema.Manifest, after schema.Manifest, validateTransition func(schema.Snapshot, schema.Snapshot) error, renames []ridumigration.Rename) (ridumigration.Artifact, error) {
	if err := ctx.Err(); err != nil {
		return ridumigration.Artifact{}, err
	}
	if before != nil {
		if err := schemadiff.RejectVersionsEnable(before.Snapshot(), after.Snapshot(), nil); err != nil {
			return ridumigration.Artifact{}, err
		}
		fromDigest, err := ridumigration.DigestManifest(*before)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		toDigest, err := ridumigration.DigestManifest(after)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		if fromDigest == toDigest {
			return ridumigration.Artifact{}, fmt.Errorf("%w; no SQLite migration steps were planned", migrationartifact.ErrSchemaCurrent)
		}
		if err := validateTransition(before.Snapshot(), after.Snapshot()); err != nil {
			return ridumigration.Artifact{}, err
		}
	}

	artifact, err := ridumigration.NewArtifact(name, ridumigration.Planner{
		Name: sqlitePlannerName, Version: sqlitePlannerVersion,
	}, before, after)
	if err != nil {
		return ridumigration.Artifact{}, err
	}

	var statements []string
	stepName := "install SQLite schema object"
	if before == nil {
		statements = sqliteSchemaStatements
	}
	steps := make([]ridumigration.Step, 0, len(statements)+1)
	for index, statement := range statements {
		payload, err := ridumigration.MarshalStepPayload(ridumigration.SQLPayload{SQL: statement})
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		steps = append(steps, ridumigration.Step{
			ID: fmt.Sprintf("step-%04d", len(steps)+1), Kind: ridumigration.StepSQL,
			ExecutorVersion: 1, Name: fmt.Sprintf("%s %03d", stepName, index+1), Payload: payload,
		})
	}
	for _, rename := range renames {
		payload, err := ridumigration.MarshalStepPayload(ridumigration.RenamePayload{Rename: rename})
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		owner, scope := string(rename.CollectionBefore), ""
		if rename.Block != "" {
			owner, scope = "block "+rename.Block, " of every "+rename.Block+" block"
		}
		steps = append(steps, ridumigration.Step{
			ID: fmt.Sprintf("step-%04d", len(steps)+1), Kind: ridumigration.StepRenameContent,
			ExecutorVersion: 1, Name: fmt.Sprintf("rename %s.%s to %s", owner, rename.FieldBefore, rename.FieldAfter), Payload: payload,
		})
		artifact.Risks = append(artifact.Risks, ridumigration.Risk{
			Code: "RIDU_SQLITE_FIELD_RENAME", Level: ridumigration.RiskWarning,
			Message: fmt.Sprintf("move stored %s.%s content to %s%s in current documents and retained versions; stop application writers while the migration runs", owner, rename.FieldBefore, rename.FieldAfter, scope),
		})
	}
	if before != nil {
		requirements, err := sqliteRequirements(*before, after, renames)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		if len(requirements) != 0 {
			payload, err := ridumigration.MarshalStepPayload(requiredfield.Payload(requirements))
			if err != nil {
				return ridumigration.Artifact{}, err
			}
			steps = append(steps, ridumigration.Step{
				ID: fmt.Sprintf("step-%04d", len(steps)+1), Kind: ridumigration.StepAuditRequiredValues,
				ExecutorVersion: 1, Name: requiredfield.StepName, Payload: payload,
			})
			artifact.Risks = append(artifact.Risks, requiredfield.Risks(requirements)...)
		}
	}
	assertion, err := ridumigration.MarshalStepPayload(ridumigration.AssertSchemaPayload{})
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	steps = append(steps, ridumigration.Step{
		ID: fmt.Sprintf("step-%04d", len(steps)+1), Kind: ridumigration.StepAssertSchema,
		ExecutorVersion: 1, Name: "verify resulting SQLite schema", Payload: assertion,
	})
	physicalBefore := ridumigration.PhysicalDigestSeed(artifact.FromDigest)
	physicalAfter, err := ridumigration.PhasePhysicalDigest(physicalBefore, ridumigration.PhaseTransaction, steps)
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	artifact.Phases = []ridumigration.Phase{{
		ID: "phase-001", Mode: ridumigration.PhaseTransaction,
		PhysicalContractVersion: ridumigration.PhysicalContractVersion,
		BeforePhysicalDigest:    physicalBefore, AfterPhysicalDigest: physicalAfter, Steps: steps,
	}}
	if err := artifact.Validate(); err != nil {
		return ridumigration.Artifact{}, err
	}
	return artifact, nil
}

func validateSQLiteAdditiveTransition(before, after schema.Snapshot) error {
	return sqliteAdditiveRules{}.snapshot(sqliteStorageSchema(before), sqliteStorageSchema(after))
}

// sqliteAdditiveRules validates an additive transition. renames maps, for each
// collection, a before field ID to the after field that carries its content
// under a new name. That pair is neither a removal nor an added field, and
// apart from its name it must pass the same rules as a field that stayed.
// paths maps the same renames by collection and dotted path, so a collection
// index or a join that names a renamed field is unchanged when it follows the
// rename. Every other rule is unchanged.
type sqliteAdditiveRules struct {
	renames map[schema.StableID]map[schema.StableID]schema.Field
	paths   map[schema.StableID]map[string]string
	// renamed is the renames entry of the collection being validated.
	renamed map[schema.StableID]schema.Field
	// embedded marks fields inside a plugin's embedded payload, which the
	// required-value audit does not read: their requiredness stays fixed.
	embedded bool
}

// sqliteFieldUnderIdentity returns target as it would be declared under
// another field's name: its own ID, name and path replaced, and every
// descendant's ID and path re-rooted beneath them. Validating the result
// against the earlier field then checks the renamed pair like any other.
func sqliteFieldUnderIdentity(target, identity schema.Field) schema.Field {
	restored := sqliteRerootedField(target, string(target.ID), string(identity.ID), len(target.Path.Segments()), identity.Path.Segments())
	restored.Name = identity.Name
	return restored
}

func sqliteRerootedField(field schema.Field, oldID, newID string, oldDepth int, newRoot []string) schema.Field {
	if rest, beneath := strings.CutPrefix(string(field.ID), oldID); beneath {
		field.ID = schema.StableID(newID + rest)
	}
	if segments := field.Path.Segments(); len(segments) >= oldDepth {
		if path, err := query.NewPath(append(append([]string(nil), newRoot...), segments[oldDepth:]...)...); err == nil {
			field.Path = path
		}
	}
	// Block definitions keep their own definition-relative fields wherever
	// they are placed, so only groups and arrays carry the renamed identity.
	if field.Nested != nil {
		nested := *field.Nested
		rerooted := make([]schema.Field, len(nested.ResolvedFields()))
		for index, child := range nested.ResolvedFields() {
			rerooted[index] = sqliteRerootedField(child, oldID, newID, oldDepth, newRoot)
		}
		nested.Fields = rerooted
		field.Nested = &nested
	}
	return field
}

// renamedPath follows a path in one collection through the reviewed renames:
// the renamed field itself and anything stored beneath it.
func (rules sqliteAdditiveRules) renamedPath(collection schema.StableID, path query.Path) query.Path {
	value := path.String()
	for before, after := range rules.paths[collection] {
		if value != before && !strings.HasPrefix(value, before+".") {
			continue
		}
		if renamed, err := query.ParsePath(after + value[len(before):]); err == nil {
			return renamed
		}
	}
	return path
}

func validateSQLiteAdditiveCollections(before, after []schema.Collection) error {
	return sqliteAdditiveRules{}.collections(before, after)
}

func validateSQLiteAdditiveResources(kind string, before, after []schema.Collection) error {
	return sqliteAdditiveRules{}.resources(kind, before, after)
}

func validateSQLiteAdditiveFields(location string, before, after []schema.Field) error {
	return sqliteAdditiveRules{}.fields(location, before, after)
}

func validateSQLiteAdditiveBlockTypes(location string, before, after []schema.BlockType) error {
	return sqliteAdditiveRules{}.blockTypes(location, before, after)
}

func (rules sqliteAdditiveRules) snapshot(before, after schema.Snapshot) error {
	// Caller-supplied ID admission is an operation-layer policy flag. It has no
	// SQLite storage representation, so changing only this setting must not
	// manufacture a physical migration incompatibility.
	currentApplication := after.Application
	currentApplication.AllowIDOnCreate = before.Application.AllowIDOnCreate
	if !reflect.DeepEqual(before.Application, currentApplication) {
		return fmt.Errorf("SQLite artifact planner supports only additive transitions; application settings changed")
	}
	if err := rules.collections(before.Collections, after.Collections); err != nil {
		return err
	}
	if err := rules.resources("global", before.Globals, after.Globals); err != nil {
		return err
	}
	return rules.definitions(before, after)
}

// sqliteDefinitionRenames keys a block definition's renames in
// sqliteAdditiveRules.renames beside collection IDs, which never contain a colon.
func sqliteDefinitionRenames(slug string) schema.StableID {
	return schema.StableID("block:" + slug)
}

// definitions validates each block definition placed before and after the
// transition once: ordinarily placed views with the ordinary rules, and views
// placed inside embedded plugin payloads with the embedded rules, which fix
// requiredness because the required-value audit does not read payloads.
// Containers already refused deselecting a placed definition.
func (rules sqliteAdditiveRules) definitions(before, after schema.Snapshot) error {
	previous, current := blockgraph.New(before), blockgraph.New(after)
	validate := func(keys map[blockgraph.Key]bool, embedded bool) error {
		for _, key := range blockgraph.SortedKeys(keys) {
			beforeView, _ := previous.View(key)
			afterView, placed := current.View(key)
			if !placed {
				continue
			}
			definition := rules
			definition.embedded = embedded
			definition.renamed = rules.renames[sqliteDefinitionRenames(key.Slug)]
			if !reflect.DeepEqual(beforeView.Labels, afterView.Labels) {
				return fmt.Errorf("SQLite artifact planner supports only additive transitions; block type %q changed", key.Slug)
			}
			if err := definition.fields(fmt.Sprintf("block type %q", key.Slug), beforeView.ResolvedFields(), afterView.ResolvedFields()); err != nil {
				return err
			}
		}
		return nil
	}
	if err := validate(blockgraph.Shared(previous, current, nil, false), false); err != nil {
		return err
	}
	return validate(blockgraph.EmbeddedPlacements(previous), true)
}

func (rules sqliteAdditiveRules) collections(before, after []schema.Collection) error {
	afterByID := make(map[schema.StableID]schema.Collection, len(after))
	for _, resource := range after {
		afterByID[resource.ID] = resource
	}
	for _, previous := range before {
		current, exists := afterByID[previous.ID]
		if !exists {
			return fmt.Errorf("SQLite artifact planner supports only additive transitions; collection %q was removed", previous.ID)
		}
		currentWithoutFieldsAndIndexes := current
		currentWithoutFieldsAndIndexes.Fields = previous.Fields
		currentWithoutFieldsAndIndexes.Indexes = previous.Indexes
		if !reflect.DeepEqual(previous, currentWithoutFieldsAndIndexes) {
			return fmt.Errorf("SQLite artifact planner supports only additive transitions; collection %q changed outside its fields and indexes", previous.ID)
		}
		previousIndexes := previous.Indexes
		if len(rules.paths[previous.ID]) != 0 {
			previousIndexes = make([]schema.CollectionIndex, len(previous.Indexes))
			for index, existing := range previous.Indexes {
				previousIndexes[index] = schema.CollectionIndex{Fields: make([]query.Path, len(existing.Fields)), Unique: existing.Unique}
				for position, path := range existing.Fields {
					previousIndexes[index].Fields[position] = rules.renamedPath(previous.ID, path)
				}
			}
		}
		if len(current.Indexes) < len(previous.Indexes) || !reflect.DeepEqual(previousIndexes, current.Indexes[:len(previous.Indexes)]) {
			return fmt.Errorf("SQLite artifact planner supports only additive transitions; collection %q changed or removed an existing index", previous.ID)
		}
		rules.renamed = rules.renames[previous.ID]
		if err := rules.fields(fmt.Sprintf("collection %q", previous.ID), previous.Fields, current.Fields); err != nil {
			return err
		}
	}
	return nil
}

func (rules sqliteAdditiveRules) resources(kind string, before, after []schema.Collection) error {
	afterByID := make(map[schema.StableID]schema.Collection, len(after))
	for _, resource := range after {
		afterByID[resource.ID] = resource
	}
	for _, previous := range before {
		current, exists := afterByID[previous.ID]
		if !exists {
			return fmt.Errorf("SQLite artifact planner supports only additive transitions; %s %q was removed", kind, previous.ID)
		}
		currentWithoutFields := current
		currentWithoutFields.Fields = previous.Fields
		if !reflect.DeepEqual(previous, currentWithoutFields) {
			return fmt.Errorf("SQLite artifact planner supports only additive transitions; %s %q changed outside its fields", kind, previous.ID)
		}
		location := fmt.Sprintf("%s %q", kind, previous.ID)
		rules.renamed = nil
		if err := rules.fields(location, previous.Fields, current.Fields); err != nil {
			return err
		}
	}
	return nil
}

func (rules sqliteAdditiveRules) fields(location string, before, after []schema.Field) error {
	afterByID := make(map[schema.StableID]schema.Field, len(after))
	for _, field := range after {
		afterByID[field.ID] = field
	}
	for _, previous := range before {
		current, exists := afterByID[previous.ID]
		if !exists {
			if intended, renamed := rules.renamed[previous.ID]; renamed {
				target, declared := afterByID[intended.ID]
				if !declared {
					return fmt.Errorf("SQLite artifact planner supports only additive transitions; field %q in %s was removed", previous.ID, location)
				}
				// Only the name may differ: a rename that also changed how the
				// field or its children are stored would leave values where the
				// new config does not read them.
				unrenamed := sqliteAdditiveRules{paths: rules.paths}
				if err := unrenamed.fields(location, []schema.Field{previous}, []schema.Field{sqliteFieldUnderIdentity(target, previous)}); err != nil {
					return fmt.Errorf("field %q cannot become %q as a rename because more than its name changes: %w", previous.Path.String(), target.Path.String(), err)
				}
				delete(afterByID, target.ID)
				continue
			}
			return fmt.Errorf("SQLite artifact planner supports only additive transitions; field %q in %s was removed", previous.ID, location)
		}

		comparison := current
		// Adding an index is physically reconciled from the after manifest.
		// Adding uniqueness is admitted only because the same transaction
		// rebuilds the derived unique-value table and rolls back on duplicates.
		if !previous.Index && comparison.Index {
			comparison.Index = false
		}
		if !previous.Unique && comparison.Unique {
			comparison.Unique = false
		}
		// Requiredness does not change stored JSON. Relaxing it is safe, and
		// the same transaction audits stored values for a field made required.
		if !rules.embedded {
			comparison.Required = previous.Required
		}
		payload := rules
		payload.embedded = true
		var embeddedErr error
		comparison, embeddedErr = embedded.CompareEvolution(previous, comparison, payload.blockTypes)
		if embeddedErr != nil {
			return embeddedErr
		}

		if previous.Type != comparison.Type && (primitivefield.IsList(previous) || primitivefield.IsList(comparison)) {
			return fmt.Errorf("field %q changes value shape from %q to %q; add a new field and migrate existing values explicitly, or register a supported compiled data transform; automatic list conversion is not available", previous.Path.String(), previous.Type, comparison.Type)
		}
		expected := previous
		if previous.Join != nil && len(rules.paths[previous.Join.CollectionID]) != 0 {
			join := *previous.Join
			join.On = rules.renamedPath(join.CollectionID, join.On)
			expected.Join = &join
		}
		if !reflect.DeepEqual(sqliteFieldComparisonMetadata(expected), sqliteFieldComparisonMetadata(comparison)) {
			return fmt.Errorf("SQLite artifact planner supports only additive transitions; field %q in %s changed", previous.ID, location)
		}
		if previous.Nested != nil {
			if err := rules.fields(fmt.Sprintf("field %q in %s", previous.ID, location), previous.Nested.ResolvedFields(), current.Nested.ResolvedFields()); err != nil {
				return err
			}
		}
		if previous.Blocks != nil {
			if err := rules.blockTypes(fmt.Sprintf("field %q in %s", previous.ID, location), previous.Blocks.Definitions(), current.Blocks.Definitions()); err != nil {
				return err
			}
		}
		delete(afterByID, previous.ID)
	}
	if !rules.embedded {
		// An added required field is audited like any field made required.
		return nil
	}
	for _, added := range after {
		if _, remains := afterByID[added.ID]; remains && added.Required && added.Category != schema.FieldCategoryPresentation {
			return fmt.Errorf("SQLite additive migration cannot add required embedded field %q to existing %s without rewriting existing documents", added.ID, location)
		}
	}
	return nil
}

// Child evolution is checked separately. Compare only container metadata here,
// excluding lazy placement bindings and their definition caches.
func sqliteFieldComparisonMetadata(value schema.Field) schema.Field {
	if n := value.Nested; n != nil {
		value.Nested = &schema.NestedField{MinRows: n.MinRows, MaxRows: n.MaxRows, RowLabel: n.RowLabel, RowLabelComponent: n.RowLabelComponent, RowLabels: n.RowLabels}
	}
	if b := value.Blocks; b != nil {
		value.Blocks = &schema.BlocksField{MinRows: b.MinRows, MaxRows: b.MaxRows}
	}
	return value
}

func (rules sqliteAdditiveRules) blockTypes(location string, before, after []schema.BlockType) error {
	afterByKey := make(map[string]schema.BlockType, len(after))
	for _, block := range after {
		afterByKey[block.Slug] = block
	}
	for _, previous := range before {
		current, exists := afterByKey[previous.Slug]
		if !exists {
			return fmt.Errorf("SQLite artifact planner supports only additive transitions; block type %q in %s was removed", previous.Slug, location)
		}
		// A container may only keep or add selections. Each selected
		// definition is validated once, wherever it is placed.
		if !reflect.DeepEqual(previous.Labels, current.Labels) {
			return fmt.Errorf("SQLite artifact planner supports only additive transitions; block type %q in %s changed", previous.Slug, location)
		}
		delete(afterByKey, previous.Slug)
	}
	return nil
}

type sqliteArtifactLedgerRow struct {
	position               int
	name                   string
	digest                 string
	previousArtifactDigest string
	fromDigest             string
	toDigest               string
	plannerName            string
	plannerVersion         string
}

const sqliteArtifactLedgerSQL = `CREATE TABLE IF NOT EXISTS ridu_migrations (
  position INTEGER PRIMARY KEY CHECK (position > 0),
  name TEXT NOT NULL UNIQUE,
  artifact_digest TEXT NOT NULL,
  previous_artifact_digest TEXT NOT NULL,
  from_digest TEXT NOT NULL,
  to_digest TEXT NOT NULL,
  planner_name TEXT NOT NULL,
  planner_version TEXT NOT NULL,
  applied_at INTEGER NOT NULL
) STRICT`

// ApplyArtifacts validates the complete committed history before creating a
// ledger or changing application schema. All pending artifacts and their
// ledger rows commit under one BEGIN IMMEDIATE writer reservation.
func (backend *Store) ApplyArtifacts(ctx context.Context, directory string, transforms ...ridumigration.DataTransform) error {
	registry, err := newSQLiteDataTransformRegistry(transforms)
	if err != nil {
		return err
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return err
	}
	return backend.applySQLiteArtifacts(ctx, files, registry)
}

func (backend *Store) applySQLiteArtifacts(ctx context.Context, files []migrationartifact.File, transforms sqliteDataTransformRegistry) error {
	if len(files) == 0 {
		return fmt.Errorf("migration artifact history is empty; create and commit an initial migration before apply")
	}
	if err := preflightSQLiteArtifacts(ctx, files); err != nil {
		return err
	}
	if err := requireSQLiteDataTransformRegistry(files, transforms); err != nil {
		return err
	}
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		exists, err := sqliteArtifactLedgerExists(ctx, connection)
		if err != nil {
			return err
		}
		var applied []sqliteArtifactLedgerRow
		if exists {
			if err := validateSQLiteArtifactLedgerShape(ctx, connection); err != nil {
				return err
			}
			applied, err = readSQLiteArtifactLedger(ctx, connection)
			if err != nil {
				return err
			}
			if len(applied) == 0 {
				return fmt.Errorf("SQLite migration ledger exists without an applied artifact")
			}
			if err := validateSQLiteArtifactLedgerContinuity(applied); err != nil {
				return err
			}
		}
		if err := validateSQLiteAppliedArtifacts(files, applied); err != nil {
			return err
		}
		if len(applied) == 0 {
			if err := assertNoSQLiteManagedSchema(ctx, connection); err != nil {
				return err
			}
		} else {
			appliedManifest, err := files[len(applied)-1].Artifact.AfterManifest()
			if err != nil {
				return err
			}
			if err := assertSQLitePhysicalSchema(ctx, connection, appliedManifest, true); err != nil {
				return fmt.Errorf("current SQLite migration state: %w", err)
			}
			if err := assertSQLiteManifestDigest(ctx, connection, files[len(applied)-1].Artifact.ToDigest); err != nil {
				return fmt.Errorf("current SQLite migration state: %w", err)
			}
		}
		expectedHead := files[len(files)-1].Digest
		if len(applied) == len(files) {
			if err := writeSQLiteExpectedArtifactDigest(ctx, connection, expectedHead); err != nil {
				return err
			}
			return assertSQLiteExpectedArtifactDigest(ctx, connection, expectedHead)
		}
		if _, err := connection.ExecContext(ctx, sqliteArtifactLedgerSQL); err != nil {
			return fmt.Errorf("create SQLite migration ledger: %w", translateError(err))
		}
		for index := len(applied); index < len(files); index++ {
			if err := backend.applySQLiteArtifact(ctx, connection, files[index], index+1, expectedHead, transforms); err != nil {
				return err
			}
		}
		latestManifest, err := files[len(files)-1].Artifact.AfterManifest()
		if err != nil {
			return err
		}
		if err := assertSQLitePhysicalSchema(ctx, connection, latestManifest, true); err != nil {
			return fmt.Errorf("completed SQLite migration state: %w", err)
		}
		if err := assertSQLiteManifestDigest(ctx, connection, files[len(files)-1].Artifact.ToDigest); err != nil {
			return err
		}
		return assertSQLiteExpectedArtifactDigest(ctx, connection, expectedHead)
	})
}

// preflightSQLiteArtifacts proves that every artifact is exactly what the
// current planner produces from its recorded manifests and intent.
func preflightSQLiteArtifacts(ctx context.Context, files []migrationartifact.File) error {
	if err := validateSQLiteDataTransformIdentities(files, nil); err != nil {
		return err
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if file.Artifact.Planner.Name != sqlitePlannerName {
			return fmt.Errorf("SQLite migration %s uses planner %q instead of %q", file.Name, file.Artifact.Planner.Name, sqlitePlannerName)
		}
		if file.Artifact.Planner.Version != sqlitePlannerVersion {
			return sqliteUnsupportedPlannerVersion("SQLite migration "+file.Name, file.Artifact.Planner.Version)
		}
		for _, phase := range file.Artifact.Phases {
			if phase.Mode != ridumigration.PhaseTransaction {
				return fmt.Errorf("SQLite migration %s uses unsupported phase mode %q", file.Name, phase.Mode)
			}
			for _, step := range phase.Steps {
				if step.Kind != ridumigration.StepSQL && step.Kind != ridumigration.StepDataTransform && step.Kind != ridumigration.StepRenameContent &&
					step.Kind != ridumigration.StepAuditRequiredValues && step.Kind != ridumigration.StepAssertSchema {
					return fmt.Errorf("SQLite migration %s uses unsupported step kind %q", file.Name, step.Kind)
				}
			}
		}
		after, err := file.Artifact.AfterManifest()
		if err != nil {
			return err
		}
		var before *schema.Manifest
		if file.Artifact.Before != nil {
			manifest, err := file.Artifact.BeforeManifest()
			if err != nil {
				return err
			}
			before = &manifest
		}
		descriptors, err := sqliteArtifactDataTransformDescriptors(file.Artifact)
		if err != nil {
			return err
		}
		renames, err := sqliteArtifactRenames(file.Artifact)
		if err != nil {
			return err
		}
		var expected ridumigration.Artifact
		switch {
		case len(renames) != 0 && len(descriptors) != 0:
			err = fmt.Errorf("content renames and data transforms cannot share one SQLite migration")
		case len(renames) != 0:
			expected, err = buildSQLiteArtifactWithRenames(ctx, file.Artifact.Name, before, after, renames)
		default:
			expected, err = buildSQLiteArtifactWithDataTransformDescriptors(ctx, file.Artifact.Name, before, after, descriptors, sqliteArtifactAllowsTransformedSchema(file.Artifact))
		}
		if err != nil {
			return fmt.Errorf("validate SQLite migration %s against planner: %w", file.Name, err)
		}
		expected.PreviousArtifactDigest = file.Artifact.PreviousArtifactDigest
		digest, err := expected.Digest()
		if err != nil {
			return err
		}
		if digest != file.Digest {
			return fmt.Errorf("SQLite migration %s does not match planner %s %s", file.Name, sqlitePlannerName, sqlitePlannerVersion)
		}
	}
	return nil
}

func (backend *Store) applySQLiteArtifact(ctx context.Context, connection *sql.Conn, file migrationartifact.File, position int, expectedHead string, transforms sqliteDataTransformRegistry) error {
	after, err := file.Artifact.AfterManifest()
	if err != nil {
		return err
	}
	var before *schema.Manifest
	if file.Artifact.Before != nil {
		manifest, err := file.Artifact.BeforeManifest()
		if err != nil {
			return err
		}
		before = &manifest
	}
	renamed := false
	for _, phase := range file.Artifact.Phases {
		for _, step := range phase.Steps {
			switch step.Kind {
			case ridumigration.StepRenameContent:
				// The first rename step moves every rename in the artifact in one
				// pass over its documents; the rest are already done.
				if renamed {
					continue
				}
				if err := applySQLiteArtifactRenames(ctx, connection, file.Artifact, before, after, false); err != nil {
					return fmt.Errorf("apply SQLite migration %s content renames: %w", file.Name, err)
				}
				renamed = true
			case ridumigration.StepSQL:
				var payload ridumigration.SQLPayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					return fmt.Errorf("decode SQLite migration %s step %s: %w", file.Name, step.ID, err)
				}
				if _, err := connection.ExecContext(ctx, payload.SQL); err != nil {
					return fmt.Errorf("apply SQLite migration %s step %s (%s): %w", file.Name, step.ID, step.Name, translateError(err))
				}
			case ridumigration.StepDataTransform:
				var payload ridumigration.DataTransformPayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					return fmt.Errorf("decode SQLite migration %s step %s: %w", file.Name, step.ID, err)
				}
				if err := backend.executeSQLiteDataTransform(ctx, connection, file, payload.Transform, transforms, false); err != nil {
					return fmt.Errorf("apply SQLite migration %s step %s (%s): %w", file.Name, step.ID, step.Name, err)
				}
			case ridumigration.StepAuditRequiredValues:
				var payload ridumigration.AuditRequiredValuesPayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					return fmt.Errorf("decode SQLite migration %s step %s: %w", file.Name, step.ID, err)
				}
				requirements, err := sqliteArtifactRequirements(file.Artifact, payload)
				if err == nil {
					err = auditSQLiteRequiredValues(ctx, connection, requirements, false)
				}
				if err != nil {
					return fmt.Errorf("apply SQLite migration %s step %s (%s): %w", file.Name, step.ID, step.Name, err)
				}
			case ridumigration.StepAssertSchema:
				if err := reconcileDocumentIndexes(ctx, connection, after); err != nil {
					return fmt.Errorf("apply SQLite migration %s step %s (%s): %w", file.Name, step.ID, step.Name, err)
				}
				if err := rebuildDocumentReferences(ctx, connection, after); err != nil {
					return fmt.Errorf("apply SQLite migration %s step %s (%s): rebuild references: %w", file.Name, step.ID, step.Name, err)
				}
				if err := rebuildUniqueValues(ctx, connection, after); err != nil {
					return fmt.Errorf("apply SQLite migration %s step %s (%s): rebuild uniqueness: %w", file.Name, step.ID, step.Name, err)
				}
				if err := assertSQLitePhysicalSchema(ctx, connection, after, true); err != nil {
					return fmt.Errorf("apply SQLite migration %s step %s (%s): %w", file.Name, step.ID, step.Name, err)
				}
			default:
				return fmt.Errorf("SQLite migration %s uses unsupported step kind %q", file.Name, step.Kind)
			}
		}
	}
	if err := writeSQLiteManifest(ctx, connection, after, expectedHead, backend.now().UTC()); err != nil {
		return fmt.Errorf("record SQLite manifest for migration %s: %w", file.Name, err)
	}
	_, err = connection.ExecContext(ctx, `INSERT INTO ridu_migrations
  (position, name, artifact_digest, previous_artifact_digest, from_digest, to_digest, planner_name, planner_version, applied_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, position, file.Name, file.Digest, file.Artifact.PreviousArtifactDigest, file.Artifact.FromDigest,
		file.Artifact.ToDigest, file.Artifact.Planner.Name, file.Artifact.Planner.Version, encodeTime(backend.now().UTC()))
	if err != nil {
		return fmt.Errorf("record SQLite migration %s: %w", file.Name, translateError(err))
	}
	return nil
}

func writeSQLiteManifest(ctx context.Context, runner sqlRunner, manifest schema.Manifest, expectedArtifactDigest string, now time.Time) error {
	digest, err := manifestDigest(manifest)
	if err != nil {
		return err
	}
	encoded, err := manifest.Bytes()
	if err != nil {
		return err
	}
	_, err = runner.ExecContext(ctx, `INSERT INTO ridu_sqlite_schema
  (singleton, manifest_digest, expected_artifact_digest, manifest_json, applied_at)
VALUES (1, ?, ?, ?, ?)
ON CONFLICT(singleton) DO UPDATE SET
  manifest_digest = excluded.manifest_digest,
  expected_artifact_digest = excluded.expected_artifact_digest,
  manifest_json = excluded.manifest_json,
  applied_at = excluded.applied_at`, digest, expectedArtifactDigest, string(encoded), encodeTime(now))
	return translateError(err)
}

func writeSQLiteExpectedArtifactDigest(ctx context.Context, runner sqlRunner, expected string) error {
	result, err := runner.ExecContext(ctx, `UPDATE ridu_sqlite_schema SET expected_artifact_digest = ? WHERE singleton = 1`, expected)
	if err != nil {
		return fmt.Errorf("record expected SQLite artifact head: %w", translateError(err))
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("record expected SQLite artifact head: %w", translateError(err))
	}
	if updated != 1 {
		return fmt.Errorf("SQLite schema ledger is missing")
	}
	return nil
}

func sqliteArtifactLedgerExists(ctx context.Context, runner sqlRunner) (bool, error) {
	var count int
	err := runner.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'ridu_migrations'`).Scan(&count)
	return count == 1, translateError(err)
}

func readSQLiteArtifactLedger(ctx context.Context, runner sqlRunner) ([]sqliteArtifactLedgerRow, error) {
	rows, err := runner.QueryContext(ctx, `SELECT position, name, artifact_digest, previous_artifact_digest, from_digest, to_digest,
	  planner_name, planner_version FROM ridu_migrations ORDER BY position`)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	var result []sqliteArtifactLedgerRow
	for rows.Next() {
		var row sqliteArtifactLedgerRow
		if err := rows.Scan(&row.position, &row.name, &row.digest, &row.previousArtifactDigest, &row.fromDigest, &row.toDigest, &row.plannerName, &row.plannerVersion); err != nil {
			return nil, translateError(err)
		}
		if row.position != len(result)+1 {
			return nil, fmt.Errorf("SQLite migration ledger position %d is not continuous after %d", row.position, len(result))
		}
		result = append(result, row)
	}
	return result, translateError(rows.Err())
}

func validateSQLiteAppliedArtifacts(files []migrationartifact.File, applied []sqliteArtifactLedgerRow) error {
	if len(applied) > len(files) {
		return fmt.Errorf("database contains %d applied SQLite migrations but the directory contains only %d", len(applied), len(files))
	}
	for index, row := range applied {
		file := files[index]
		if row.name != file.Name {
			return fmt.Errorf("SQLite migration history diverged at %s; database records %s", file.Name, row.name)
		}
		if row.digest != file.Digest {
			return fmt.Errorf("SQLite migration %s changed after application", file.Name)
		}
		if row.previousArtifactDigest != file.Artifact.PreviousArtifactDigest {
			return fmt.Errorf("SQLite migration %s predecessor differs from the database ledger", file.Name)
		}
		if row.fromDigest != file.Artifact.FromDigest || row.toDigest != file.Artifact.ToDigest {
			return fmt.Errorf("SQLite migration %s manifest lineage differs from the database ledger", file.Name)
		}
		if row.plannerName != file.Artifact.Planner.Name || row.plannerVersion != file.Artifact.Planner.Version {
			return fmt.Errorf("SQLite migration %s planner provenance differs from the database ledger", file.Name)
		}
	}
	return nil
}

func validateSQLiteArtifactLedgerContinuity(applied []sqliteArtifactLedgerRow) error {
	for index, row := range applied {
		if index == 0 {
			if row.previousArtifactDigest != "" {
				return fmt.Errorf("SQLite migration ledger initial artifact has a predecessor")
			}
			if row.fromDigest != "" {
				return fmt.Errorf("SQLite migration ledger does not start from an empty manifest")
			}
			continue
		}
		previous := applied[index-1]
		if row.name <= previous.name {
			return fmt.Errorf("SQLite migration ledger is reordered at %s after %s", row.name, previous.name)
		}
		if row.previousArtifactDigest != previous.digest {
			return fmt.Errorf("SQLite migration ledger artifact history is discontinuous between %s and %s", previous.name, row.name)
		}
		if row.fromDigest != previous.toDigest {
			return fmt.Errorf("SQLite migration ledger manifest history is discontinuous between %s and %s", previous.name, row.name)
		}
	}
	return nil
}

// verifyImmutableReadyState checks the schema ledger digest and physical shape
// in one read transaction. Immutable databases additionally verify artifact
// history, and its digest when expectedHistoryDigest is set; development
// databases intentionally have no ridu_migrations table.
func (backend *Store) verifyImmutableReadyState(ctx context.Context, manifest schema.Manifest, expectedHistoryDigest *string) error {
	connection, err := backend.db.Conn(ctx)
	if err != nil {
		return translateError(err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "BEGIN"); err != nil {
		return translateError(err)
	}
	defer connection.ExecContext(context.Background(), "ROLLBACK")
	exists, err := sqliteArtifactLedgerExists(ctx, connection)
	if err != nil {
		return err
	}
	// The database records the manifest it was brought to. Admin
	// presentation never changes stored data, so it may differ from the
	// executable's without a migration.
	stored, err := readSQLiteDevelopmentManifest(ctx, connection)
	if err != nil {
		return err
	}
	if stored == nil {
		return fmt.Errorf("SQLite schema ledger is missing")
	}
	storedDigest, err := manifestDigest(*stored)
	if err != nil {
		return fmt.Errorf("digest recorded SQLite manifest: %w", err)
	}
	if err := assertSQLiteManifestDigest(ctx, connection, storedDigest); err != nil {
		return err
	}
	if !stored.SameStorage(manifest) {
		expected, err := manifestDigest(manifest)
		if err != nil {
			return fmt.Errorf("digest executable SQLite manifest: %w", err)
		}
		return fmt.Errorf("database manifest digest %s does not match executable digest %s", storedDigest, expected)
	}
	if !exists {
		if expectedHistoryDigest != nil {
			return fmt.Errorf("SQLite migration ledger is missing for executable migration history")
		}
		expectedArtifactDigest, err := readSQLiteExpectedArtifactDigest(ctx, connection)
		if err != nil {
			return err
		}
		if expectedArtifactDigest != "" {
			return fmt.Errorf("SQLite schema ledger expects artifact digest %s but the migration ledger is missing", expectedArtifactDigest)
		}
		if err := assertSQLitePhysicalSchema(ctx, connection, manifest, false); err != nil {
			return fmt.Errorf("SQLite development readiness physical schema: %w", err)
		}
		return nil
	}
	if err := validateSQLiteArtifactLedgerShape(ctx, connection); err != nil {
		return err
	}
	applied, err := readSQLiteArtifactLedger(ctx, connection)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		return fmt.Errorf("SQLite migration ledger exists without an applied artifact")
	}
	if err := validateSQLiteArtifactLedgerContinuity(applied); err != nil {
		return err
	}
	if expectedHistoryDigest != nil {
		if err := validateSQLiteMigrationHistory(applied, manifest, *expectedHistoryDigest); err != nil {
			return err
		}
	}
	head := applied[len(applied)-1]
	if head.toDigest != storedDigest {
		return fmt.Errorf("SQLite migration ledger head %s digest %s does not match the recorded manifest digest %s", head.name, head.toDigest, storedDigest)
	}
	if err := assertSQLiteExpectedArtifactDigest(ctx, connection, head.digest); err != nil {
		return fmt.Errorf("SQLite migration ledger head %s: %w", head.name, err)
	}
	if head.plannerName != sqlitePlannerName {
		return fmt.Errorf("SQLite migration ledger head %s uses planner %q instead of %q", head.name, head.plannerName, sqlitePlannerName)
	}
	if head.plannerVersion != sqlitePlannerVersion {
		return sqliteUnsupportedPlannerVersion("SQLite migration ledger head "+head.name, head.plannerVersion)
	}
	if err := assertSQLitePhysicalSchema(ctx, connection, manifest, true); err != nil {
		return fmt.Errorf("SQLite migration readiness physical schema: %w", err)
	}
	return nil
}

func validateSQLiteMigrationHistory(applied []sqliteArtifactLedgerRow, manifest schema.Manifest, expected string) error {
	identities := make([]ridumigration.ArtifactIdentity, len(applied))
	for index, row := range applied {
		identities[index] = ridumigration.ArtifactIdentity{Name: row.name, Digest: row.digest}
	}
	head := applied[len(applied)-1]
	actual, err := ridumigration.DigestArtifactHistory(identities, head.toDigest, manifest)
	if err != nil {
		return fmt.Errorf("digest applied SQLite migration history: %w", err)
	}
	if actual != expected {
		return fmt.Errorf("applied SQLite migration history digest %s does not match executable history digest %s", actual, expected)
	}
	return nil
}

// ArtifactStatus binds immutable history to the executable schema, then uses
// that exact history snapshot to report the atomic applied/pending boundary.
func (backend *Store) ArtifactStatus(ctx context.Context, directory string, executableManifest schema.Manifest) ([]MigrationStatus, error) {
	files, err := migrationartifact.RequireCurrentHistory(directory, executableManifest)
	if err != nil {
		return nil, err
	}
	return backend.artifactStatus(ctx, files)
}

// InspectArtifacts reports immutable migration state without creating or
// changing the selected SQLite database. Missing files have an all-pending
// state; existing files are opened read-only without adapter write pragmas.
func InspectArtifacts(ctx context.Context, path, directory string, executableManifest schema.Manifest) ([]MigrationStatus, error) {
	files, err := migrationartifact.RequireCurrentHistory(directory, executableManifest)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("migration artifact history is empty; create and commit an initial migration before status")
	}
	if err := preflightSQLiteArtifacts(ctx, files); err != nil {
		return nil, err
	}
	backend, exists, err := openSQLiteArtifactInspection(ctx, path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return sqliteMigrationStatuses(files, 0), nil
	}
	defer backend.Close()
	return backend.artifactStatus(ctx, files)
}

func openSQLiteArtifactInspection(ctx context.Context, path string) (*Store, bool, error) {
	dsn, filePath, memory, err := sqliteDSN(path)
	if err != nil {
		return nil, false, err
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		return nil, false, fmt.Errorf("parse SQLite inspection path: %w", err)
	}
	parameters := parsed.Query()
	if err := rejectProtectedSQLiteParameters(parameters); err != nil {
		return nil, false, err
	}
	if memory {
		return nil, false, nil
	}
	if _, err := os.Stat(filePath); err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("inspect SQLite target: %w", err)
	}
	parameters.Set("mode", "ro")
	parameters.Set("_query_only", "1")
	parsed.RawQuery = parameters.Encode()
	database, err := sql.Open("sqlite", parsed.String())
	if err != nil {
		return nil, false, fmt.Errorf("configure read-only SQLite inspection: %w", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, false, fmt.Errorf("connect read-only SQLite inspection: %w", sanitizeSQLiteError(err))
	}
	return &Store{db: database}, true, nil
}

func (backend *Store) artifactStatus(ctx context.Context, files []migrationartifact.File) ([]MigrationStatus, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("migration artifact history is empty; create and commit an initial migration before status")
	}
	if err := preflightSQLiteArtifacts(ctx, files); err != nil {
		return nil, err
	}
	connection, err := backend.db.Conn(ctx)
	if err != nil {
		return nil, translateError(err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "BEGIN"); err != nil {
		return nil, translateError(err)
	}
	defer connection.ExecContext(context.Background(), "ROLLBACK")
	exists, err := sqliteArtifactLedgerExists(ctx, connection)
	if err != nil {
		return nil, err
	}
	var applied []sqliteArtifactLedgerRow
	if exists {
		if err := validateSQLiteArtifactLedgerShape(ctx, connection); err != nil {
			return nil, err
		}
		applied, err = readSQLiteArtifactLedger(ctx, connection)
		if err != nil {
			return nil, err
		}
		if len(applied) == 0 {
			return nil, fmt.Errorf("SQLite migration ledger exists without an applied artifact")
		}
		if err := validateSQLiteArtifactLedgerContinuity(applied); err != nil {
			return nil, err
		}
	}
	if err := validateSQLiteAppliedArtifacts(files, applied); err != nil {
		return nil, err
	}
	if len(applied) == 0 {
		if err := assertNoSQLiteManagedSchema(ctx, connection); err != nil {
			return nil, err
		}
	} else {
		appliedManifest, err := files[len(applied)-1].Artifact.AfterManifest()
		if err != nil {
			return nil, err
		}
		if err := assertSQLitePhysicalSchema(ctx, connection, appliedManifest, true); err != nil {
			return nil, fmt.Errorf("SQLite migration status physical schema: %w", err)
		}
		if err := assertSQLiteManifestDigest(ctx, connection, files[len(applied)-1].Artifact.ToDigest); err != nil {
			return nil, fmt.Errorf("SQLite migration status manifest: %w", err)
		}
	}
	return sqliteMigrationStatuses(files, len(applied)), nil
}

func sqliteMigrationStatuses(files []migrationartifact.File, appliedCount int) []MigrationStatus {
	statuses := make([]MigrationStatus, len(files))
	for index, file := range files {
		state := "pending"
		if index < appliedCount {
			state = "complete"
		}
		status := MigrationStatus{Name: file.Name, Checksum: file.Digest, Version: file.Artifact.Version, Applied: index < appliedCount}
		for _, phase := range file.Artifact.Phases {
			phaseStatus := MigrationPhaseStatus{ID: phase.ID, Mode: phase.Mode, State: state}
			for _, step := range phase.Steps {
				phaseStatus.Steps = append(phaseStatus.Steps, MigrationStepStatus{ID: step.ID, Kind: step.Kind, State: state})
			}
			status.Phases = append(status.Phases, phaseStatus)
		}
		statuses[index] = status
	}
	return statuses
}

// ArtifactPlan returns the same manifest-bound immutable SQLite topology as status.
func (backend *Store) ArtifactPlan(ctx context.Context, directory string, executableManifest schema.Manifest) ([]MigrationStatus, error) {
	return backend.ArtifactStatus(ctx, directory, executableManifest)
}

// VerifyArtifacts replays complete history into an isolated temporary SQLite
// database and proves that every artifact and the latest manifest are complete.
func VerifyArtifacts(ctx context.Context, directory string, transforms ...ridumigration.DataTransform) error {
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return err
	}
	registry, err := newSQLiteDataTransformRegistry(transforms)
	if err != nil {
		return err
	}
	return verifySQLiteArtifacts(ctx, files, registry)
}

func verifySQLiteArtifacts(ctx context.Context, files []migrationartifact.File, transforms sqliteDataTransformRegistry) error {
	if len(files) == 0 {
		return fmt.Errorf("migration artifact history is empty; create and commit an initial migration before verification")
	}
	if err := preflightSQLiteArtifacts(ctx, files); err != nil {
		return err
	}
	temporary, err := os.MkdirTemp("", "ridu-sqlite-shadow-")
	if err != nil {
		return fmt.Errorf("create SQLite shadow directory: %w", err)
	}
	defer os.RemoveAll(temporary)
	shadow, err := Open(ctx, filepath.Join(temporary, "shadow.sqlite"))
	if err != nil {
		return fmt.Errorf("open SQLite shadow database: %w", err)
	}
	defer shadow.Close()
	if err := shadow.applySQLiteArtifacts(ctx, files, transforms); err != nil {
		return fmt.Errorf("verify SQLite migrations in shadow database: %w", err)
	}
	statuses, err := shadow.artifactStatus(ctx, files)
	if err != nil {
		return fmt.Errorf("verify completed SQLite shadow migration state: %w", err)
	}
	if len(statuses) != len(files) {
		return fmt.Errorf("SQLite shadow replay reported %d of %d migration artifacts", len(statuses), len(files))
	}
	for _, status := range statuses {
		if !status.Applied {
			return fmt.Errorf("SQLite shadow replay left migration %s pending", status.Name)
		}
	}
	latest, err := files[len(files)-1].Artifact.AfterManifest()
	if err != nil {
		return err
	}
	if err := shadow.Ready(ctx, latest); err != nil {
		return fmt.Errorf("verify SQLite shadow readiness: %w", err)
	}
	return nil
}

func withSQLiteMigrationShadow(ctx context.Context, action func(*sql.Conn) error) error {
	shadow, err := Open(ctx, ":memory:")
	if err != nil {
		return err
	}
	defer shadow.Close()
	return shadow.withImmediate(ctx, action)
}

func expectedSQLiteObjects(ctx context.Context, manifest *schema.Manifest, includeArtifactLedger bool) (map[string]string, error) {
	var objects map[string]string
	err := withSQLiteMigrationShadow(ctx, func(database *sql.Conn) error {
		if includeArtifactLedger {
			if _, err := database.ExecContext(ctx, sqliteArtifactLedgerSQL); err != nil {
				return err
			}
		}
		for _, statement := range sqliteSchemaStatements {
			if _, err := database.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("install SQLite expected schema: %w", translateError(err))
			}
		}
		if manifest != nil {
			if err := reconcileDocumentIndexes(ctx, database, *manifest); err != nil {
				return err
			}
		}
		var readError error
		objects, readError = readSQLiteObjects(ctx, database, manifest)
		return readError
	})
	return objects, err
}

type sqliteSchemaObject struct {
	objectType string
	name       string
	table      string
	statement  string
}

func readAllSQLiteSchemaObjects(ctx context.Context, runner sqlRunner) (map[string]sqliteSchemaObject, error) {
	rows, err := runner.QueryContext(ctx, `SELECT type, name, tbl_name, sql FROM sqlite_master
WHERE sql IS NOT NULL ORDER BY type, name`)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	objects := make(map[string]sqliteSchemaObject)
	for rows.Next() {
		var object sqliteSchemaObject
		if err := rows.Scan(&object.objectType, &object.name, &object.table, &object.statement); err != nil {
			return nil, translateError(err)
		}
		object.statement = normalizeSQLiteSchemaSQL(object.statement)
		objects[object.objectType+":"+object.name] = object
	}
	return objects, translateError(rows.Err())
}

func readSQLiteObjects(ctx context.Context, runner sqlRunner, manifest *schema.Manifest) (map[string]string, error) {
	all, err := readAllSQLiteSchemaObjects(ctx, runner)
	if err != nil {
		return nil, err
	}
	objects := make(map[string]string)
	for key, object := range all {
		if !strings.HasPrefix(object.name, "ridu_") && !strings.HasPrefix(object.table, "ridu_") {
			continue
		}
		objects[key] = object.statement
	}
	return objects, nil
}

func normalizeSQLiteSchemaSQL(statement string) string {
	var normalized strings.Builder
	normalized.Grow(len(statement))
	pendingSpace := false
	quote := byte(0)
	lineComment := false
	blockComment := false
	for index := 0; index < len(statement); index++ {
		current := statement[index]
		if lineComment {
			normalized.WriteByte(current)
			if current == '\n' || current == '\r' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			normalized.WriteByte(current)
			if current == '*' && index+1 < len(statement) && statement[index+1] == '/' {
				normalized.WriteByte('/')
				index++
				blockComment = false
			}
			continue
		}
		if quote != 0 {
			normalized.WriteByte(current)
			if quote == '[' {
				if current == ']' {
					quote = 0
				}
				continue
			}
			if current == quote {
				if index+1 < len(statement) && statement[index+1] == quote {
					normalized.WriteByte(statement[index+1])
					index++
				} else {
					quote = 0
				}
			}
			continue
		}
		if isSQLiteSchemaSpace(current) {
			pendingSpace = normalized.Len() != 0
			continue
		}
		if pendingSpace {
			normalized.WriteByte(' ')
			pendingSpace = false
		}
		if current == '-' && index+1 < len(statement) && statement[index+1] == '-' {
			normalized.WriteString("--")
			index++
			lineComment = true
			continue
		}
		if current == '/' && index+1 < len(statement) && statement[index+1] == '*' {
			normalized.WriteString("/*")
			index++
			blockComment = true
			continue
		}
		normalized.WriteByte(current)
		if current == '\'' || current == '"' || current == '`' || current == '[' {
			quote = current
		}
	}
	return normalized.String()
}

func isSQLiteSchemaSpace(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	default:
		return false
	}
}

func validateSQLiteArtifactLedgerShape(ctx context.Context, runner sqlRunner) error {
	expected, err := expectedSQLiteObjects(ctx, nil, true)
	if err != nil {
		return err
	}
	var statement string
	if err := runner.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'ridu_migrations'`).Scan(&statement); err != nil {
		return fmt.Errorf("read SQLite migration ledger shape: %w", translateError(err))
	}
	if normalizeSQLiteSchemaSQL(statement) != expected["table:ridu_migrations"] {
		return fmt.Errorf("SQLite migration ledger is malformed")
	}
	return nil
}

func assertNoSQLiteManagedSchema(ctx context.Context, runner sqlRunner) error {
	rows, err := runner.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE name GLOB 'ridu_*' ORDER BY name`)
	if err != nil {
		return translateError(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return translateError(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return translateError(err)
	}
	if len(names) != 0 {
		return ridumigration.UnmanagedSchemaError("SQLite", names)
	}
	return nil
}

func assertSQLitePhysicalSchema(ctx context.Context, runner sqlRunner, manifest schema.Manifest, includeArtifactLedger bool) error {
	expected, err := expectedSQLiteObjects(ctx, &manifest, includeArtifactLedger)
	if err != nil {
		return err
	}
	actual, err := readSQLiteObjects(ctx, runner, &manifest)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(expected, actual) {
		return nil
	}
	keys := make([]string, 0, len(expected)+len(actual))
	seen := make(map[string]bool, len(expected)+len(actual))
	for key := range expected {
		seen[key] = true
		keys = append(keys, key)
	}
	for key := range actual {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		expectedSQL, expectedExists := expected[key]
		actualSQL, actualExists := actual[key]
		switch {
		case !actualExists:
			return fmt.Errorf("SQLite physical schema drift: required object %s is missing", key)
		case !expectedExists:
			if strings.HasPrefix(key, "index:") {
				name := strings.TrimPrefix(key, "index:")
				ordinary, err := sqliteUnexpectedIndexIsOrdinary(ctx, runner, name)
				if err != nil {
					return err
				}
				if ordinary && !sqliteObjectNameIsReserved(name) {
					continue
				}
			}
			return fmt.Errorf("SQLite physical schema drift: unexpected behavior-changing object %s exists", key)
		case actualSQL != expectedSQL:
			return fmt.Errorf("SQLite physical schema drift: object %s differs from the adapter contract", key)
		}
	}
	return nil
}

func sqliteObjectNameIsReserved(name string) bool {
	const prefix = "ridu_"
	return len(name) >= len(prefix) && strings.EqualFold(name[:len(prefix)], prefix)
}

func sqliteUnexpectedIndexIsOrdinary(ctx context.Context, runner sqlRunner, name string) (bool, error) {
	var table string
	if err := runner.QueryRowContext(ctx, `SELECT tbl_name FROM main.sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&table); err != nil {
		return false, fmt.Errorf("inspect unexpected SQLite index %q: %w", name, translateError(err))
	}
	var unique, partial int
	if err := runner.QueryRowContext(ctx, `SELECT "unique", partial FROM pragma_index_list(?, 'main') WHERE name = ?`, table, name).Scan(&unique, &partial); err != nil {
		return false, fmt.Errorf("inspect unexpected SQLite index %q: %w", name, translateError(err))
	}
	if unique != 0 || partial != 0 {
		return false, nil
	}
	rows, err := runner.QueryContext(ctx, `SELECT cid, coll, "key" FROM pragma_index_xinfo(?, 'main')`, name)
	if err != nil {
		return false, fmt.Errorf("inspect unexpected SQLite index %q: %w", name, translateError(err))
	}
	defer rows.Close()
	foundKey := false
	for rows.Next() {
		var cid, key int
		var collation sql.NullString
		if err := rows.Scan(&cid, &collation, &key); err != nil {
			return false, fmt.Errorf("inspect unexpected SQLite index %q: %w", name, translateError(err))
		}
		if key == 0 {
			continue
		}
		foundKey = true
		if cid < 0 || !sqliteBuiltInIndexCollation(collation.String) {
			return false, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("inspect unexpected SQLite index %q: %w", name, translateError(err))
	}
	return foundKey, nil
}

func sqliteBuiltInIndexCollation(value string) bool {
	switch strings.ToUpper(value) {
	case "BINARY", "NOCASE", "RTRIM":
		return true
	default:
		return false
	}
}

func assertSQLiteManifestDigest(ctx context.Context, runner sqlRunner, expected string) error {
	var actual string
	err := runner.QueryRowContext(ctx, `SELECT manifest_digest FROM ridu_sqlite_schema WHERE singleton = 1`).Scan(&actual)
	if err == sql.ErrNoRows || isNoSuchTable(err) {
		return fmt.Errorf("SQLite schema ledger is missing")
	}
	if err != nil {
		return fmt.Errorf("read SQLite schema ledger: %w", translateError(err))
	}
	if actual != expected {
		return fmt.Errorf("database manifest digest %s does not match executable digest %s", actual, expected)
	}
	return nil
}

func readSQLiteExpectedArtifactDigest(ctx context.Context, runner sqlRunner) (string, error) {
	var actual string
	err := runner.QueryRowContext(ctx, `SELECT expected_artifact_digest FROM ridu_sqlite_schema WHERE singleton = 1`).Scan(&actual)
	if err == sql.ErrNoRows || isNoSuchTable(err) {
		return "", fmt.Errorf("SQLite schema ledger is missing")
	}
	if err != nil {
		return "", fmt.Errorf("read expected SQLite artifact head: %w", translateError(err))
	}
	return actual, nil
}

func assertSQLiteExpectedArtifactDigest(ctx context.Context, runner sqlRunner, expected string) error {
	actual, err := readSQLiteExpectedArtifactDigest(ctx, runner)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("expected artifact digest %s does not match applied artifact digest %s", actual, expected)
	}
	return nil
}
