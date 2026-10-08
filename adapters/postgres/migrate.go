package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	atlaspostgres "ariga.io/atlas/sql/postgres"
	atlasschema "ariga.io/atlas/sql/schema"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/riducms/ridu/internal/primitivefield"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// developmentStatement is one ordered SQL change in a development schema
// plan. Only SyncDevelopmentSchema applies one, after auditing stored content.
type developmentStatement struct {
	// kind is a stable, human-readable description of the planned change.
	kind string
	// sql is the complete PostgreSQL statement to apply.
	sql string
}

// RenameKind identifies the address whose storage identity changes.
type RenameKind string

const (
	// RenameCollection preserves a collection while changing its derived identity.
	RenameCollection RenameKind = "collection"
	// RenameField preserves a field while changing its derived identity.
	RenameField RenameKind = "field"
	// RenameBlockField preserves a block definition's field at every
	// placement of the block while changing its name.
	RenameBlockField RenameKind = "block-field"
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
	// Block names the block definition of a block field rename, whose
	// collections are zero and whose fields are the definition's.
	Block string
	// BeforeField and AfterField are set for a standalone field rename.
	BeforeField *schema.Field
	AfterField  *schema.Field
	// Fields contains a collection rename's own physical and nested field pairs.
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

// VerifySchema reports, without changing anything, whether the physical
// PostgreSQL schema exactly matches manifest. It is the check readiness and
// the migration runner apply; schema changes go only through
// SyncDevelopmentSchema or immutable artifacts, which also audit stored
// content against the new schema.
func (backend *Store) VerifySchema(ctx context.Context, manifest schema.Manifest) error {
	if err := primitivefield.ValidateManifestIndexes(manifest); err != nil {
		return err
	}
	database := stdlib.OpenDB(*backend.pool.Config().ConnConfig)
	defer database.Close()
	connection, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	return verifyPostgresPhysicalState(ctx, connection, &manifest)
}

// planPostgresDevelopmentSchema returns the safe Atlas changes that bring the
// physical schema to manifest. It refuses destructive changes. Its caller has
// already refused to start keeping versions on a resource that stores
// documents, so a newly versioned resource gets its live table here.
func planPostgresDevelopmentSchema(ctx context.Context, transaction *sql.Tx, manifest schema.Manifest) ([]developmentStatement, error) {
	previous, err := readPostgresDevelopmentManifest(ctx, transaction)
	if err != nil {
		return nil, err
	}
	if previous != nil {
		referencesCurrent, err := transactionColumnExists(ctx, transaction, "ridu_document_references", "published_head")
		if err != nil {
			return nil, err
		}
		previousSnapshot := previous.Snapshot()
		previousResources := append(append([]schema.Collection(nil), previousSnapshot.Collections...), previousSnapshot.Globals...)
		// Every versioned resource of the current layout has a typed live table.
		liveTablesCurrent := true
		for _, resource := range previousResources {
			if resource.Versions == nil || !liveTablesCurrent {
				continue
			}
			liveTablesCurrent, err = transactionTableExists(ctx, transaction, publishedCollectionTable(resource.ID))
			if err != nil {
				return nil, err
			}
		}
		if !referencesCurrent || !liveTablesCurrent {
			return nil, fmt.Errorf("unsupported PostgreSQL development schema layout; recreate this development database with the current schema")
		}
	}
	driver, err := atlaspostgres.Open(transaction)
	if err != nil {
		return nil, fmt.Errorf("open Atlas development inspector: %w", err)
	}
	actual, err := driver.InspectSchema(ctx, "", &atlasschema.InspectOptions{Mode: atlasschema.InspectTables})
	if err != nil {
		return nil, fmt.Errorf("inspect development schema: %w", err)
	}
	for index := len(actual.Tables) - 1; index >= 0; index-- {
		if postgresSchemaMetadataTable(actual.Tables[index].Name) {
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
	statements := make([]developmentStatement, 0, len(steps))
	for _, step := range steps {
		if step.Kind == ridumigration.StepSQL {
			statements = append(statements, developmentStatement{kind: step.Name, sql: step.SQL})
		}
	}
	return statements, nil
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
