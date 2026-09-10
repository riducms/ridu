package postgres

import (
	"context"
	"github.com/riducms/ridu/schema"
	"testing"
)

func TestBlockGeneratedNameIsMetadataOnly(t *testing.T) {
	before := atlasTestManifest(phaseOneBlocks())
	snapshot := before.Snapshot()
	snapshot.Collections[0].Fields[0].Blocks.ResolvedTypes()[0].TypeName = "Hero"
	after := schema.NewManifest(snapshot)
	if _, err := buildPostgresTransformTestArtifact(context.Background(), "name-block", &before, after); err != nil {
		t.Fatal(err)
	}
}
