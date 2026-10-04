package postgres

import (
	"context"
	"strings"
	"testing"

	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

func TestPostgresArtifactRejectsEnablingVersionsOnExistingCollection(t *testing.T) {
	before := atlasTestManifest(atlasTextField("posts-title", "title"))
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections[0].Versions = &schema.VersionSettings{Drafts: true, MaxPerDocument: 2}
	afterSnapshot.Collections[0].Capabilities.Versions = true
	after := schema.NewManifest(afterSnapshot)
	for _, allowDestructive := range []bool{false, true} {
		_, err := BuildArtifact(context.Background(), "enable-versions", &before, after, nil, allowDestructive)
		if err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_ENABLE_UNSUPPORTED") {
			t.Fatalf("allowDestructive=%t: %v", allowDestructive, err)
		}
	}
	forged, err := ridumigration.NewArtifact("forged-enable-versions", atlasPlanner(), &before, after)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePostgresCapabilityDecreasePreflight(forged); err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_ENABLE_UNSUPPORTED") {
		t.Fatalf("runner preflight accepted forged version enable: %v", err)
	}
	if _, err := BuildArtifact(context.Background(), "initial-versioned", nil, after, nil, false); err != nil {
		t.Fatalf("initial versioned resource: %v", err)
	}
}

func TestPostgresDevelopmentRejectsEnablingVersionsBeforePlanning(t *testing.T) {
	backend := migrationArtifactTestBackend(t)
	before := atlasTestManifest(atlasTextField("posts-title", "title"))
	if err := backend.SyncDevelopmentSchema(t.Context(), before); err != nil {
		t.Fatal(err)
	}
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections[0].Versions = &schema.VersionSettings{Drafts: true, MaxPerDocument: 2}
	afterSnapshot.Collections[0].Capabilities.Versions = true
	after := schema.NewManifest(afterSnapshot)
	if _, err := backend.Plan(t.Context(), after); err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_ENABLE_UNSUPPORTED") {
		t.Fatalf("development plan: %v", err)
	}
	if err := backend.SyncDevelopmentSchema(t.Context(), after); err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_ENABLE_UNSUPPORTED") {
		t.Fatalf("development sync: %v", err)
	}
	if recorded, exists, err := backend.DevelopmentManifest(t.Context()); err != nil || !exists || !recorded.Equal(before) {
		t.Fatalf("refused sync changed authoritative manifest: exists=%t, error=%v", exists, err)
	}
}
