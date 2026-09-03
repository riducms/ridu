package mongodb

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMongoMigrationRunnerAppliesStatusesReadiesAndResumesIdempotently(t *testing.T) {
	config := mongoDBMigrationVerifierConfig(t)
	directory := filepath.Join("testdata", "historical-v1")
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	err = withMongoDBShadowDatabase(t.Context(), config, func(shadow *Store) error {
		if err := shadow.ApplyArtifacts(t.Context(), directory); err != nil {
			return err
		}
		statuses, err := shadow.ArtifactStatus(t.Context(), directory)
		if err != nil {
			return err
		}
		if len(statuses) != len(files) {
			t.Fatalf("MongoDB migration statuses = %d, want %d", len(statuses), len(files))
		}
		for _, status := range statuses {
			if !status.Applied {
				t.Fatalf("MongoDB migration status = %#v", status)
			}
			for _, phase := range status.Phases {
				if phase.State != mongoMigrationStepComplete {
					t.Fatalf("MongoDB migration phase status = %#v", phase)
				}
			}
		}
		manifest, err := files[len(files)-1].Artifact.AfterManifest()
		if err != nil {
			return err
		}
		if err := shadow.Ready(t.Context(), manifest); err != nil {
			return err
		}
		return shadow.ApplyArtifacts(t.Context(), directory)
	})
	if err != nil {
		t.Fatalf("MongoDB production migration lifecycle: %v", err)
	}
}

func TestMongoMigrationReadinessAllowsHarmlessUnmanagedNonUniqueIndex(t *testing.T) {
	config := mongoDBMigrationVerifierConfig(t)
	directory := filepath.Join("testdata", "historical-v1")
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	direction, err := bson.ParseDecimal128("1.00")
	if err != nil {
		t.Fatal(err)
	}
	historyDigest := mongoMigrationHistoryDigest(t, files)
	err = withMongoDBShadowDatabase(t.Context(), config, func(shadow *Store) error {
		if err := shadow.ApplyArtifacts(t.Context(), directory); err != nil {
			return err
		}
		manifest, err := files[len(files)-1].Artifact.AfterManifest()
		if err != nil {
			return err
		}
		collection := manifest.Snapshot().Collections[0]
		physical := shadow.database.Collection(physicalCollectionName(collection.ID))
		if _, err := physical.Indexes().CreateOne(t.Context(), mongo.IndexModel{
			Keys:    bson.D{{Key: "meta.updatedAt", Value: direction}},
			Options: options.Index().SetName("application_updated_at_lookup"),
		}); err != nil {
			return err
		}
		if err := shadow.VerifyIndexes(t.Context(), manifest); err != nil {
			return fmt.Errorf("verify harmless unmanaged index: %w", err)
		}
		if err := shadow.SyncIndexes(t.Context(), manifest); err != nil {
			return fmt.Errorf("sync harmless unmanaged index: %w", err)
		}
		if err := shadow.ReadyWithMigrationHistory(t.Context(), manifest, historyDigest); err != nil {
			return fmt.Errorf("ready with harmless unmanaged index: %w", err)
		}
		if _, err := shadow.ArtifactStatus(t.Context(), directory); err != nil {
			return fmt.Errorf("status with harmless unmanaged index: %w", err)
		}
		if _, err := mongoIndexedCreate(t.Context(), shadow, collection, "after-status", store.Values{
			"title": store.String("still writable after status"),
		}); err != nil {
			return fmt.Errorf("write after harmless-index status: %w", err)
		}
		specifications, err := physical.Indexes().ListSpecifications(t.Context())
		if err != nil {
			return err
		}
		for _, specification := range specifications {
			if specification.Name == "application_updated_at_lookup" {
				return nil
			}
		}
		return fmt.Errorf("additive sync removed harmless unmanaged index")
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMongoMigrationReadinessRejectsUnmanagedIndexHazards(t *testing.T) {
	config := mongoDBMigrationVerifierConfig(t)
	directory := filepath.Join("testdata", "historical-v1")
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := files[len(files)-1].Artifact.AfterManifest()
	if err != nil {
		t.Fatal(err)
	}
	historyDigest := mongoMigrationHistoryDigest(t, files)
	collection := manifest.Snapshot().Collections[0]
	for _, test := range []struct {
		name      string
		indexName string
		install   func(context.Context, *Store, string, string) error
	}{
		{
			name: "unique index", indexName: "application_unique_hazard",
			install: func(ctx context.Context, shadow *Store, physicalName, indexName string) error {
				_, err := shadow.database.Collection(physicalName).Indexes().CreateOne(ctx, mongo.IndexModel{
					Keys: bson.D{{Key: "values.applicationUnique", Value: int32(1)}},
					Options: options.Index().
						SetName(indexName).
						SetUnique(true),
				})
				return err
			},
		},
		{
			name: "TTL index", indexName: "application_ttl_hazard",
			install: func(ctx context.Context, shadow *Store, physicalName, indexName string) error {
				_, err := shadow.database.Collection(physicalName).Indexes().CreateOne(ctx, mongo.IndexModel{
					Keys: bson.D{{Key: "meta.applicationExpiresAt", Value: int32(1)}},
					Options: options.Index().
						SetName(indexName).
						SetExpireAfterSeconds(0),
				})
				return err
			},
		},
		{
			name: "prepareUnique index", indexName: "application_prepare_unique_hazard",
			install: func(ctx context.Context, shadow *Store, physicalName, indexName string) error {
				if _, err := shadow.database.Collection(physicalName).Indexes().CreateOne(ctx, mongo.IndexModel{
					Keys:    bson.D{{Key: "values.applicationPrepared", Value: int32(1)}},
					Options: options.Index().SetName(indexName),
				}); err != nil {
					return err
				}
				return shadow.database.RunCommand(ctx, bson.D{
					{Key: "collMod", Value: physicalName},
					{Key: "index", Value: bson.D{
						{Key: "name", Value: indexName},
						{Key: "prepareUnique", Value: true},
					}},
				}).Err()
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := withMongoDBShadowDatabase(t.Context(), config, func(shadow *Store) error {
				if err := shadow.ApplyArtifacts(t.Context(), directory); err != nil {
					return err
				}
				physicalName := physicalCollectionName(collection.ID)
				if err := test.install(t.Context(), shadow, physicalName, test.indexName); err != nil {
					return err
				}
				for _, verification := range []struct {
					name string
					run  func() error
				}{
					{name: "ReadyWithMigrationHistory", run: func() error {
						return shadow.ReadyWithMigrationHistory(t.Context(), manifest, historyDigest)
					}},
					{name: "ArtifactStatus", run: func() error {
						_, err := shadow.ArtifactStatus(t.Context(), directory)
						return err
					}},
				} {
					err := verification.run()
					if err == nil || !strings.Contains(err.Error(), test.indexName) ||
						!strings.Contains(err.Error(), "valid writes or data lifetime") {
						t.Fatalf("%s unmanaged-index hazard error = %v", verification.name, err)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMongoMigrationLedgerPhysicalReadiness(t *testing.T) {
	config := mongoDBMigrationVerifierConfig(t)
	directory := filepath.Join("testdata", "historical-v1")
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := files[len(files)-1].Artifact.AfterManifest()
	if err != nil {
		t.Fatal(err)
	}
	historyDigest := mongoMigrationHistoryDigest(t, files)
	ledgerNames := bson.A{
		mongoMigrationArtifactCollectionName,
		mongoMigrationStepCollectionName,
		mongoMigrationLeaseCollectionName,
	}

	t.Run("absent namespaces remain absent", func(t *testing.T) {
		err := withMongoDBShadowDatabase(t.Context(), config, func(shadow *Store) error {
			filter := bson.D{{Key: "name", Value: bson.D{{Key: "$in", Value: ledgerNames}}}}
			before, err := shadow.database.ListCollectionNames(t.Context(), filter)
			if err != nil {
				return err
			}
			if len(before) != 0 {
				t.Fatalf("fresh MongoDB ledger namespaces = %v", before)
			}
			statuses, err := shadow.ArtifactStatus(t.Context(), directory)
			if err != nil {
				return fmt.Errorf("status with absent MongoDB ledger namespaces: %w", err)
			}
			if len(statuses) != len(files) {
				t.Fatalf("pending MongoDB migration statuses = %d, want %d", len(statuses), len(files))
			}
			for _, status := range statuses {
				if status.Applied {
					t.Fatalf("absent MongoDB ledger reported applied migration %#v", status)
				}
			}
			after, err := shadow.database.ListCollectionNames(t.Context(), filter)
			if err != nil {
				return err
			}
			if len(after) != 0 {
				t.Fatalf("status created MongoDB ledger namespaces = %v", after)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("harmless state remains unchanged", func(t *testing.T) {
		err := withMongoDBShadowDatabase(t.Context(), config, func(shadow *Store) error {
			if err := shadow.ApplyArtifacts(t.Context(), directory); err != nil {
				return err
			}
			if _, err := shadow.database.Collection(mongoMigrationArtifactCollectionName).Indexes().CreateOne(
				t.Context(),
				mongo.IndexModel{
					Keys:    bson.D{{Key: "codec", Value: int64(1)}},
					Options: options.Index().SetName("application_ledger_codec_lookup"),
				},
			); err != nil {
				return err
			}
			if err := shadow.database.RunCommand(t.Context(), bson.D{
				{Key: "collMod", Value: mongoMigrationStepCollectionName},
				{Key: "validator", Value: bson.D{{Key: "futureRequired", Value: bson.D{{Key: "$exists", Value: true}}}}},
				{Key: "validationLevel", Value: "off"},
				{Key: "validationAction", Value: "error"},
			}).Err(); err != nil {
				return err
			}
			if err := shadow.database.RunCommand(t.Context(), bson.D{
				{Key: "collMod", Value: mongoMigrationLeaseCollectionName},
				{Key: "changeStreamPreAndPostImages", Value: bson.D{{Key: "enabled", Value: false}}},
			}).Err(); err != nil {
				return err
			}
			before, err := mongoMigrationLedgerPhysicalSnapshot(t.Context(), shadow)
			if err != nil {
				return err
			}
			if err := shadow.Ready(t.Context(), manifest); err != nil {
				return fmt.Errorf("manifest-only readiness with harmless MongoDB ledger state: %w", err)
			}
			if err := shadow.ReadyWithMigrationHistory(t.Context(), manifest, historyDigest); err != nil {
				return fmt.Errorf("history readiness with harmless MongoDB ledger state: %w", err)
			}
			if _, err := shadow.ArtifactStatus(t.Context(), directory); err != nil {
				return fmt.Errorf("status with harmless MongoDB ledger state: %w", err)
			}
			collection := manifest.Snapshot().Collections[0]
			if _, err := mongoIndexedCreate(t.Context(), shadow, collection, "after-ledger-status", store.Values{
				"title": store.String("still authorized after ledger status"),
			}); err != nil {
				return fmt.Errorf("write after harmless MongoDB ledger status: %w", err)
			}
			after, err := mongoMigrationLedgerPhysicalSnapshot(t.Context(), shadow)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("MongoDB readiness or status mutated harmless ledger metadata\nbefore: %#v\nafter:  %#v", before, after)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	tests := []struct {
		name           string
		namespace      string
		wantDetail     string
		index          *mongo.IndexModel
		modify         bson.D
		leaseWriteCode string
	}{
		{
			name: "enforcing validator blocks the migration lease", namespace: mongoMigrationLeaseCollectionName,
			wantDetail: "ordinary writable collection",
			modify: bson.D{
				{Key: "collMod", Value: mongoMigrationLeaseCollectionName},
				{Key: "validator", Value: bson.D{{Key: "futureRequired", Value: bson.D{{Key: "$exists", Value: true}}}}},
				{Key: "validationLevel", Value: "strict"},
				{Key: "validationAction", Value: "error"},
			},
			leaseWriteCode: "server code 121",
		},
		{
			name: "unique index can block the next artifact", namespace: mongoMigrationArtifactCollectionName,
			wantDetail: "application_future_artifact_unique",
			index: &mongo.IndexModel{
				Keys: bson.D{{Key: "codec", Value: int32(1)}},
				Options: options.Index().SetName("application_future_artifact_unique").
					SetUnique(true).
					SetPartialFilterExpression(bson.D{{Key: "position", Value: bson.D{{Key: "$gte", Value: int64(len(files))}}}}),
			},
		},
		{
			name: "TTL index can change step lifetime", namespace: mongoMigrationStepCollectionName,
			wantDetail: "application_step_ttl",
			index: &mongo.IndexModel{
				Keys:    bson.D{{Key: "completedAt", Value: int32(1)}},
				Options: options.Index().SetName("application_step_ttl").SetExpireAfterSeconds(0),
			},
		},
		{
			name: "prepareUnique index can block step completion", namespace: mongoMigrationStepCollectionName,
			wantDetail: "application_step_prepare_unique",
			index: &mongo.IndexModel{
				Keys:    bson.D{{Key: "state", Value: int32(1)}},
				Options: options.Index().SetName("application_step_prepare_unique"),
			},
			modify: bson.D{
				{Key: "collMod", Value: mongoMigrationStepCollectionName},
				{Key: "index", Value: bson.D{{Key: "name", Value: "application_step_prepare_unique"}, {Key: "prepareUnique", Value: true}}},
			},
		},
		{
			name: "special index can reject a lease owner", namespace: mongoMigrationLeaseCollectionName,
			wantDetail: "application_lease_owner_geo",
			index: &mongo.IndexModel{
				Keys:    bson.D{{Key: "owner", Value: "2dsphere"}},
				Options: options.Index().SetName("application_lease_owner_geo"),
			},
			leaseWriteCode: "server code 16755",
		},
		{
			name: "compound index uses the conservative write classifier", namespace: mongoMigrationLeaseCollectionName,
			wantDetail: "application_lease_compound",
			index: &mongo.IndexModel{
				Keys: bson.D{
					{Key: "owner", Value: int32(1)},
					{Key: "fence", Value: int32(1)},
				},
				Options: options.Index().SetName("application_lease_compound"),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := withMongoDBShadowDatabase(t.Context(), config, func(shadow *Store) error {
				if err := shadow.ApplyArtifacts(t.Context(), directory); err != nil {
					return err
				}
				if err := shadow.ReadyWithMigrationHistory(t.Context(), manifest, historyDigest); err != nil {
					return err
				}
				if test.index != nil {
					if _, err := shadow.database.Collection(test.namespace).Indexes().CreateOne(t.Context(), *test.index); err != nil {
						return err
					}
				}
				if len(test.modify) != 0 {
					if err := shadow.database.RunCommand(t.Context(), test.modify).Err(); err != nil {
						return err
					}
				}
				if test.leaseWriteCode != "" {
					lease, err := shadow.acquireMongoMigrationLease(t.Context(), 100*time.Millisecond, 200*time.Millisecond)
					if lease != nil {
						lease.release()
					}
					if err == nil || !strings.Contains(err.Error(), test.leaseWriteCode) {
						return fmt.Errorf("valid MongoDB migration lease write through %s = %v", test.name, err)
					}
				}
				before, err := mongoMigrationLedgerPhysicalSnapshot(t.Context(), shadow)
				if err != nil {
					return err
				}
				if _, err := shadow.ArtifactStatus(t.Context(), directory); err == nil ||
					!strings.Contains(err.Error(), test.namespace) || !strings.Contains(err.Error(), test.wantDetail) {
					t.Fatalf("MongoDB status physical hazard error = %v", err)
				}
				if err := shadow.requireVerifiedIndexes(manifest.Snapshot().Collections[0]); err != nil {
					t.Fatalf("failed status inspection cleared serving authorization: %v", err)
				}
				if err := shadow.Ready(t.Context(), manifest); err == nil ||
					!strings.Contains(err.Error(), test.namespace) || !strings.Contains(err.Error(), test.wantDetail) {
					t.Fatalf("MongoDB manifest-only readiness physical hazard error = %v", err)
				}
				if err := shadow.requireVerifiedIndexes(manifest.Snapshot().Collections[0]); err == nil ||
					!strings.Contains(err.Error(), "not verified") {
					t.Fatalf("failed readiness retained serving authorization: %v", err)
				}
				if err := shadow.ReadyWithMigrationHistory(t.Context(), manifest, historyDigest); err == nil ||
					!strings.Contains(err.Error(), test.namespace) || !strings.Contains(err.Error(), test.wantDetail) {
					t.Fatalf("MongoDB history readiness physical hazard error = %v", err)
				}
				after, err := mongoMigrationLedgerPhysicalSnapshot(t.Context(), shadow)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(after, before) {
					t.Fatalf("MongoDB readiness or status mutated hazardous ledger metadata\nbefore: %#v\nafter:  %#v", before, after)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

type mongoMigrationLedgerPhysicalState struct {
	collections []mongo.CollectionSpecification
	indexes     map[string][]mongo.IndexSpecification
}

func mongoMigrationLedgerPhysicalSnapshot(ctx context.Context, backend *Store) (mongoMigrationLedgerPhysicalState, error) {
	names := bson.A{
		mongoMigrationArtifactCollectionName,
		mongoMigrationStepCollectionName,
		mongoMigrationLeaseCollectionName,
	}
	collections, err := backend.database.ListCollectionSpecifications(
		ctx,
		bson.D{{Key: "name", Value: bson.D{{Key: "$in", Value: names}}}},
	)
	if err != nil {
		return mongoMigrationLedgerPhysicalState{}, err
	}
	state := mongoMigrationLedgerPhysicalState{
		collections: collections,
		indexes:     make(map[string][]mongo.IndexSpecification, len(names)),
	}
	for _, value := range names {
		name := value.(string)
		indexes, err := backend.database.Collection(name).Indexes().ListSpecifications(ctx)
		if err != nil {
			return mongoMigrationLedgerPhysicalState{}, err
		}
		state.indexes[name] = indexes
	}
	return state, nil
}

func TestMongoMigrationReadinessAllowsQueryableEncryptionAuxiliaryNamespaces(t *testing.T) {
	config := mongoDBMigrationVerifierConfig(t)
	directory := filepath.Join("testdata", "historical-v1")
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := files[len(files)-1].Artifact.AfterManifest()
	if err != nil {
		t.Fatal(err)
	}
	historyDigest := mongoMigrationHistoryDigest(t, files)
	collection := manifest.Snapshot().Collections[0]
	err = withMongoDBShadowDatabase(t.Context(), config, func(shadow *Store) error {
		if err := shadow.ApplyArtifacts(t.Context(), directory); err != nil {
			return err
		}
		physicalName := physicalCollectionName(collection.ID)
		for _, suffix := range []string{".esc", ".ecoc"} {
			if err := shadow.database.CreateCollection(t.Context(), "enxcol_."+physicalName+suffix); err != nil {
				return err
			}
		}
		if err := shadow.ReadyWithMigrationHistory(t.Context(), manifest, historyDigest); err != nil {
			return fmt.Errorf("ready with queryable-encryption auxiliary namespaces: %w", err)
		}
		if _, err := shadow.ArtifactStatus(t.Context(), directory); err != nil {
			return fmt.Errorf("status with queryable-encryption auxiliary namespaces: %w", err)
		}
		if _, err := mongoIndexedCreate(t.Context(), shadow, collection, "after-qe-status", store.Values{
			"title": store.String("still writable with auxiliary namespaces"),
		}); err != nil {
			return fmt.Errorf("write after queryable-encryption auxiliary namespace status: %w", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMongoMigrationReadinessTreatsRetiredNamespacesAsStatusDiagnostics(t *testing.T) {
	config := mongoDBMigrationVerifierConfig(t)
	directory := filepath.Join("testdata", "historical-v1")
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := files[len(files)-1].Artifact.AfterManifest()
	if err != nil {
		t.Fatal(err)
	}
	collection := manifest.Snapshot().Collections[0]
	historyDigest := mongoMigrationHistoryDigest(t, files)
	for _, test := range []struct {
		name           string
		namespace      string
		wantDiagnostic string
	}{
		{
			name:           "retired version namespace",
			namespace:      physicalVersionCollectionName(collection.ID),
			wantDiagnostic: "version state exists while versions are disabled",
		},
		{
			name:           "retired reference namespace",
			namespace:      mongoReferenceCollectionName,
			wantDiagnostic: "namespace exists while no relationship or upload field requires it",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := withMongoDBShadowDatabase(t.Context(), config, func(shadow *Store) error {
				if err := shadow.ApplyArtifacts(t.Context(), directory); err != nil {
					return err
				}
				if _, err := shadow.database.Collection(test.namespace).InsertOne(
					t.Context(), bson.D{{Key: "_id", Value: "retired-state"}},
				); err != nil {
					return err
				}
				if err := shadow.ReadyWithMigrationHistory(t.Context(), manifest, historyDigest); err != nil {
					return fmt.Errorf("serving readiness rejected retired namespace: %w", err)
				}
				if _, err := shadow.ArtifactStatus(t.Context(), directory); err == nil ||
					!strings.Contains(err.Error(), test.wantDiagnostic) {
					t.Fatalf("retired namespace status diagnostic = %v", err)
				}
				if _, err := mongoIndexedCreate(t.Context(), shadow, collection, "after-status", store.Values{
					"title": store.String("still writable after diagnostic"),
				}); err != nil {
					return fmt.Errorf("write after retired-namespace status diagnostic: %w", err)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMongoMigrationLeaseWaitExpiryAndStaleOwnerFencing(t *testing.T) {
	config := mongoDBMigrationVerifierConfig(t)
	err := withMongoDBShadowDatabase(t.Context(), config, func(shadow *Store) error {
		first, err := shadow.acquireMongoMigrationLease(t.Context(), 100*time.Millisecond, 200*time.Millisecond)
		if err != nil {
			return err
		}
		defer first.release()
		started := time.Now()
		if _, err := shadow.acquireMongoMigrationLease(t.Context(), 75*time.Millisecond, 200*time.Millisecond); err == nil {
			t.Fatal("concurrent MongoDB migrator acquired an active lease")
		} else if time.Since(started) > time.Second {
			t.Fatalf("bounded MongoDB lease wait took %s", time.Since(started))
		}
		time.Sleep(250 * time.Millisecond)
		second, err := shadow.acquireMongoMigrationLease(t.Context(), time.Second, 500*time.Millisecond)
		if err != nil {
			return err
		}
		defer second.release()
		if err := first.transaction(t.Context(), func(_ context.Context) error { return nil }); !errors.Is(err, errMongoMigrationLeaseLost) {
			t.Fatalf("stale MongoDB migration owner checkpoint = %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("MongoDB migration lease lifecycle: %v", err)
	}
}

func TestMongoMigrationHeartbeatPreventsTakeoverUntilStopped(t *testing.T) {
	config := mongoDBMigrationVerifierConfig(t)
	err := withMongoDBShadowDatabase(t.Context(), config, func(shadow *Store) error {
		competitor := &Store{
			client: shadow.client, database: shadow.database, now: time.Now,
			verifiedIndexes: make(map[schema.StableID]mongoVerifiedIndexPlan), verifiedVersionIndexes: make(map[schema.StableID]bool),
		}
		lease, err := shadow.acquireMongoMigrationLease(t.Context(), time.Second, 150*time.Millisecond)
		if err != nil {
			return err
		}
		defer lease.release()
		heartbeatContext, stopHeartbeat := context.WithCancel(t.Context())
		heartbeatResult := make(chan error, 1)
		go lease.heartbeat(heartbeatContext, stopHeartbeat, heartbeatResult)
		time.Sleep(350 * time.Millisecond)
		if acquired, err := competitor.acquireMongoMigrationLease(t.Context(), 75*time.Millisecond, 150*time.Millisecond); err == nil {
			acquired.release()
			t.Fatal("second MongoDB Store took over a heartbeat-renewed lease")
		}
		stopHeartbeat()
		if err := <-heartbeatResult; err != nil {
			t.Fatalf("stop MongoDB migration heartbeat: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
		takeover, err := competitor.acquireMongoMigrationLease(t.Context(), time.Second, 150*time.Millisecond)
		if err != nil {
			return err
		}
		takeover.release()
		return nil
	})
	if err != nil {
		t.Fatalf("MongoDB migration heartbeat lifecycle: %v", err)
	}
}

func TestMongoMigrationRunnerResumesPhysicalIndexAfterCrash(t *testing.T) {
	config := mongoDBMigrationVerifierConfig(t)
	directory := filepath.Join("testdata", "historical-v1")
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := prepareMongoDBArtifactReplay(t.Context(), files)
	if err != nil {
		t.Fatal(err)
	}
	first := replay[0].steps[0]
	if first.kind != ridumigration.StepMongoDBCreateIndex {
		t.Fatalf("first frozen step = %q", first.kind)
	}
	err = withMongoDBShadowDatabase(t.Context(), config, func(shadow *Store) error {
		if err := shadow.createMongoIndex(t.Context(), first.index.collection, first.index.description, first.index.definition); err != nil {
			return err
		}
		row := mongoMigrationStepLedgerRow{
			ArtifactName: files[0].Name, ArtifactDigest: files[0].Digest,
			PhaseID: first.phaseID, StepID: first.stepID, PhaseMode: first.mode, StepKind: first.kind,
			State: mongoMigrationStepRunning, Attempts: 1,
			Owner: strings.Repeat("1", 32), Fence: strings.Repeat("2", 32), UpdatedAt: time.Now().UTC(),
		}
		document, err := encodeMongoMigrationStepLedger(row)
		if err != nil {
			return err
		}
		if _, err := shadow.mongoMigrationCollection(mongoMigrationStepCollectionName).InsertOne(t.Context(), document); err != nil {
			return err
		}
		if err := shadow.ApplyArtifacts(t.Context(), directory); err != nil {
			return err
		}
		statuses, err := shadow.ArtifactStatus(t.Context(), directory)
		if err != nil {
			return err
		}
		for _, status := range statuses {
			if !status.Applied {
				t.Fatalf("resumed MongoDB migration status = %#v", status)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("resume MongoDB physical index: %v", err)
	}
}

func TestMongoMigrationStatusRejectsCompletedStepWithMissingPhysicalIndex(t *testing.T) {
	config := mongoDBMigrationVerifierConfig(t)
	directory := filepath.Join("testdata", "historical-v1")
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := prepareMongoDBArtifactReplay(t.Context(), files)
	if err != nil {
		t.Fatal(err)
	}
	first := replay[0].steps[0]
	err = withMongoDBShadowDatabase(t.Context(), config, func(shadow *Store) error {
		now := time.Now().UTC()
		row := mongoMigrationStepLedgerRow{
			ArtifactName: files[0].Name, ArtifactDigest: files[0].Digest,
			PhaseID: first.phaseID, StepID: first.stepID, PhaseMode: first.mode, StepKind: first.kind,
			State: mongoMigrationStepComplete, Attempts: 1,
			Owner: strings.Repeat("1", 32), Fence: strings.Repeat("2", 32), UpdatedAt: now, CompletedAt: &now,
		}
		document, err := encodeMongoMigrationStepLedger(row)
		if err != nil {
			return err
		}
		if _, err := shadow.mongoMigrationCollection(mongoMigrationStepCollectionName).InsertOne(t.Context(), document); err != nil {
			return err
		}
		_, err = shadow.ArtifactStatus(t.Context(), directory)
		if err == nil || !strings.Contains(err.Error(), "required index") || !strings.Contains(err.Error(), "missing") {
			t.Fatalf("completed-step physical drift error = %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify completed MongoDB step physical state: %v", err)
	}
}

func TestMongoMigrationLedgerRejectsChecksumAndLineageTampering(t *testing.T) {
	config := mongoDBMigrationVerifierConfig(t)
	directory := filepath.Join("testdata", "historical-v1")
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	err = withMongoDBShadowDatabase(t.Context(), config, func(shadow *Store) error {
		if err := shadow.ApplyArtifacts(t.Context(), directory); err != nil {
			return err
		}
		artifacts := shadow.mongoMigrationCollection(mongoMigrationArtifactCollectionName)
		if _, err := artifacts.UpdateOne(t.Context(), bson.D{{Key: "_id", Value: files[0].Name}}, bson.D{{Key: "$set", Value: bson.D{{Key: "artifactDigest", Value: strings.Repeat("c", 64)}}}}); err != nil {
			return err
		}
		if _, err := shadow.ArtifactStatus(t.Context(), directory); err == nil ||
			(!strings.Contains(err.Error(), "changed after application") && !strings.Contains(err.Error(), "discontinuous")) {
			t.Fatalf("MongoDB checksum-tamper error = %v", err)
		}
		if _, err := artifacts.UpdateOne(t.Context(), bson.D{{Key: "_id", Value: files[0].Name}}, bson.D{{Key: "$set", Value: bson.D{{Key: "artifactDigest", Value: files[0].Digest}}}}); err != nil {
			return err
		}
		if _, err := artifacts.UpdateOne(t.Context(), bson.D{{Key: "_id", Value: files[1].Name}}, bson.D{{Key: "$set", Value: bson.D{{Key: "previousArtifactDigest", Value: strings.Repeat("d", 64)}}}}); err != nil {
			return err
		}
		if _, err := shadow.ArtifactStatus(t.Context(), directory); err == nil || !strings.Contains(err.Error(), "predecessor") {
			t.Fatalf("MongoDB lineage-tamper error = %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("MongoDB migration ledger tamper checks: %v", err)
	}
}

func mongoMigrationHistoryDigest(t *testing.T, files []migrationartifact.File) string {
	t.Helper()
	identities := make([]ridumigration.ArtifactIdentity, len(files))
	for index, file := range files {
		identities[index] = ridumigration.ArtifactIdentity{Name: file.Name, Digest: file.Digest}
	}
	digest, err := ridumigration.DigestArtifactHistory(identities)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}
