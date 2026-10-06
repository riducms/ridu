package postgres

import (
	"context"
	"github.com/riducms/ridu/schema"
	"testing"
)

func TestBlockGeneratedNameIsMetadataOnly(t *testing.T) {
	before := phaseOneManifest()
	snapshot := before.Snapshot()
	snapshot.Blocks[0].TypeName = "Banner"
	after := schema.NewManifest(snapshot)
	if _, err := buildPostgresTransformTestArtifact(context.Background(), "name-block", &before, after); err != nil {
		t.Fatal(err)
	}
}
