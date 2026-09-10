package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	atlaspostgres "ariga.io/atlas/sql/postgres"
	atlasschema "ariga.io/atlas/sql/schema"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/riducms/ridu/internal/primitivefield"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// Statement is one ordered SQL change in a development schema plan.
type Statement struct {
	// Kind is a stable, human-readable description of the planned change.
	Kind string
	// SQL is the complete PostgreSQL statement to apply.
	SQL string
	// CollectionID identifies the collection affected by the statement, when any.
	CollectionID schema.StableID
	// FieldID identifies the top-level field affected by the statement, when any.
	FieldID schema.StableID
}

// RenameKind identifies the address whose storage identity changes.
type RenameKind string

const (
	// RenameCollection preserves a collection while changing its derived identity.
	RenameCollection RenameKind = "collection"
	// RenameField preserves a field while changing its derived identity.
	RenameField RenameKind = "field"
)

// FieldRename relates one field before and after a confirmed rename.
type FieldRename struct {
	// Before is the field embedded in the previous migration artifact.
	Before schema.Field
	// After is its confirmed match in current executable config.
	After schema.Field
}

// Rename records migration-time identity intent confirmed by the application author.
type Rename struct {
	// Kind selects collection or field rename planning.
	Kind RenameKind
	// BeforeCollection is the collection embedded in the previous artifact.
	BeforeCollection schema.Collection
	// AfterCollection is its collection in current executable config.
	AfterCollection schema.Collection
	// BeforeField and AfterField are set for a standalone field rename.
	BeforeField *schema.Field
	AfterField  *schema.Field
	// Fields contains all physical and nested field pairs for a collection rename.
	Fields []FieldRename
}

// MigrationStatus describes one immutable artifact relative to the database ledger.
type MigrationStatus struct {
	// Name is the complete artifact filename.
	Name string `json:"name"`
	// Checksum is the canonical artifact SHA-256 digest.
	Checksum string `json:"checksum"`
	// Version is the frozen artifact wire version.
	Version uint32 `json:"version"`
	// Applied reports whether the database ledger contains this artifact.
	Applied bool `json:"applied"`
	// Phases exposes resumable progress.
	Phases []MigrationPhaseStatus `json:"phases,omitempty"`
}

// MigrationPhaseStatus describes one immutable execution boundary.
type MigrationPhaseStatus struct {
	ID    string                  `json:"id"`
	Mode  ridumigration.PhaseMode `json:"mode"`
	State string                  `json:"state"`
	Steps []MigrationStepStatus   `json:"steps"`
}

// MigrationStepStatus describes one stable executor and its durable checkpoint.
type MigrationStepStatus struct {
	ID         string                 `json:"id"`
	Kind       ridumigration.StepKind `json:"kind"`
	State      string                 `json:"state"`
	Checkpoint json.RawMessage        `json:"checkpoint,omitempty"`
}

// Plan inspects the current PostgreSQL schema and returns only a safe
// development synchronization plan. Production history uses BuildArtifact.
func (backend *Store) Plan(ctx context.Context, manifest schema.Manifest) ([]Statement, error) {
	if err := primitivefield.ValidateManifestIndexes(manifest); err != nil {
		return nil, err
	}
	database := stdlib.OpenDB(*backend.pool.Config().ConnConfig)
	defer database.Close()
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer transaction.Rollback()
	driver, err := atlaspostgres.Open(transaction)
	if err != nil {
		return nil, fmt.Errorf("open Atlas development inspector: %w", err)
	}
	actual, err := driver.InspectSchema(ctx, "", &atlasschema.InspectOptions{Mode: atlasschema.InspectTables})
	if err != nil {
		return nil, fmt.Errorf("inspect development schema: %w", err)
	}
	for index := len(actual.Tables) - 1; index >= 0; index-- {
		if actual.Tables[index].Name == "ridu_migrations" || actual.Tables[index].Name == "ridu_migration_steps" {
			actual.Tables = append(actual.Tables[:index], actual.Tables[index+1:]...)
		}
	}
	expected := atlasSchema(manifest, atlasIdentityMap{})
	expected.Name = actual.Name
	for _, table := range expected.Tables {
		table.Schema = expected
	}
	changes, err := atlaspostgres.DefaultDiff.SchemaDiff(actual, expected)
	if err != nil {
		return nil, fmt.Errorf("Atlas development diff: %w", err)
	}
	steps, risks, err := atlasSteps(ctx, "development-sync", changes)
	if err != nil {
		return nil, err
	}
	risks = normalizeRisks(append(physicalChangeRisks(changes), risks...))
	var blocked []ridumigration.Risk
	for _, risk := range risks {
		if risk.Level == ridumigration.RiskDestructive {
			blocked = append(blocked, risk)
		}
	}
	if len(blocked) != 0 {
		return nil, &SafetyError{Risks: blocked}
	}
	statements := make([]Statement, 0, len(steps))
	for _, step := range steps {
		if step.Kind == ridumigration.StepSQL {
			statements = append(statements, Statement{Kind: step.Name, SQL: step.SQL})
		}
	}
	return statements, nil
}

// ApplyPlan applies an Atlas-planned, non-destructive development sync in one
// transaction. It is intentionally separate from production artifact history.
func (backend *Store) ApplyPlan(ctx context.Context, statements []Statement) error {
	transaction, err := backend.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = rollbackPostgresTransaction(ctx, transaction) }()
	if _, err := transaction.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrationLockID); err != nil {
		return err
	}
	for _, statement := range statements {
		if _, err := transaction.Exec(ctx, statement.SQL); err != nil {
			return fmt.Errorf("apply %s: %w", statement.Kind, err)
		}
	}
	return transaction.Commit(ctx)
}

func hasForeignKey(field schema.Field) bool {
	return field.Type == schema.FieldTypeRelationship && field.Relationship != nil && !field.Relationship.HasMany && !field.Relationship.Polymorphic ||
		field.Type == schema.FieldTypeUpload && field.Upload != nil && !field.Upload.HasMany
}

func statementFieldKey(collectionID, fieldID schema.StableID) string {
	return string(collectionID) + ":" + string(fieldID)
}

func columnType(field schema.Field) string {
	if isJSONStoredField(field) {
		return "jsonb"
	}
	if field.Type == schema.FieldTypeNumber {
		return "double precision"
	}
	if field.Type == schema.FieldTypeCheckbox {
		return "boolean"
	}
	return "text"
}
