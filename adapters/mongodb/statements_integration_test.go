package mongodb

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// mongoMonitorCommands routes backend through a client that records the
// commands sent to its database until the test ends.
func mongoMonitorCommands(t *testing.T, backend *Store) func() []*event.CommandStartedEvent {
	t.Helper()
	var mu sync.Mutex
	var commands []*event.CommandStartedEvent
	databaseName := backend.database.Name()
	parsed, err := url.Parse(strings.TrimSpace(os.Getenv("RIDU_MONGODB_URL")))
	if err != nil {
		t.Fatal("RIDU_MONGODB_URL is not a valid MongoDB URL")
	}
	parsed.Path = "/" + databaseName
	clientOptions, _, err := normalizedClientOptions(Config{
		DatabaseURL: parsed.String(), AllowInsecureTransport: true,
		ConnectTimeout: 10 * time.Second, ServerSelectionTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	clientOptions.SetMonitor(&event.CommandMonitor{Started: func(_ context.Context, started *event.CommandStartedEvent) {
		if started.DatabaseName != databaseName {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		commands = append(commands, started)
	}})
	monitored, err := mongo.Connect(clientOptions)
	if err != nil {
		t.Fatal(err)
	}
	originalClient, originalDatabase := backend.client, backend.database
	backend.client, backend.database = monitored, monitored.Database(databaseName)
	t.Cleanup(func() {
		backend.client, backend.database = originalClient, originalDatabase
		disconnect, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = monitored.Disconnect(disconnect)
	})
	return func() []*event.CommandStartedEvent {
		mu.Lock()
		defer mu.Unlock()
		observed := commands
		commands = nil
		return observed
	}
}

// Every read an operation transaction sends carries a server time limit no
// later than its caller's deadline, and a list over a deep block schema sends
// bounded commands.
func TestMongoDBDocumentReadsCarryABoundedServerTimeLimit(t *testing.T) {
	backend := mongoIntegrationStore(t)
	manifest := mongoBlockGraphManifest(t, 8)
	if err := backend.syncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	pages := mongoManifestCollection(t, manifest, "pages")
	layout := store.List(
		store.Object(store.Values{"_key": store.String("a"), "blockType": store.String("section-8"), "title": store.String("Outer"), "content": store.List(
			store.Object(store.Values{"_key": store.String("b"), "blockType": store.String("section-3"), "content": store.List(
				store.Object(store.Values{"_key": store.String("c"), "blockType": store.String("text"), "heading": store.String("Inner")}),
			)}),
		)}),
	)
	write := mongoBegin(t, backend, false)
	for index := range 3 {
		if _, err := write.Create(t.Context(), store.CreateRequest{Collection: pages, ID: fmt.Sprintf("page-%d", index), Status: store.StatusPublished, Values: store.Values{
			"title": store.String(fmt.Sprintf("Page %d", index)), "layout": layout,
		}}); err != nil {
			mongoRollback(t, write)
			t.Fatal(err)
		}
	}
	mongoCommit(t, write)
	commands := mongoMonitorCommands(t, backend)
	titlePath, err := query.ParsePath("title")
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name    string
		timeout time.Duration
		ceiling time.Duration
	}{
		{name: "caller deadline", timeout: 5 * time.Second, ceiling: 5 * time.Second},
		{name: "no caller deadline", ceiling: mongoStatementTimeLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			if test.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, test.timeout)
				defer cancel()
			}
			commands()
			read, err := backend.BeginSnapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer mongoRollback(t, read)
			// Both list read paths: a find, and the aggregate a computed
			// localized sort would need is exercised by the count.
			page, err := read.List(ctx, store.Request{Collection: pages, PublishedOnly: true, Limit: 2, Sort: []query.Sort{query.Desc(titlePath)}})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Documents) != 2 || page.Total == nil || *page.Total != 3 {
				t.Fatalf("page = %d documents, total %v", len(page.Documents), page.Total)
			}
			reads := 0
			for _, command := range commands() {
				switch command.CommandName {
				case "find", "aggregate":
				default:
					continue
				}
				reads++
				if size := len(command.Command); size > 64*1024 {
					t.Fatalf("%s command is %d bytes", command.CommandName, size)
				}
				maximum, ok := command.Command.Lookup("maxTimeMS").AsInt64OK()
				if !ok || maximum <= 0 || maximum > test.ceiling.Milliseconds() {
					t.Fatalf("%s maxTimeMS = %v, want (0, %d]", command.CommandName, command.Command.Lookup("maxTimeMS"), test.ceiling.Milliseconds())
				}
			}
			if reads != 2 {
				t.Fatalf("list sent %d count and find commands, want 2", reads)
			}
		})
	}
}
