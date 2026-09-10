package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/tests/contracts/primitivelists"
)

func TestPostgresPrimitiveListArtifactPhysicalSchema(t *testing.T) {
	backend := migrationArtifactTestBackend(t)
	config := primitivelists.Config()
	config.Collections[0].Fields = append(config.Collections[0].Fields,
		field.TextList("readOnlyPoints").Default("Fixed", "Fixed"),
		field.NumberList("lockedSizes").Default(0, 8),
		field.TextList("privatePoints").Default("Hidden"),
		field.TextList("quotedPoints").Default("O'Reilly", `a\b`, "\n", `"quoted"`),
		field.NumberList("exponentSizes").Default(1e21, 1e-7, 1.25),
	)
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := BuildArtifact(t.Context(), "initial", nil, manifest, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if _, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(t.Context(), directory); err != nil {
		t.Fatal(err)
	}
	if err := backend.Ready(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	collection := manifest.Snapshot().Collections[0]
	var column string
	for _, field := range collection.Fields {
		if field.Name == "lockedSizes" {
			column = fieldColumn(field.ID)
		}
	}
	if column == "" {
		t.Fatal("missing number-list field")
	}
	if _, err := backend.pool.Exec(t.Context(), "ALTER TABLE "+quote(collectionTable(collection.ID))+" ALTER COLUMN "+quote(column)+" SET DEFAULT '[0,9]'::jsonb"); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(t.Context(), directory); err == nil || !strings.Contains(err.Error(), "physical schema drift") || !strings.Contains(err.Error(), column) {
		t.Fatalf("changed list default was not diagnosed: %v", err)
	}
}
