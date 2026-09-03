package mongodb

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

const (
	mongoDBProductionFixtureEnvironment = "RIDU_MONGODB_PRODUCTION_FIXTURE_TEST"
	mongoDBProductionFixtureVersion     = "8.2.9"
	mongoDBProductionFixtureImage       = "mongo:8.2.9-noble@sha256:007773db61cb1aa44e526fb7175fc582902e67d4e6cc5f13106445767d46c818"
	mongoDBProductionReplicaSet         = "ridu-production-rs0"
)

// TestMongoDBProductionReplicaSetTLSAuthenticationAndElection is deliberately
// one representative production topology, not a failover matrix. Its Docker
// resources, credentials, CA, certificates, and data volumes are unique to the
// invocation and are removed even when the proof fails.
func TestMongoDBProductionReplicaSetTLSAuthenticationAndElection(t *testing.T) {
	if os.Getenv(mongoDBProductionFixtureEnvironment) != "true" {
		t.Skip("set RIDU_MONGODB_PRODUCTION_FIXTURE_TEST=true to run the authenticated TLS replica-set proof")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("docker is required for the MongoDB production fixture: %v", err)
	}
	fixture := newMongoDBProductionFixture(t)
	fixture.start(t)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	fixture.bootstrap(ctx, t)

	applicationURL := fixture.applicationURL(fixture.caPath, fixture.applicationPassword, true)
	clientOptions, databaseName, err := normalizedClientOptions(Config{DatabaseURL: applicationURL})
	if err != nil {
		t.Fatalf("normalize verified-TLS fixture URL: %v", err)
	}
	if databaseName != fixture.database || clientOptions.TLSConfig == nil || clientOptions.TLSConfig.InsecureSkipVerify {
		t.Fatalf("production fixture did not retain CA and hostname verification")
	}

	backend, err := OpenWithConfig(ctx, productionMongoDBStoreConfig(applicationURL, "ridu-production-fixture"))
	if err != nil {
		t.Fatalf("open authenticated TLS replica set: %v", err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Errorf("close MongoDB production fixture Store: %v", err)
		}
	})

	collection := mongoScalarCollection(false)
	manifest := mongoIndexTestManifest(collection)
	if err := backend.SyncIndexes(ctx, manifest); err != nil {
		t.Fatalf("prepare production fixture indexes: %v", err)
	}
	before, err := createMongoDBProductionFixtureDocument(ctx, backend, collection, "before-election", "before election")
	if err != nil {
		t.Fatalf("write through authenticated TLS Store before election: %v", err)
	}

	fixture.proveRedactedConnectionFailures(t)

	adminClient := fixture.authenticatedAdminClient(t, fixture.seedURL(fixture.caPath, true))
	t.Cleanup(func() { disconnectMongoDBProductionClient(t, adminClient) })
	oldPrimary := waitForMongoDBProductionPrimary(t, adminClient, "", 30*time.Second)
	assertMongoDBProductionServerVersion(t, adminClient)
	waitForMongoDBProductionReplicaSetHealth(t, adminClient, fixture.hosts(), oldPrimary, 45*time.Second)
	stepDownMongoDBProductionPrimary(t, fixture, oldPrimary)
	newPrimary := waitForMongoDBProductionPrimary(t, adminClient, oldPrimary, 60*time.Second)
	if newPrimary == oldPrimary {
		t.Fatalf("MongoDB production fixture retained primary %q after stepdown", oldPrimary)
	}

	waitForMongoDBProductionStore(t, backend, 60*time.Second)
	after, err := createMongoDBProductionFixtureDocument(ctx, backend, collection, "after-election", "after election")
	if err != nil {
		t.Fatalf("same Store did not recover for a post-election write: %v", err)
	}
	waitForMongoDBProductionSecondary(t, fixture, oldPrimary, newPrimary, 45*time.Second)
	waitForMongoDBProductionReplicaSetHealth(t, adminClient, fixture.hosts(), newPrimary, 45*time.Second)

	for _, expected := range []store.Document{before, after} {
		found, findErr := findMongoDBProductionFixtureDocument(ctx, backend, collection, expected.ID)
		if findErr != nil {
			t.Fatalf("read %s after election: %v", expected.ID, findErr)
		}
		if found.ID != expected.ID || found.Revision != expected.Revision || found.Status != expected.Status {
			t.Fatalf("document after election = %#v, want identity/revision/status from %#v", found, expected)
		}
	}
}

func productionMongoDBStoreConfig(databaseURL, applicationName string) Config {
	return Config{
		DatabaseURL:            databaseURL,
		ApplicationName:        applicationName,
		ConnectTimeout:         5 * time.Second,
		ServerSelectionTimeout: 5 * time.Second,
		MaxPoolSize:            5,
	}
}

type mongoDBProductionFixture struct {
	project             string
	database            string
	root                string
	composePath         string
	startPath           string
	caPath              string
	wrongCAPath         string
	ports               []int
	adminUser           string
	adminPassword       string
	applicationUser     string
	applicationPassword string
	environment         []string
}

func newMongoDBProductionFixture(t *testing.T) *mongoDBProductionFixture {
	t.Helper()
	token := mongoDBProductionRandomHex(t, 6)
	root := t.TempDir()
	composePath, err := filepath.Abs(filepath.Join("testdata", "production", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	startPath, err := filepath.Abs(filepath.Join("testdata", "production", "start-member.sh"))
	if err != nil {
		t.Fatal(err)
	}
	fixture := &mongoDBProductionFixture{
		project:             "ridu-mongodb-production-" + token,
		database:            "ridu_production_" + token,
		root:                root,
		composePath:         composePath,
		startPath:           startPath,
		caPath:              filepath.Join(root, "ca.pem"),
		wrongCAPath:         filepath.Join(root, "wrong-ca.pem"),
		ports:               reserveMongoDBProductionPorts(t, 3),
		adminUser:           "ridu_fixture_admin",
		adminPassword:       mongoDBProductionRandomSecret(t, 32),
		applicationUser:     "ridu_fixture_application",
		applicationPassword: mongoDBProductionRandomSecret(t, 32),
	}
	writeMongoDBProductionTLSMaterials(t, fixture)
	fixture.environment = mongoDBProductionEnvironment(map[string]string{
		"RIDU_MONGODB_PRODUCTION_FIXTURE_ROOT":  fixture.root,
		"RIDU_MONGODB_PRODUCTION_FIXTURE_START": fixture.startPath,
		"RIDU_MONGODB_MEMBER_1_PORT":            fmt.Sprint(fixture.ports[0]),
		"RIDU_MONGODB_MEMBER_2_PORT":            fmt.Sprint(fixture.ports[1]),
		"RIDU_MONGODB_MEMBER_3_PORT":            fmt.Sprint(fixture.ports[2]),
	})
	return fixture
}

func (fixture *mongoDBProductionFixture) start(t *testing.T) {
	t.Helper()
	mongoDBProductionDeploymentImage(t, fixture.composePath)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		output, err := fixture.compose(ctx, "down", "--volumes", "--remove-orphans", "--timeout", "10")
		if err != nil {
			t.Errorf("clean up MongoDB production fixture: %v\n%s", err, output)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	if output, err := fixture.compose(ctx, "up", "-d"); err != nil {
		t.Fatalf("start MongoDB production fixture: %v\n%s", err, output)
	}
	waitForMongoDBProductionTLSListeners(t, fixture, 60*time.Second)
}

func (fixture *mongoDBProductionFixture) compose(ctx context.Context, arguments ...string) (string, error) {
	commandArguments := []string{"compose", "--project-name", fixture.project, "--file", fixture.composePath}
	command := exec.CommandContext(ctx, "docker", append(commandArguments, arguments...)...)
	command.Env = fixture.environment
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return string(output), ctx.Err()
	}
	return string(output), err
}

func (fixture *mongoDBProductionFixture) bootstrap(ctx context.Context, t *testing.T) {
	t.Helper()
	bootstrapURL := fixture.directURL(fixture.hosts()[0], "admin", "", "", fixture.caPath, true)
	bootstrap := connectMongoDBProductionClient(t, bootstrapURL, readpref.PrimaryPreferred())
	defer disconnectMongoDBProductionClient(t, bootstrap)
	configuration := bson.D{
		{Key: "_id", Value: mongoDBProductionReplicaSet},
		{Key: "members", Value: bson.A{
			bson.D{{Key: "_id", Value: int32(0)}, {Key: "host", Value: fixture.hosts()[0]}},
			bson.D{{Key: "_id", Value: int32(1)}, {Key: "host", Value: fixture.hosts()[1]}},
			bson.D{{Key: "_id", Value: int32(2)}, {Key: "host", Value: fixture.hosts()[2]}},
		}},
	}
	if err := bootstrap.Database("admin").RunCommand(ctx, bson.D{{Key: "replSetInitiate", Value: configuration}}).Err(); err != nil {
		t.Fatalf("initialize MongoDB production replica set: %v", err)
	}
	primaryURL := mongoDBProductionURL(fixture.hosts(), "admin", "", "", fixture.caPath, true, false, true)
	primaryClient := connectMongoDBProductionClient(
		t,
		primaryURL,
		readpref.PrimaryPreferred(),
	)
	defer disconnectMongoDBProductionClient(t, primaryClient)
	waitForMongoDBProductionPrimary(t, primaryClient, "", 45*time.Second)
	if err := primaryClient.Database("admin").RunCommand(ctx, bson.D{
		{Key: "createUser", Value: fixture.adminUser},
		{Key: "pwd", Value: fixture.adminPassword},
		{Key: "mechanisms", Value: bson.A{"SCRAM-SHA-256"}},
		{Key: "roles", Value: bson.A{bson.D{{Key: "role", Value: "root"}, {Key: "db", Value: "admin"}}}},
	}).Err(); err != nil {
		t.Fatalf("create MongoDB production fixture administrator: %v", err)
	}

	admin := fixture.authenticatedAdminClient(t, fixture.seedURL(fixture.caPath, true))
	defer disconnectMongoDBProductionClient(t, admin)
	if err := admin.Database(fixture.database).RunCommand(ctx, bson.D{
		{Key: "createUser", Value: fixture.applicationUser},
		{Key: "pwd", Value: fixture.applicationPassword},
		{Key: "mechanisms", Value: bson.A{"SCRAM-SHA-256"}},
		{Key: "roles", Value: bson.A{bson.D{{Key: "role", Value: "dbOwner"}, {Key: "db", Value: fixture.database}}}},
	}).Err(); err != nil {
		t.Fatalf("create scoped MongoDB production fixture application user: %v", err)
	}
}

func (fixture *mongoDBProductionFixture) proveRedactedConnectionFailures(t *testing.T) {
	t.Helper()
	wrongPassword := mongoDBProductionRandomSecret(t, 24)
	tests := []struct {
		name      string
		database  string
		forbidden []string
	}{
		{
			name:     "plaintext",
			database: fixture.applicationURL("", fixture.applicationPassword, false),
			forbidden: []string{
				fixture.applicationUser, fixture.applicationPassword, strings.Join(fixture.hosts(), ","),
			},
		},
		{
			name:     "wrong password",
			database: fixture.applicationURL(fixture.caPath, wrongPassword, true),
			forbidden: []string{
				fixture.applicationUser, wrongPassword, strings.Join(fixture.hosts(), ","),
			},
		},
		{
			name:     "wrong CA",
			database: fixture.applicationURL(fixture.wrongCAPath, fixture.applicationPassword, true),
			forbidden: []string{
				fixture.applicationUser, fixture.applicationPassword, strings.Join(fixture.hosts(), ","), fixture.wrongCAPath,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend, err := OpenWithConfig(t.Context(), productionMongoDBStoreConfig(test.database, "ridu-production-negative"))
			if backend != nil {
				_ = backend.Close()
				t.Fatal("invalid MongoDB production connection returned a Store")
			}
			if err == nil {
				t.Fatal("invalid MongoDB production connection was accepted")
			}
			for _, forbidden := range append(test.forbidden, test.database) {
				if forbidden != "" && strings.Contains(err.Error(), forbidden) {
					t.Fatalf("MongoDB %s error exposed credential-bearing connection detail: %v", test.name, err)
				}
			}
		})
	}
}

func (fixture *mongoDBProductionFixture) hosts() []string {
	hosts := make([]string, len(fixture.ports))
	for index, port := range fixture.ports {
		hosts[index] = fmt.Sprintf("localhost:%d", port)
	}
	return hosts
}

func (fixture *mongoDBProductionFixture) seedURL(caPath string, tlsEnabled bool) string {
	return mongoDBProductionURL(fixture.hosts(), "admin", fixture.adminUser, fixture.adminPassword, caPath, tlsEnabled, false, true)
}

func (fixture *mongoDBProductionFixture) applicationURL(caPath, password string, tlsEnabled bool) string {
	return mongoDBProductionURL(fixture.hosts(), fixture.database, fixture.applicationUser, password, caPath, tlsEnabled, false, true)
}

func (fixture *mongoDBProductionFixture) directURL(host, database, username, password, caPath string, tlsEnabled bool) string {
	return mongoDBProductionURL([]string{host}, database, username, password, caPath, tlsEnabled, true, false)
}

func mongoDBProductionURL(hosts []string, database, username, password, caPath string, tlsEnabled, direct, replicaSet bool) string {
	var userInfo string
	if username != "" {
		userInfo = url.UserPassword(username, password).String() + "@"
	}
	parameters := make(url.Values)
	if tlsEnabled {
		parameters.Set("tls", "true")
		parameters.Set("tlsCAFile", caPath)
	}
	if direct {
		parameters.Set("directConnection", "true")
	}
	if replicaSet {
		parameters.Set("replicaSet", mongoDBProductionReplicaSet)
	}
	return "mongodb://" + userInfo + strings.Join(hosts, ",") + "/" + url.PathEscape(database) + "?" + parameters.Encode()
}

type mongoDBProductionHello struct {
	SetName           string `bson:"setName"`
	Primary           string `bson:"primary"`
	IsWritablePrimary bool   `bson:"isWritablePrimary"`
	Secondary         bool   `bson:"secondary"`
}

type mongoDBProductionBuildInfo struct {
	Version string `bson:"version"`
}

type mongoDBProductionReplicaSetStatus struct {
	Set     string                                    `bson:"set"`
	Members []mongoDBProductionReplicaSetStatusMember `bson:"members"`
}

type mongoDBProductionReplicaSetStatusMember struct {
	Name        string  `bson:"name"`
	Health      float64 `bson:"health"`
	State       int32   `bson:"state"`
	StateString string  `bson:"stateStr"`
}

func assertMongoDBProductionServerVersion(t *testing.T, client *mongo.Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var buildInfo mongoDBProductionBuildInfo
	if err := client.Database("admin").RunCommand(ctx, bson.D{{Key: "buildInfo", Value: 1}}).Decode(&buildInfo); err != nil {
		t.Fatalf("inspect MongoDB production fixture server version: %v", err)
	}
	if buildInfo.Version != mongoDBProductionFixtureVersion {
		t.Fatalf("MongoDB production fixture server version = %q, want %q", buildInfo.Version, mongoDBProductionFixtureVersion)
	}
}

func waitForMongoDBProductionReplicaSetHealth(
	t *testing.T,
	client *mongo.Client,
	expectedHosts []string,
	expectedPrimary string,
	timeout time.Duration,
) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastStatus mongoDBProductionReplicaSetStatus
	var lastError error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		lastStatus = mongoDBProductionReplicaSetStatus{}
		lastError = client.Database("admin").RunCommand(
			ctx,
			bson.D{{Key: "replSetGetStatus", Value: 1}},
		).Decode(&lastStatus)
		cancel()
		if lastError == nil {
			lastError = validateMongoDBProductionReplicaSetHealth(lastStatus, expectedHosts, expectedPrimary)
		}
		if lastError == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf(
		"MongoDB production replica set did not reach exactly three healthy members with primary %q: %v; status=%#v",
		expectedPrimary,
		lastError,
		lastStatus,
	)
}

func validateMongoDBProductionReplicaSetHealth(
	status mongoDBProductionReplicaSetStatus,
	expectedHosts []string,
	expectedPrimary string,
) error {
	if status.Set != mongoDBProductionReplicaSet {
		return fmt.Errorf("replica set name = %q, want %q", status.Set, mongoDBProductionReplicaSet)
	}
	if len(expectedHosts) != 3 {
		return fmt.Errorf("qualified replica-set host count = %d, want 3", len(expectedHosts))
	}
	if len(status.Members) != 3 {
		return fmt.Errorf("healthy replica-set member count = %d, want 3", len(status.Members))
	}

	missingHosts := make(map[string]struct{}, len(expectedHosts))
	for _, host := range expectedHosts {
		missingHosts[host] = struct{}{}
	}
	primaryCount := 0
	secondaryCount := 0
	actualPrimary := ""
	for _, member := range status.Members {
		if _, expected := missingHosts[member.Name]; !expected {
			return fmt.Errorf("replica set reported unexpected or duplicate member %q", member.Name)
		}
		delete(missingHosts, member.Name)
		if member.Health != 1 {
			return fmt.Errorf("replica-set member %q health = %v, want 1", member.Name, member.Health)
		}
		switch {
		case member.State == 1 && member.StateString == "PRIMARY":
			primaryCount++
			actualPrimary = member.Name
		case member.State == 2 && member.StateString == "SECONDARY":
			secondaryCount++
		default:
			return fmt.Errorf(
				"replica-set member %q state = %d/%q, want PRIMARY or SECONDARY",
				member.Name,
				member.State,
				member.StateString,
			)
		}
	}
	if len(missingHosts) != 0 {
		return fmt.Errorf("replica set omitted configured members: %#v", missingHosts)
	}
	if primaryCount != 1 || secondaryCount != 2 {
		return fmt.Errorf("replica-set roles = %d PRIMARY/%d SECONDARY, want 1/2", primaryCount, secondaryCount)
	}
	if actualPrimary != expectedPrimary {
		return fmt.Errorf("replica-set primary = %q, want %q", actualPrimary, expectedPrimary)
	}
	return nil
}

func TestMongoDBProductionReplicaSetHealthRequiresThreeHealthyMembers(t *testing.T) {
	hosts := []string{"localhost:27017", "localhost:27018", "localhost:27019"}
	healthyStatus := func() mongoDBProductionReplicaSetStatus {
		return mongoDBProductionReplicaSetStatus{
			Set: mongoDBProductionReplicaSet,
			Members: []mongoDBProductionReplicaSetStatusMember{
				{Name: hosts[0], Health: 1, State: 1, StateString: "PRIMARY"},
				{Name: hosts[1], Health: 1, State: 2, StateString: "SECONDARY"},
				{Name: hosts[2], Health: 1, State: 2, StateString: "SECONDARY"},
			},
		}
	}
	if err := validateMongoDBProductionReplicaSetHealth(healthyStatus(), hosts, hosts[0]); err != nil {
		t.Fatalf("healthy production topology was rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*mongoDBProductionReplicaSetStatus)
	}{
		{
			name: "missing member",
			mutate: func(status *mongoDBProductionReplicaSetStatus) {
				status.Members = status.Members[:2]
			},
		},
		{
			name: "unhealthy member",
			mutate: func(status *mongoDBProductionReplicaSetStatus) {
				status.Members[2].Health = 0
			},
		},
		{
			name: "two primaries",
			mutate: func(status *mongoDBProductionReplicaSetStatus) {
				status.Members[2].State = 1
				status.Members[2].StateString = "PRIMARY"
			},
		},
		{
			name: "unexpected member",
			mutate: func(status *mongoDBProductionReplicaSetStatus) {
				status.Members[2].Name = "localhost:27020"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status := healthyStatus()
			test.mutate(&status)
			if err := validateMongoDBProductionReplicaSetHealth(status, hosts, hosts[0]); err == nil {
				t.Fatalf("invalid production topology was accepted: %#v", status)
			}
		})
	}
}

func waitForMongoDBProductionPrimary(t *testing.T, client *mongo.Client, previous string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastError error
	var lastHello mongoDBProductionHello
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		var hello mongoDBProductionHello
		lastError = client.Database("admin").RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello)
		cancel()
		lastHello = hello
		if lastError == nil && hello.SetName == mongoDBProductionReplicaSet && hello.IsWritablePrimary && hello.Primary != "" && hello.Primary != previous {
			return hello.Primary
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("MongoDB production fixture did not elect a primary different from %q: %v; hello=%#v", previous, lastError, lastHello)
	return ""
}

func stepDownMongoDBProductionPrimary(t *testing.T, fixture *mongoDBProductionFixture, primary string) {
	t.Helper()
	client := fixture.authenticatedAdminClient(t, fixture.directURL(primary, "admin", fixture.adminUser, fixture.adminPassword, fixture.caPath, true))
	defer disconnectMongoDBProductionClient(t, client)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	err := client.Database("admin").RunCommand(ctx, bson.D{
		{Key: "replSetStepDown", Value: 30},
		{Key: "secondaryCatchUpPeriodSecs", Value: 5},
		{Key: "force", Value: true},
	}).Err()
	if err != nil && !mongo.IsNetworkError(err) && !errors.Is(err, context.Canceled) {
		t.Fatalf("step down MongoDB production primary: %v", err)
	}
}

func waitForMongoDBProductionSecondary(t *testing.T, fixture *mongoDBProductionFixture, member, newPrimary string, timeout time.Duration) {
	t.Helper()
	client := fixture.authenticatedAdminClient(t, fixture.directURL(member, "admin", fixture.adminUser, fixture.adminPassword, fixture.caPath, true))
	defer disconnectMongoDBProductionClient(t, client)
	deadline := time.Now().Add(timeout)
	var lastError error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		var hello mongoDBProductionHello
		lastError = client.Database("admin").RunCommand(
			ctx,
			bson.D{{Key: "hello", Value: 1}},
			options.RunCmd().SetReadPreference(readpref.Nearest()),
		).Decode(&hello)
		cancel()
		if lastError == nil && hello.SetName == mongoDBProductionReplicaSet && hello.Secondary && hello.Primary == newPrimary {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("stepped-down MongoDB member %q did not rejoin primary %q as a secondary: %v", member, newPrimary, lastError)
}

func waitForMongoDBProductionStore(t *testing.T, backend *Store, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastError error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		lastError = backend.Ping(ctx)
		cancel()
		if lastError == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("existing MongoDB Store did not recover after election: %v", lastError)
}

func createMongoDBProductionFixtureDocument(ctx context.Context, backend *Store, collection schema.Collection, id, title string) (store.Document, error) {
	transaction, err := backend.Begin(ctx)
	if err != nil {
		return store.Document{}, err
	}
	document, err := transaction.Create(ctx, store.CreateRequest{
		Collection: collection,
		ID:         id,
		Values:     store.Values{"title": store.String(title), "rank": store.Number(1)},
	})
	if err != nil {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
		return store.Document{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return store.Document{}, err
	}
	return document, nil
}

func findMongoDBProductionFixtureDocument(ctx context.Context, backend *Store, collection schema.Collection, id string) (store.Document, error) {
	transaction, err := backend.BeginSnapshot(ctx)
	if err != nil {
		return store.Document{}, err
	}
	document, err := transaction.Find(ctx, store.Request{Collection: collection, ID: id})
	if err != nil {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
		return store.Document{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return store.Document{}, err
	}
	return document, nil
}

func (fixture *mongoDBProductionFixture) authenticatedAdminClient(t *testing.T, databaseURL string) *mongo.Client {
	t.Helper()
	return connectMongoDBProductionClient(t, databaseURL, readpref.PrimaryPreferred())
}

func connectMongoDBProductionClient(t *testing.T, databaseURL string, preference *readpref.ReadPref) *mongo.Client {
	t.Helper()
	client, err := mongo.Connect(options.Client().
		ApplyURI(databaseURL).
		SetConnectTimeout(5 * time.Second).
		SetServerSelectionTimeout(5 * time.Second).
		SetReadPreference(preference))
	if err != nil {
		t.Fatalf("construct MongoDB production fixture client: %v", err)
	}
	return client
}

func disconnectMongoDBProductionClient(t *testing.T, client *mongo.Client) {
	t.Helper()
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Disconnect(ctx); err != nil {
		t.Errorf("disconnect MongoDB production fixture client: %v", err)
	}
}

func waitForMongoDBProductionTLSListeners(t *testing.T, fixture *mongoDBProductionFixture, timeout time.Duration) {
	t.Helper()
	caPEM, err := os.ReadFile(fixture.caPath)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("parse MongoDB production fixture CA")
	}
	for _, port := range fixture.ports {
		address := fmt.Sprintf("127.0.0.1:%d", port)
		deadline := time.Now().Add(timeout)
		var lastError error
		for time.Now().Before(deadline) {
			dialer := &net.Dialer{Timeout: time.Second}
			connection, dialErr := tlsDialMongoDBProduction(dialer, address, roots)
			if dialErr == nil {
				_ = connection.Close()
				lastError = nil
				break
			}
			lastError = dialErr
			time.Sleep(200 * time.Millisecond)
		}
		if lastError != nil {
			t.Fatalf("MongoDB production TLS listener %s did not become ready: %v", address, lastError)
		}
	}
}

func tlsDialMongoDBProduction(dialer *net.Dialer, address string, roots *x509.CertPool) (net.Conn, error) {
	return tls.DialWithDialer(dialer, "tcp", address, &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
		ServerName: "localhost",
	})
}

func reserveMongoDBProductionPorts(t *testing.T, count int) []int {
	t.Helper()
	listeners := make([]net.Listener, 0, count)
	ports := make([]int, 0, count)
	for range count {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listeners = append(listeners, listener)
		ports = append(ports, listener.Addr().(*net.TCPAddr).Port)
	}
	for _, listener := range listeners {
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return ports
}

func mongoDBProductionEnvironment(overrides map[string]string) []string {
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, overridden := overrides[name]; !overridden {
			environment = append(environment, entry)
		}
	}
	for name, value := range overrides {
		environment = append(environment, name+"="+value)
	}
	return environment
}

func writeMongoDBProductionTLSMaterials(t *testing.T, fixture *mongoDBProductionFixture) {
	t.Helper()
	now := time.Now().UTC()
	ca, caKey, caPEM := newMongoDBProductionCA(t, "Ridu MongoDB production fixture CA", now)
	_, _, wrongCAPEM := newMongoDBProductionCA(t, "Ridu MongoDB wrong fixture CA", now)
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber:          mongoDBProductionSerial(t),
		Subject:               pkix.Name{CommonName: "localhost", Organization: []string{"Ridu test fixture"}},
		DNSNames:              []string{"localhost"},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, ca, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	serverPEM := append(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)})...,
	)
	writeMongoDBProductionFile(t, fixture.caPath, caPEM, 0o600)
	writeMongoDBProductionFile(t, fixture.wrongCAPath, wrongCAPEM, 0o600)
	writeMongoDBProductionFile(t, filepath.Join(fixture.root, "server.pem"), serverPEM, 0o600)
	keyMaterial := make([]byte, 96)
	if _, err := rand.Read(keyMaterial); err != nil {
		t.Fatal(err)
	}
	keyfile := []byte(base64.RawStdEncoding.EncodeToString(keyMaterial) + "\n")
	writeMongoDBProductionFile(t, filepath.Join(fixture.root, "keyfile"), keyfile, 0o600)
}

func newMongoDBProductionCA(t *testing.T, commonName string, now time.Time) (*x509.Certificate, *rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          mongoDBProductionSerial(t),
		Subject:               pkix.Name{CommonName: commonName, Organization: []string{"Ridu test fixture"}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	encoded, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return template, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: encoded})
}

func mongoDBProductionSerial(t *testing.T) *big.Int {
	t.Helper()
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		t.Fatal(err)
	}
	return serial
}

func mongoDBProductionRandomSecret(t *testing.T, size int) string {
	t.Helper()
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(value)
}

func mongoDBProductionRandomHex(t *testing.T, size int) string {
	t.Helper()
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(value)
}

func writeMongoDBProductionFile(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatal(err)
	}
}
