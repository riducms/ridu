package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	atlaspostgres "ariga.io/atlas/sql/postgres"
	atlasschema "ariga.io/atlas/sql/schema"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPostgresDurableTaskPhysicalSchemaHasNoInspectorDrift(t *testing.T) {
	backend := migrationArtifactTestBackend(t)
	ctx := context.Background()
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion,
		Application: schema.Application{
			Name: "Durable task physical schema",
		},
		Collections: []schema.Collection{{
			ID: "posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"}, Fields: []schema.Field{},
		}},
		Plugins: []schema.Plugin{},
	})
	expected := atlasSchema(manifest, atlasIdentityMap{})
	changes, err := atlaspostgres.DefaultDiff.SchemaDiff(atlasschema.New("public"), expected)
	if err != nil {
		t.Fatal(err)
	}
	steps, _, err := atlasSteps(ctx, "task-schema", changes)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if _, err := backend.pool.Exec(ctx, step.SQL); err != nil {
			t.Fatalf("apply %s: %v\n%s", step.Name, err, step.SQL)
		}
	}
	if plan, err := backend.Plan(ctx, manifest); err != nil || len(plan) != 0 {
		rows, queryErr := backend.pool.Query(ctx, `SELECT indexname, indexdef FROM pg_indexes WHERE schemaname = current_schema() AND tablename = 'ridu_tasks' ORDER BY indexname`)
		if queryErr == nil {
			defer rows.Close()
			for rows.Next() {
				var name, definition string
				if scanErr := rows.Scan(&name, &definition); scanErr == nil {
					t.Logf("inspected task index %s: %s", name, definition)
				}
			}
		}
		t.Fatalf("post-task-schema plan = %#v, %v", plan, err)
	}
	base := `INSERT INTO ridu_tasks (
  id, task_slug, queue, input, output, state, run_at, max_attempts,
  retry_delay_ms, max_retry_delay_ms, backoff, timeout_ms, retention_ms,
  completed_at, retain_until, last_error
) VALUES ($1, $2, 'default', $3, $4, $5, now(), 3, 1000, 60000, 'fixed', $6, $7, $8, $9, $10)`
	tests := []struct {
		name string
		args []any
	}{
		{name: "payload", args: []any{"task-oversized-payload", "bounded-task", json.RawMessage(`"` + strings.Repeat("x", store.MaxTaskPayloadBytes) + `"`), nil, "queued", int64(time.Minute / time.Millisecond), int64(time.Hour / time.Millisecond), nil, nil, nil}},
		{name: "identifier", args: []any{"task-oversized-identifier", strings.Repeat("a", 129), json.RawMessage(`{}`), nil, "queued", int64(time.Minute / time.Millisecond), int64(time.Hour / time.Millisecond), nil, nil, nil}},
		{name: "retention", args: []any{"task-oversized-retention", "bounded-task", json.RawMessage(`{}`), nil, "queued", int64(time.Minute / time.Millisecond), store.MaxTaskRetention.Milliseconds() + 1, nil, nil, nil}},
		{name: "error", args: []any{"task-oversized-error", "bounded-task", json.RawMessage(`{}`), nil, "queued", int64(time.Minute / time.Millisecond), int64(time.Hour / time.Millisecond), nil, nil, strings.Repeat("x", store.MaxTaskErrorBytes+1)}},
		{name: "output state", args: []any{"task-invalid-output-state", "bounded-task", json.RawMessage(`{}`), json.RawMessage(`{}`), "queued", int64(time.Minute / time.Millisecond), int64(time.Hour / time.Millisecond), nil, nil, nil}},
	}
	for _, test := range tests {
		t.Run("rejects direct "+test.name, func(t *testing.T) {
			if _, err := backend.pool.Exec(ctx, base, test.args...); err == nil {
				t.Fatalf("direct SQL %s violation was accepted", test.name)
			}
		})
	}
}
