package content

import (
	"encoding/json"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

func TestLocalExamplesPreserveValuesAndRowIdentity(t *testing.T) {
	app, err := ridu.New(ridu.Config{
		Name:        "Document value examples",
		Collections: []ridu.Collection{Pages},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	options := ridu.MutationOptions{}
	page, err := CreatePage(t.Context(), app, options)
	if err != nil {
		t.Fatal(err)
	}
	if page.ID == "" || page.CreatedAt.IsZero() || page.UpdatedAt.IsZero() {
		t.Fatalf("missing document metadata: %#v", page)
	}
	if _, exists := page.Values["id"]; exists {
		t.Fatal("document ID appeared in authored field values")
	}
	seo, ok := page.Values["seo"].CopyObject()
	if title, valid := seo["title"].StringValue(); !ok || !valid || title != "Welcome to Acme" {
		t.Fatalf("unexpected SEO title: %q", title)
	}
	rows, ok := page.Values["links"].CopyList()
	if !ok || len(rows) != 1 {
		t.Fatalf("unexpected links: %#v", rows)
	}
	row, ok := rows[0].CopyObject()
	key, hasKey := row["_key"].StringValue()
	if !ok || !hasKey || key == "" {
		t.Fatalf("row has no assigned key: %#v", row)
	}
	updated, err := RenamePageLink(t.Context(), app, page, "About Acme", options)
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := updated.Values["title"].StringValue(); title != "Home" {
		t.Fatalf("omitted title changed: %q", title)
	}
	if summary, _ := updated.Values["summary"].StringValue(); summary != "Start here" {
		t.Fatalf("omitted summary changed: %q", summary)
	}
	newRows, _ := updated.Values["links"].CopyList()
	newRow, _ := newRows[0].CopyObject()
	if newKey, _ := newRow["_key"].StringValue(); newKey != key {
		t.Fatalf("row key changed: %q -> %q", key, newKey)
	}
	if label, _ := newRow["label"].StringValue(); label != "About Acme" {
		t.Fatalf("label did not change: %q", label)
	}
	oldRows, _ := page.Values["links"].CopyList()
	oldRow, _ := oldRows[0].CopyObject()
	if label, _ := oldRow["label"].StringValue(); label != "About" {
		t.Fatalf("original snapshot changed: %q", label)
	}

	// A map edit only changes this response. It is not a database write.
	updated.Values["title"] = store.String("Unsaved")
	found, err := app.Local().Find(t.Context(), "pages", page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := found.Values["title"].StringValue(); title != "Home" {
		t.Fatalf("local map edit was persisted: %q", title)
	}
	cleared, err := app.Local().Update(t.Context(), "pages", page.ID, store.Values{
		"summary": store.Null(),
		"links":   store.List(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Values["summary"].Kind() != store.ValueNull {
		t.Fatalf("explicit null was not retained: %#v", cleared.Values["summary"])
	}
	if links, ok := cleared.Values["links"].CopyList(); !ok || len(links) != 0 {
		t.Fatalf("empty list did not remove rows: %#v", links)
	}
}

func TestRenameFirstLinkRejectsInvalidInput(t *testing.T) {
	for _, value := range []store.Value{
		store.Null(), store.String("link"), store.List(), store.List(store.String("link")),
	} {
		if _, err := RenameFirstLink(value, "Updated"); err == nil {
			t.Fatalf("accepted invalid links: %#v", value)
		}
	}
}

func TestEmptyValuesAndClonedMaps(t *testing.T) {
	for _, test := range []struct {
		value store.Value
		json  string
	}{
		{store.Null(), "null"},
		{store.String(""), `""`},
		{store.Number(0), "0"},
		{store.Boolean(false), "false"},
		{store.List(), "[]"},
		{store.Object(nil), "{}"},
	} {
		encoded, err := json.Marshal(test.value)
		if err != nil || string(encoded) != test.json {
			t.Fatalf("JSON = %s, error = %v; want %s", encoded, err, test.json)
		}
	}
	if _, err := json.Marshal(store.Value{}); err == nil {
		t.Fatal("zero Value silently became null")
	}
	original := store.Values{"title": store.String("Original")}
	copy := store.CloneValues(original)
	copy["title"] = store.String("Edited")
	if title, _ := original["title"].StringValue(); title != "Original" {
		t.Fatal("cloned map shared mutable storage")
	}
	populated := store.Populated(store.Document{ID: "user-1", Values: original})
	document, ok := populated.CopyDocument()
	if !ok || document.ID != "user-1" {
		t.Fatalf("populated document = %#v, %v", document, ok)
	}
	if _, ok := populated.StringValue(); ok {
		t.Fatal("populated document was treated as an ID string")
	}
}
