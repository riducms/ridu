package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/riducms/ridu"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
)

func TestMongoFixtureOwnsOnlyGeneratedDatabase(t *testing.T) {
	const base = "mongodb://127.0.0.1:27017/unrelated?replicaSet=ridu-rs0&directConnection=true"
	first, err := mongoFixtureURL(base)
	if err != nil {
		t.Fatal(err)
	}
	second, err := mongoFixtureURL(base)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("workers shared a database")
	}
	parsed, err := url.Parse(first)
	if err != nil {
		t.Fatal(err)
	}
	if !mongoFixtureDatabasePattern.MatchString(parsed.Path[1:]) || parsed.Query().Get("replicaSet") != "ridu-rs0" {
		t.Fatal("fixture lost isolation or topology settings")
	}
	if err := resetMongoFixture(t.Context(), base); err == nil {
		t.Fatal("reset accepted unrelated database")
	}
}

func TestMongoFixtureResetReopensFreshStore(t *testing.T) {
	base := os.Getenv("RIDU_MONGODB_URL")
	if base == "" {
		t.Skip("set RIDU_MONGODB_URL for live isolated fixture reset")
	}
	owned, err := mongoFixtureURL(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := resetMongoFixture(ctx, owned); err != nil {
			t.Error(err)
		}
	})
	config := ridu.Config{Name: "Mongo browser reset", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
		field.Text("title"),
	}}}}
	backend, closeBackend, err := fixtureBackend(t.Context(), config, "", "", owned)
	if err != nil {
		t.Fatal(err)
	}
	app, err := ridu.New(config, backend)
	if err != nil {
		closeBackend()
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"title": store.String("Reset me")}, nil)
	if err != nil {
		closeBackend()
		t.Fatal(err)
	}
	closeBackend()
	if err := resetMongoFixture(t.Context(), owned); err != nil {
		t.Fatal(err)
	}
	backend, closeBackend, err = fixtureBackend(t.Context(), config, "", "", owned)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBackend()
	app, err = ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Local().Find(t.Context(), "pages", created.ID, nil); err == nil {
		t.Fatal("reset retained a document")
	} else {
		var failure *ridu.OperationError
		if !errors.As(err, &failure) || failure.Code != "not_found" {
			t.Fatalf("fresh store read failed unexpectedly: %v", err)
		}
	}
	if _, err := app.Local().Create(t.Context(), "pages", store.Values{"title": store.String("Fresh")}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMongoFixtureSeedsFullAdminConfig(t *testing.T) {
	base := os.Getenv("RIDU_MONGODB_URL")
	if base == "" {
		t.Skip("set RIDU_MONGODB_URL for the live admin seed fixture")
	}
	owned, err := mongoFixtureURL(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := resetMongoFixture(ctx, owned); err != nil {
			t.Error(err)
		}
	})
	storage, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := fixtureConfig(storage)
	backend, closeBackend, err := fixtureBackend(t.Context(), config, "", "", owned)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBackend()
	app, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seedFixture(t.Context(), app); err != nil {
		for cause := err; cause != nil; cause = errors.Unwrap(cause) {
			t.Logf("seed error: %v", cause)
		}
		t.Fatal(err)
	}
}
