package mongodb

import (
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

// `ridu dev` synchronizes MongoDB indexes without recording migrations.
// Baseline records the committed history those indexes already match, and a
// later baseline catches up after ridu dev runs ahead again.
func TestMongoDBBaselineAdoptsADevelopmentSynchronizedDatabase(t *testing.T) {
	ctx := t.Context()
	resolve := func(fields ...field.Node) schema.Manifest {
		t.Helper()
		manifest, err := ridu.Resolve(ridu.Config{Name: "MongoDB baseline", Collections: []ridu.Collection{{Slug: "posts", Fields: fields}}})
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	initial := resolve(field.Text("title"))
	withSlug := resolve(field.Text("title"), field.Text("slug").Unique())
	backend := mongoIntegrationStore(t)
	directory := t.TempDir()

	if _, err := CreateArtifact(ctx, directory, "initial", initial, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncDevelopmentSchema(ctx, initial); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "ridu migrate baseline") {
		t.Fatalf("migrate up over a development database = %v", err)
	}
	adopted, err := backend.AdoptArtifacts(ctx, directory)
	if err != nil || len(adopted) != 1 {
		t.Fatalf("baseline = %v, %v", adopted, err)
	}
	if err := backend.Ready(ctx, initial); err != nil {
		t.Fatalf("readiness after baseline: %v", err)
	}

	// ridu dev keeps synchronizing, runs ahead, and the next migration follows.
	if err := backend.SyncDevelopmentSchema(ctx, withSlug); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateArtifact(ctx, directory, "slug", withSlug, time.Unix(2, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	adopted, err = backend.AdoptArtifacts(ctx, directory)
	if err != nil || len(adopted) != 1 {
		t.Fatalf("second baseline = %v, %v", adopted, err)
	}
	statuses, err := backend.ArtifactStatus(ctx, directory)
	if err != nil || len(statuses) != 2 || !statuses[0].Applied || !statuses[1].Applied {
		t.Fatalf("status after baseline = %#v, %v", statuses, err)
	}
	if err := backend.Ready(ctx, withSlug); err != nil {
		t.Fatalf("readiness after the second baseline: %v", err)
	}
	if again, err := backend.AdoptArtifacts(ctx, directory); err != nil || len(again) != 0 {
		t.Fatalf("baseline of a current history = %v, %v", again, err)
	}
}
