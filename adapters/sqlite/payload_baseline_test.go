package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func TestSQLitePayloadBaselineWorkflow(t *testing.T) {
	ctx := context.Background()
	config := ridu.Config{
		Name: "SQLite Payload baseline",
		Collections: []ridu.Collection{
			{Slug: "authors", Fields: []field.Definition{field.Text("name", field.Required())}},
			{Slug: "categories", Fields: []field.Definition{field.Text("name", field.Required())}},
			{
				Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
				Fields: []field.Definition{
					field.Text("title", field.Required()),
					field.Textarea("summary"),
					field.Relationship("author", field.To("authors"), field.Required()),
					field.Relationship("category", field.To("categories"), field.Required()),
				},
			},
		},
	}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	migrations := t.TempDir()
	if _, err := CreateArtifact(ctx, migrations, "initial", manifest, time.Unix(1, 0), false); err != nil {
		t.Fatal(err)
	}
	backend, err := Open(ctx, filepath.Join(t.TempDir(), "baseline.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.ApplyArtifacts(ctx, migrations); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, migrations); err != nil {
		t.Fatalf("repeat immutable migration: %v", err)
	}
	statuses, err := backend.ArtifactStatus(ctx, migrations, manifest)
	if err != nil || len(statuses) != 1 || !statuses[0].Applied {
		t.Fatalf("migration ledger = %#v, %v", statuses, err)
	}
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}

	author, err := application.Local().Create(ctx, "authors", store.Values{"name": store.String("Ada Lovelace")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	category, err := application.Local().Create(ctx, "categories", store.Values{"name": store.String("Release notes")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{
		"title":    store.String("Pinned Payload SQLite"),
		"summary":  store.String("Initial SQLite baseline"),
		"author":   store.String(author.ID),
		"category": store.String(category.ID),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(ctx, "posts", store.Values{
		"title":    store.String("Unrelated Payload SQLite"),
		"summary":  store.String("Nonmatching SQLite baseline"),
		"author":   store.String(author.ID),
		"category": store.String(category.ID),
	}, nil); err != nil {
		t.Fatal(err)
	}
	includeDrafts := true
	read, err := application.Local().FindWithOptions(ctx, "posts", post.ID, ridu.FindOptions{Draft: &includeDrafts})
	if err != nil || read.ID != post.ID {
		t.Fatalf("created post read = %#v, %v", read, err)
	}
	updated, err := application.Local().Update(ctx, "posts", post.ID, store.Values{"summary": store.String("Updated SQLite baseline")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary, _ := updated.Values["summary"].StringValue(); summary != "Updated SQLite baseline" {
		t.Fatalf("updated post summary = %q", summary)
	}

	authorPath, _ := query.ParsePath("author")
	categoryPath, _ := query.ParsePath("category")
	titlePath, _ := query.ParsePath("title")
	page, err := application.Local().List(ctx, "posts", ridu.ListOptions{
		Draft: &includeDrafts, Where: query.Equal(titlePath, query.String("Pinned Payload SQLite")),
		Populate: []query.Population{
			{Path: authorPath}, {Path: categoryPath},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Documents) != 1 {
		t.Fatalf("populated post page = %#v", page)
	}
	if page.Documents[0].ID != post.ID {
		t.Fatalf("filtered post ID = %q, want %q", page.Documents[0].ID, post.ID)
	}
	if title, _ := page.Documents[0].Values["title"].StringValue(); title != "Pinned Payload SQLite" {
		t.Fatalf("filtered post title = %q", title)
	}
	populatedAuthor, ok := page.Documents[0].Values["author"].DocumentValue()
	if !ok || populatedAuthor.ID != author.ID {
		t.Fatalf("populated author = %#v", page.Documents[0].Values["author"])
	}
	if name, _ := populatedAuthor.Values["name"].StringValue(); name != "Ada Lovelace" {
		t.Fatalf("populated author name = %q", name)
	}
	populatedCategory, ok := page.Documents[0].Values["category"].DocumentValue()
	if !ok || populatedCategory.ID != category.ID {
		t.Fatalf("populated category = %#v", page.Documents[0].Values["category"])
	}
	if name, _ := populatedCategory.Values["name"].StringValue(); name != "Release notes" {
		t.Fatalf("populated category name = %q", name)
	}
	versions, err := application.Local().Versions(ctx, "posts", post.ID, nil)
	if err != nil || len(versions) != 2 {
		t.Fatalf("post versions = %#v, %v", versions, err)
	}
	foundInitialVersion := false
	foundUpdatedVersion := false
	for _, version := range versions {
		summary, _ := version.Snapshot.Values["summary"].StringValue()
		if summary == "Initial SQLite baseline" {
			foundInitialVersion = true
		}
		if summary == "Updated SQLite baseline" {
			foundUpdatedVersion = true
		}
	}
	if !foundInitialVersion {
		t.Fatalf("original post version was not retained: %#v", versions)
	}
	if !foundUpdatedVersion {
		t.Fatalf("updated post version was not retained: %#v", versions)
	}
	if _, err := application.Local().Delete(ctx, "posts", post.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().FindWithOptions(ctx, "posts", post.ID, ridu.FindOptions{Draft: &includeDrafts}); !hasOperationCode(err, "not_found") {
		t.Fatalf("find deleted post error = %v", err)
	}
	if err := backend.DownArtifacts(ctx, migrations); err != nil {
		t.Fatalf("reverse committed migration: %v", err)
	}
	statuses, err = backend.ArtifactStatus(ctx, migrations, manifest)
	if err != nil || len(statuses) != 1 || statuses[0].Applied {
		t.Fatalf("migration ledger after down = %#v, %v", statuses, err)
	}
	if err := backend.ApplyArtifacts(ctx, migrations); err != nil {
		t.Fatalf("reapply committed migration: %v", err)
	}
	statuses, err = backend.ArtifactStatus(ctx, migrations, manifest)
	if err != nil || len(statuses) != 1 || !statuses[0].Applied {
		t.Fatalf("migration ledger after reapply = %#v, %v", statuses, err)
	}
}

func hasOperationCode(err error, code string) bool {
	var operationError *ridu.OperationError
	return errors.As(err, &operationError) && operationError.Code == code
}
