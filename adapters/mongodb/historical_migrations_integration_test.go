package mongodb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestFrozenMongoDBV1HistoryReplaysInIsolatedShadowDatabase(t *testing.T) {
	config := mongoDBMigrationVerifierConfig(t)
	observer := mongoDBMigrationVerifierObserver(t, config)
	before := mongoDBShadowDatabaseInventory(t, observer)

	if err := verifyMongoDBArtifacts(t.Context(), config, filepath.Join("testdata", "historical-v1")); err != nil {
		t.Fatalf("verify frozen MongoDB history: %v", err)
	}
	if after := mongoDBShadowDatabaseInventory(t, observer); !reflect.DeepEqual(after, before) {
		t.Fatalf("MongoDB shadow inventory after successful replay = %#v, want %#v", after, before)
	}
}

func TestMongoDBShadowReplayIsConcurrentAndLeavesNoDatabase(t *testing.T) {
	config := mongoDBMigrationVerifierConfig(t)
	observer := mongoDBMigrationVerifierObserver(t, config)
	before := mongoDBShadowDatabaseInventory(t, observer)
	directory := filepath.Join("testdata", "historical-v1")

	start := make(chan struct{})
	errorsChannel := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			errorsChannel <- verifyMongoDBArtifacts(t.Context(), config, directory)
		}()
	}
	close(start)
	workers.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("concurrent MongoDB shadow replay: %v", err)
		}
	}
	if after := mongoDBShadowDatabaseInventory(t, observer); !reflect.DeepEqual(after, before) {
		t.Fatalf("MongoDB shadow inventory after concurrent replay = %#v, want %#v", after, before)
	}
}

func TestMongoDBShadowReplayRejectsIntermediatePhysicalDriftAndCleansUp(t *testing.T) {
	config := mongoDBMigrationVerifierConfig(t)
	observer := mongoDBMigrationVerifierObserver(t, config)
	files, err := migrationartifact.ReadAll(filepath.Join("testdata", "historical-v1"))
	if err != nil {
		t.Fatal(err)
	}
	replay, err := prepareMongoDBArtifactReplay(t.Context(), files)
	if err != nil {
		t.Fatal(err)
	}
	firstCreate := replay[0].steps[0]
	if firstCreate.kind != ridumigration.StepMongoDBCreateIndex {
		t.Fatalf("first frozen MongoDB replay step = %q", firstCreate.kind)
	}
	shadowName := ""
	err = withMongoDBShadowDatabase(t.Context(), config, func(shadow *Store) error {
		shadowName = shadow.database.Name()
		_, err := shadow.database.Collection(firstCreate.index.collection).Indexes().CreateOne(
			t.Context(),
			mongo.IndexModel{
				Keys:    bson.D{{Key: "unexpected", Value: int32(1)}},
				Options: options.Index().SetName("z_shadow_unexpected"),
			},
		)
		if err != nil {
			return err
		}
		return replayMongoDBArtifacts(t.Context(), shadow, replay)
	})
	if err == nil || !strings.Contains(err.Error(), files[0].Name) || !strings.Contains(err.Error(), "unexpected index") {
		t.Fatalf("intermediate MongoDB assertion error = %v", err)
	}
	if mongoDBDatabaseExists(t, observer, shadowName) {
		t.Fatalf("failed MongoDB shadow replay retained database %q", shadowName)
	}
}

func TestMongoDBShadowLifecycleDropsOnlyItsExactDatabaseOnFailureAndCancellation(t *testing.T) {
	config := mongoDBMigrationVerifierConfig(t)
	observer := mongoDBMigrationVerifierObserver(t, config)
	sentinelName, err := newMongoDBShadowDatabaseName()
	if err != nil {
		t.Fatal(err)
	}
	sentinel := observer.client.Database(sentinelName)
	if _, err := sentinel.Collection("sentinel").InsertOne(t.Context(), bson.D{{Key: "owned", Value: true}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mongoDBDropExactDatabase(t, sentinel) })

	primaryError := errors.New("injected shadow replay failure")
	failedName := ""
	err = withMongoDBShadowDatabase(t.Context(), config, func(shadow *Store) error {
		failedName = shadow.database.Name()
		if _, err := shadow.database.Collection("materialized").InsertOne(t.Context(), bson.D{{Key: "value", Value: 1}}); err != nil {
			return err
		}
		return primaryError
	})
	if !errors.Is(err, primaryError) {
		t.Fatalf("MongoDB shadow failure = %v, want injected error", err)
	}
	if mongoDBDatabaseExists(t, observer, failedName) {
		t.Fatalf("failed MongoDB shadow action retained database %q", failedName)
	}
	if !mongoDBDatabaseExists(t, observer, sentinelName) {
		t.Fatalf("MongoDB shadow cleanup removed independent sentinel %q", sentinelName)
	}

	canceled, cancel := context.WithCancel(t.Context())
	canceledName := ""
	err = withMongoDBShadowDatabase(canceled, config, func(shadow *Store) error {
		canceledName = shadow.database.Name()
		if _, err := shadow.database.Collection("materialized").InsertOne(canceled, bson.D{{Key: "value", Value: 1}}); err != nil {
			return err
		}
		cancel()
		return canceled.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled MongoDB shadow action = %v", err)
	}
	if mongoDBDatabaseExists(t, observer, canceledName) {
		t.Fatalf("canceled MongoDB shadow action retained database %q", canceledName)
	}
	if !mongoDBDatabaseExists(t, observer, sentinelName) {
		t.Fatalf("canceled MongoDB shadow cleanup removed independent sentinel %q", sentinelName)
	}
}

func TestMongoDBShadowCleanupFailureRetainsThePrimaryError(t *testing.T) {
	config := mongoDBMigrationVerifierConfig(t)
	observer := mongoDBMigrationVerifierObserver(t, config)
	primaryError := errors.New("injected replay failure before cleanup")
	shadowName := ""
	err := withMongoDBShadowDatabase(t.Context(), config, func(shadow *Store) error {
		shadowName = shadow.database.Name()
		if _, err := shadow.database.Collection("materialized").InsertOne(t.Context(), bson.D{{Key: "value", Value: 1}}); err != nil {
			return err
		}
		if err := shadow.client.Disconnect(t.Context()); err != nil {
			return err
		}
		return primaryError
	})
	if shadowName != "" {
		t.Cleanup(func() { mongoDBDropExactDatabase(t, observer.client.Database(shadowName)) })
	}
	if !errors.Is(err, primaryError) || !strings.Contains(err.Error(), "drop isolated MongoDB migration database "+shadowName) {
		t.Fatalf("MongoDB joined replay/cleanup error = %v", err)
	}
	if !mongoDBDatabaseExists(t, observer, shadowName) {
		t.Fatalf("cleanup-failure fixture unexpectedly removed database %q", shadowName)
	}
}

func mongoDBMigrationVerifierConfig(t *testing.T) Config {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("RIDU_MONGODB_URL"))
	if databaseURL == "" {
		t.Skip("set RIDU_MONGODB_URL to run MongoDB migration verification tests")
	}
	return Config{
		DatabaseURL: databaseURL, AllowInsecureTransport: true,
		ConnectTimeout: 10 * time.Second, ServerSelectionTimeout: 10 * time.Second,
	}
}

func mongoDBMigrationVerifierObserver(t *testing.T, config Config) *Store {
	t.Helper()
	observer, err := OpenWithConfig(t.Context(), config)
	if err != nil {
		t.Fatalf("open MongoDB migration verification observer: %v", err)
	}
	t.Cleanup(func() {
		if err := observer.Close(); err != nil {
			t.Errorf("close MongoDB migration verification observer: %v", err)
		}
	})
	return observer
}

func mongoDBShadowDatabaseInventory(t *testing.T, observer *Store) map[string]struct{} {
	t.Helper()
	names, err := observer.client.ListDatabaseNames(t.Context(), bson.D{})
	if err != nil {
		t.Fatal(err)
	}
	inventory := make(map[string]struct{})
	for _, name := range names {
		if strings.HasPrefix(name, mongoDBShadowDatabasePrefix) {
			inventory[name] = struct{}{}
		}
	}
	return inventory
}

func mongoDBDatabaseExists(t *testing.T, observer *Store, name string) bool {
	t.Helper()
	if name == "" {
		t.Fatal("MongoDB database identity is empty")
	}
	names, err := observer.client.ListDatabaseNames(t.Context(), bson.D{{Key: "name", Value: name}})
	if err != nil {
		t.Fatal(err)
	}
	return len(names) != 0
}

func mongoDBDropExactDatabase(t *testing.T, database *mongo.Database) {
	t.Helper()
	if database == nil || !strings.HasPrefix(database.Name(), mongoDBShadowDatabasePrefix) {
		t.Errorf("refuse to drop unexpected MongoDB test database")
		return
	}
	cleanupContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := database.Drop(cleanupContext); err != nil {
		t.Errorf("drop exact MongoDB shadow test database %s: %v", database.Name(), err)
	}
}
