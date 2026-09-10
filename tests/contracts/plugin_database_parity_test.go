package contracts_test

import (
	"context"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/field"
)

type portableCollectionPlugin struct{}

func (portableCollectionPlugin) Key() string { return "portable-records" }

func (portableCollectionPlugin) TransformConfig(config ridu.Config) (ridu.Config, error) {
	config.Collections[0].Fields = append(config.Collections[0].Fields, field.Text("pluginNote"))
	config.Collections = append(config.Collections, ridu.Collection{
		Slug: "plugin-records", Fields: field.Fields{field.Text("title")},
	})
	return config, nil
}

func TestPluginCollectionContributionsPlanAcrossPostgresAndSQLite(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Portable plugin storage",
		Collections: []ridu.Collection{{
			Slug: "posts", Fields: field.Fields{field.Text("title")},
		}},
		Plugins: []ridu.Plugin{portableCollectionPlugin{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	if len(snapshot.Collections) != 2 || snapshot.Collections[1].Slug != "plugin-records" || len(snapshot.Collections[0].Fields) != 2 {
		t.Fatalf("resolved plugin collections = %#v", snapshot.Collections)
	}
	if _, err := postgres.BuildArtifact(context.Background(), "portable-plugin", nil, manifest, nil, false); err != nil {
		t.Fatalf("PostgreSQL rejected collection-based plugin storage: %v", err)
	}
	if _, err := sqlite.CreateArtifact(context.Background(), t.TempDir(), "portable-plugin", manifest, time.Unix(1, 0), false); err != nil {
		t.Fatalf("SQLite rejected collection-based plugin storage: %v", err)
	}
}
