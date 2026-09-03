package migration

import (
	"encoding/json"
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestRetireResourcesPayloadRequiresCanonicalStableIDsAndTransaction(t *testing.T) {
	step := func(payload string) Step {
		return Step{Kind: StepRetireResources, Payload: json.RawMessage(payload)}
	}
	if err := validateStepPayload(PhaseTransaction, step(`{"resourceIds":["global-old","users-old"]}`)); err != nil {
		t.Fatalf("canonical resource retirement payload: %v", err)
	}
	for name, candidate := range map[string]struct {
		mode    PhaseMode
		payload string
	}{
		"empty":                     {PhaseTransaction, `{"resourceIds":[]}`},
		"duplicate":                 {PhaseTransaction, `{"resourceIds":["users-old","users-old"]}`},
		"not sorted":                {PhaseTransaction, `{"resourceIds":["users-old","global-old"]}`},
		"invalid ID":                {PhaseTransaction, `{"resourceIds":["Users"]}`},
		"version owners not sorted": {PhaseTransaction, `{"resourceIds":["users-old"],"purgeVersionOwnerIds":["posts","entries"]}`},
		"version owner overlaps":    {PhaseTransaction, `{"resourceIds":["users-old"],"purgeVersionOwnerIds":["users-old"]}`},
		"unknown field":             {PhaseTransaction, `{"resourceIds":["users-old"],"future":true}`},
		"batch phase":               {PhaseBatch, `{"resourceIds":["users-old"]}`},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateStepPayload(candidate.mode, step(candidate.payload)); err == nil {
				t.Fatal("invalid resource retirement payload was admitted")
			}
		})
	}

	payload, err := MarshalStepPayload(RetireResourcesPayload{
		ResourceIDs: []schema.StableID{"global-old", "users-old"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != `{"resourceIds":["global-old","users-old"]}` {
		t.Fatalf("canonical resource retirement payload = %s", payload)
	}
}

func TestPluginStepsAreBoundToTheArtifactPlannerAdapter(t *testing.T) {
	migration := schema.PluginMigration{
		Version: 1, Name: "create-state", UpSQL: []string{"CREATE TABLE ridu_plugin_search_state (id TEXT)"}, DownSQL: []string{"DROP TABLE ridu_plugin_search_state"},
	}
	artifact := Artifact{
		Planner: Planner{Name: "atlas", Version: "1.0.0"},
		After: schema.Snapshot{Plugins: []schema.Plugin{{
			Key: "search",
			DatabaseContributions: []schema.PluginDatabaseContribution{
				{Adapter: schema.PluginDatabaseAdapterPostgres, Migrations: []schema.PluginMigration{migration}},
				{Adapter: schema.PluginDatabaseAdapterSQLite, Migrations: []schema.PluginMigration{migration}},
			},
		}}},
	}
	step := PluginStep{
		Adapter: schema.PluginDatabaseAdapterSQLite, Plugin: "search", Version: 1, Direction: "up", SQL: append([]string(nil), migration.UpSQL...),
	}
	step.Checksum = PluginStepChecksum(step.Adapter, step.Plugin, step.Version, step.Direction, step.SQL)
	if err := artifact.validatePluginSteps([]PluginStep{step}); err == nil {
		t.Fatal("PostgreSQL planner accepted SQLite plugin SQL")
	}
}
