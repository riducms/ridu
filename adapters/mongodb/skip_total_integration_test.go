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
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// A SkipTotal list must not count: its only read of the documents is one find
// that requests a single overflow document beyond the limit.
func TestMongoDBSkipTotalListExecutesNoCount(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoWindowCollection(t)
	manifest := schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "MongoDB skip total"},
		Collections: []schema.Collection{collection}, Plugins: []schema.Plugin{},
	})
	if err := backend.syncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	write := mongoBegin(t, backend, false)
	for index, slug := range []string{"apple", "banana", "cherry"} {
		if _, err := write.Create(t.Context(), store.CreateRequest{
			Collection: collection, ID: fmt.Sprintf("skip-total-%d", index),
			Values: store.Values{"slug": store.String(slug), "title": store.String(slug)},
		}); err != nil {
			mongoRollback(t, write)
			t.Fatal(err)
		}
	}
	mongoCommit(t, write)

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

	slug := []query.Sort{query.Asc(mongoWindowPath(t, "slug"))}
	for _, test := range []struct {
		name      string
		request   store.Request
		documents int
		next      bool
		counted   bool
		readLimit int64
	}{
		{name: "first page", request: store.Request{Collection: collection, Sort: slug, Page: 1, Limit: 2, SkipTotal: true}, documents: 2, next: true, readLimit: 3},
		{name: "last page", request: store.Request{Collection: collection, Sort: slug, Page: 2, Limit: 2, SkipTotal: true}, documents: 1, readLimit: 3},
		{name: "counted page", request: store.Request{Collection: collection, Sort: slug, Page: 1, Limit: 2}, documents: 2, next: true, counted: true, readLimit: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			read := mongoBegin(t, backend, true)
			mu.Lock()
			commands = nil
			mu.Unlock()
			page, err := read.List(t.Context(), test.request)
			mu.Lock()
			observed := commands
			mu.Unlock()
			mongoRollback(t, read)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Documents) != test.documents || page.HasNextPage != test.next || (page.Total != nil) != test.counted {
				t.Fatalf("page = documents %d next %t total %v, want %d/%t, total only when counted", len(page.Documents), page.HasNextPage, page.Total, test.documents, test.next)
			}
			counts, finds := 0, 0
			for _, command := range observed {
				switch command.CommandName {
				case "aggregate", "count":
					counts++
				case "find":
					finds++
					if limit, ok := command.Command.Lookup("limit").AsInt64OK(); !ok || limit != test.readLimit {
						t.Fatalf("find limit = %v, want %d", command.Command.Lookup("limit"), test.readLimit)
					}
				}
			}
			wantCounts := 0
			if test.counted {
				wantCounts = 1
			}
			if counts != wantCounts || finds != 1 {
				t.Fatalf("list ran %d counts and %d finds, want %d and 1", counts, finds, wantCounts)
			}
		})
	}
}
