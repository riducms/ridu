package mongodb

import (
	"context"
	"strings"
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestMongoDBArtifactRejectsEnablingVersionsOnExistingCollection(t *testing.T) {
	before := mongoDevelopmentSchemaTestManifest(t)
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections[0].Versions = &schema.VersionSettings{Drafts: true, MaxPerDocument: 2}
	afterSnapshot.Collections[0].Capabilities.Versions = true
	after := schema.NewManifest(afterSnapshot)
	_, err := buildMongoDBArtifact(context.Background(), "enable-versions", &before, after, ArtifactOptions{AllowDestructive: true})
	if err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_ENABLE_UNSUPPORTED") {
		t.Fatalf("immutable planner: %v", err)
	}
	if _, err := buildMongoDBArtifact(context.Background(), "initial-versioned", nil, after, ArtifactOptions{}); err != nil {
		t.Fatalf("initial versioned resource: %v", err)
	}
}

func TestMongoDBDevelopmentRejectsEnablingVersions(t *testing.T) {
	backend := mongoIntegrationStore(t)
	before := mongoDevelopmentSchemaTestManifest(t)
	if err := backend.SyncDevelopmentSchema(t.Context(), before); err != nil {
		t.Fatal(err)
	}
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections[0].Versions = &schema.VersionSettings{Drafts: true, MaxPerDocument: 2}
	afterSnapshot.Collections[0].Capabilities.Versions = true
	after := schema.NewManifest(afterSnapshot)
	if err := backend.SyncDevelopmentSchema(t.Context(), after); err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_ENABLE_UNSUPPORTED") {
		t.Fatalf("development sync: %v", err)
	}
	if recorded, exists, err := backend.DevelopmentManifest(t.Context()); err != nil || !exists || !recorded.Equal(before) {
		t.Fatalf("refused sync changed authoritative manifest: exists=%t, error=%v", exists, err)
	}
}
