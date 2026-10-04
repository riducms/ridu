package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestSQLiteRejectsEnablingVersionsOnExistingCollection(t *testing.T) {
	before := sqliteMigrationManifest(t, false)
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections[0].Versions = &schema.VersionSettings{Drafts: true, MaxPerDocument: 2}
	afterSnapshot.Collections[0].Capabilities.Versions = true
	after := schema.NewManifest(afterSnapshot)
	if _, err := planArtifact(context.Background(), "enable-versions", &before, after, true); err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_ENABLE_UNSUPPORTED") {
		t.Fatalf("immutable planner: %v", err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.Migrate(t.Context(), before); err != nil {
		t.Fatal(err)
	}
	if err := backend.Migrate(t.Context(), after); err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_ENABLE_UNSUPPORTED") {
		t.Fatalf("development sync: %v", err)
	}
	if recorded, exists, err := backend.DevelopmentManifest(t.Context()); err != nil || !exists || !recorded.Equal(before) {
		t.Fatalf("refused sync changed authoritative manifest: exists=%t, error=%v", exists, err)
	}
}
