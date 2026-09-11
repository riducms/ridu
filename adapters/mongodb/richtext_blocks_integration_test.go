package mongodb

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/store"
	richtextblocks "github.com/riducms/ridu/tests/contracts/richtext_blocks"
)

func TestMongoDBRichTextBlocksAcceptance(t *testing.T) {
	richtextblocks.Run(t, mongoRichTextBlocksFactory)
}

func TestMongoDBRichTextBlockReferencesAcceptance(t *testing.T) {
	richtextblocks.RunReferences(t, mongoRichTextBlocksFactory)
}

func TestMongoDBRichTextBlockNamesAcceptance(t *testing.T) {
	richtextblocks.RunNames(t, mongoRichTextBlocksFactory)
}

func TestMongoDBRichTextBlocksPerformance(t *testing.T) {
	richtextblocks.RunPerformance(t, "mongodb", mongoRichTextBlocksFactory)
}

// This supplements the local development replica-set run with the existing
// pinned three-member, SCRAM-authenticated, verified-TLS production topology.
// Running it on a developer Mac is not a Linux x86-64 release qualification.
func TestMongoDBRichTextBlocksProductionReplicaSet(t *testing.T) {
	if os.Getenv(mongoDBProductionFixtureEnvironment) != "true" {
		t.Skip("set RIDU_MONGODB_PRODUCTION_FIXTURE_TEST=true to exercise the authenticated TLS replica set")
	}
	richtextblocks.Run(t, func(t *testing.T, config ridu.Config) (store.Store, *ridu.App) {
		fixture := newMongoDBProductionFixture(t)
		fixture.start(t)
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
		defer cancel()
		fixture.bootstrap(ctx, t)
		backend, err := OpenWithConfig(ctx, productionMongoDBStoreConfig(fixture.applicationURL(fixture.caPath, fixture.applicationPassword, true), "ridu-richtext-blocks"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := backend.Close(); err != nil {
				t.Error(err)
			}
		})
		app, err := ridu.New(config, backend)
		if err != nil {
			t.Fatal(err)
		}
		if err := backend.SyncIndexes(ctx, app.Manifest()); err != nil {
			t.Fatal(err)
		}
		return backend, app
	})
}

func mongoRichTextBlocksFactory(t *testing.T, config ridu.Config) (store.Store, *ridu.App) {
	t.Helper()
	backend := mongoIntegrationStore(t)
	app, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), app.Manifest()); err != nil {
		t.Fatal(err)
	}
	return backend, app
}
