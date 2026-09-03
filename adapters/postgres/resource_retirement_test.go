package postgres

import (
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestFrameworkResourceRetirementAuditsEverySharedAtlasTable(t *testing.T) {
	versions := &schema.VersionSettings{Drafts: true, MaxPerDocument: 10}
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion,
		Collections: []schema.Collection{{
			ID: "users", Capabilities: schema.Capabilities{Auth: true, Versions: true, Locking: true},
			Auth: &schema.AuthSettings{IdentityField: "email"}, Versions: versions,
			DocumentLock: &schema.DocumentLockSettings{DurationSeconds: 60}, Fields: []schema.Field{},
		}},
		Globals: []schema.Global{{
			ID: "global-settings", Capabilities: schema.Capabilities{Global: true, Versions: true},
			Versions: versions, Fields: []schema.Field{},
		}},
		Plugins: []schema.Plugin{},
	})
	resourceTables := map[string]bool{
		collectionTable("users"):           true,
		collectionTable("global-settings"): true,
	}
	shared := make(map[string]bool)
	rateLimitSeen := false
	for _, table := range atlasSchema(manifest, atlasIdentityMap{}).Tables {
		if resourceTables[table.Name] {
			continue
		}
		if table.Name == "ridu_auth_rate_limits" {
			rateLimitSeen = true
			continue
		}
		shared[table.Name] = true
	}
	if !rateLimitSeen {
		t.Fatal("complete Atlas fixture omitted the intentionally resource-agnostic auth rate-limit table")
	}
	for _, statement := range frameworkResourceRetirementStatements {
		if statement.table == "ridu_auth_rate_limits" {
			t.Fatal("resource retirement must not globally weaken unrelated authentication rate limits")
		}
		if !shared[statement.table] {
			t.Fatalf("resource retirement references non-resource shared table %q", statement.table)
		}
		delete(shared, statement.table)
	}
	if len(shared) != 0 {
		t.Fatalf("framework shared tables lack an explicit resource-retirement policy: %#v", shared)
	}
}
