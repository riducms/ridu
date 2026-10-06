package datatransform

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/schematest"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

type recordedDocuments struct {
	stored  store.Document
	finds   []store.Request
	updates []store.UpdateRequest
	deleted []store.DocumentReference
}

func (documents *recordedDocuments) Create(context.Context, store.CreateRequest) (store.Document, error) {
	return store.Document{}, nil
}

func (documents *recordedDocuments) Find(_ context.Context, request store.Request) (store.Document, error) {
	documents.finds = append(documents.finds, request)
	return store.CloneDocument(documents.stored), nil
}

func (documents *recordedDocuments) List(context.Context, store.Request) (store.Page, error) {
	return store.Page{}, nil
}

func (documents *recordedDocuments) Update(_ context.Context, request store.UpdateRequest) (store.Document, error) {
	documents.updates = append(documents.updates, request)
	return *request.Current, nil
}

func (documents *recordedDocuments) Trash(context.Context, store.Request) (store.Document, error) {
	return store.Document{}, nil
}

func (documents *recordedDocuments) Restore(context.Context, store.Request) (store.Document, error) {
	return store.Document{}, nil
}

func (documents *recordedDocuments) Delete(_ context.Context, request store.Request) (store.Document, error) {
	return store.Document{ID: request.ID}, nil
}

func (documents *recordedDocuments) DeleteDocumentState(_ context.Context, reference store.DocumentReference) error {
	documents.deleted = append(documents.deleted, reference)
	return nil
}

func transformFixture(fields ...schema.Field) (schema.Collection, migration.Artifact) {
	collection := schema.Collection{ID: "notes", Slug: "notes", Fields: fields}
	versioned := schema.Collection{ID: "posts", Slug: "posts", Versions: &schema.VersionSettings{}, Fields: fields}
	return collection, migration.Artifact{After: schema.Snapshot{
		Application: schema.Application{Localization: &schema.LocalizationSettings{Locales: []schema.Locale{{Code: "en"}}}},
		Collections: []schema.Collection{collection, versioned},
	}}
}

func textField(name string) schema.Field {
	path, _ := query.ParsePath(name)
	return schema.Field{ID: schema.StableID("notes-" + name), Name: name, Path: path, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar}
}

// A transform update never supplies the current document: the transaction
// reads the whole stored row with the update's own predicates and a mutation
// lock, whatever projection the callback used for its own reads.
func TestUpdateReadsTheWholeLockedStoredDocument(t *testing.T) {
	collection, artifact := transformFixture(textField("title"), textField("body"))
	stored := store.Document{ID: "one", Values: store.Values{"title": store.String("Stored"), "body": store.String("Kept")}}
	documents := &recordedDocuments{stored: stored}
	transaction := New(documents, artifact, Options{Engine: "Test"})
	title, _ := query.ParsePath("title")
	access := query.Equal("title", "Stored").Node()
	request := store.Request{
		Collection: collection, ID: "one", Access: &access, ExpectedRevision: 4, Deletion: store.DeletionAll,
		Select: []query.Path{title}, Populate: []query.Population{{Path: title}}, Lock: store.LockReference, PublishedOnly: true,
	}
	if _, err := transaction.Update(t.Context(), migration.UpdateRequest{Request: request, Values: store.Values{"title": store.String("Changed")}}); err != nil {
		t.Fatal(err)
	}
	if len(documents.finds) != 1 || len(documents.updates) != 1 {
		t.Fatalf("finds %d updates %d, want one locked read and one update", len(documents.finds), len(documents.updates))
	}
	read := documents.finds[0]
	if read.Lock != store.LockMutation || read.Select != nil || read.Populate != nil || read.ExpectedRevision != 0 || read.PublishedOnly ||
		read.ID != "one" || read.Access != &access || read.Deletion != store.DeletionAll {
		t.Fatalf("current read = %#v, want the update's document read whole with LockMutation", read)
	}
	update := documents.updates[0]
	if update.Current == nil || update.Current.Values["body"].Kind() != store.ValueString || update.ExpectedRevision != 4 {
		t.Fatalf("update = %#v, want the stored document as Current and the caller's expected revision", update)
	}
}

func TestDeleteRemovesTheDocumentState(t *testing.T) {
	collection, artifact := transformFixture(textField("title"))
	documents := &recordedDocuments{}
	transaction := New(documents, artifact, Options{Engine: "Test"})
	if _, err := transaction.Delete(t.Context(), store.Request{Collection: collection, ID: "one"}); err != nil {
		t.Fatal(err)
	}
	if len(documents.deleted) != 1 || documents.deleted[0] != (store.DocumentReference{CollectionID: "notes", DocumentID: "one"}) {
		t.Fatalf("deleted state = %#v", documents.deleted)
	}
}

func TestRequestsMustMatchTheArtifactManifests(t *testing.T) {
	collection, artifact := transformFixture(textField("title"))
	transaction := New(&recordedDocuments{}, artifact, Options{Engine: "Test"})
	forged := collection
	forged.Fields = append([]schema.Field{}, collection.Fields...)
	forged.Fields[0].Required = true
	for name, run := range map[string]func() error{
		"forged shape": func() error {
			_, err := transaction.Find(t.Context(), store.Request{Collection: forged, ID: "one"})
			return err
		},
		"unknown locale": func() error {
			_, err := transaction.Find(t.Context(), store.Request{Collection: collection, ID: "one", Locales: []schema.LocaleCode{"fr"}})
			return err
		},
		"versioned mutation": func() error {
			_, err := transaction.Update(t.Context(), migration.UpdateRequest{Request: store.Request{Collection: artifact.After.Collections[1], ID: "one"}})
			return err
		},
		"unknown value": func() error {
			_, err := transaction.Update(t.Context(), migration.UpdateRequest{Request: store.Request{Collection: collection, ID: "one"}, Values: store.Values{"missing": store.String("x")}})
			return err
		},
	} {
		if err := run(); err == nil || !strings.HasPrefix(err.Error(), "Test data transform") {
			t.Fatalf("%s error = %v", name, err)
		}
	}
}

func TestBlockRowBoundsAreValidated(t *testing.T) {
	path, _ := query.ParsePath("layout")
	hero := schema.BlockType{Slug: "hero", TypeName: "Hero", Labels: schema.BlockLabels{Singular: "Hero"}, Fields: []schema.Field{}}
	bind := func(localized bool) schema.Field {
		return schematest.Bind(t, "posts", []schema.BlockType{hero}, schema.Field{
			ID: "layout", Name: "layout", Path: path, Type: schema.FieldTypeBlocks, Category: schema.FieldCategoryNested, Localized: localized,
			Blocks: &schema.BlocksField{MinRows: 2, MaxRows: 3, BlockReferences: []string{"hero"}},
		})[0]
	}
	field := bind(false)
	rows := func(count int) store.Value {
		values := make([]store.Value, count)
		for index := range values {
			values[index] = store.Object(store.Values{"_key": store.String(fmt.Sprintf("row-%d", index)), "blockType": store.String("hero")})
		}
		return store.List(values...)
	}
	transaction := &Transaction{engine: "Test", locales: map[string]struct{}{"en": {}}}
	validate := func(field schema.Field, values store.Values) error {
		return transaction.validateValues([]schema.Field{field}, values, "posts", nil)
	}
	for _, values := range []store.Values{{}, {"layout": store.Null()}} {
		if err := validate(field, values); err != nil {
			t.Fatalf("optional absent/null: %v", err)
		}
	}
	for _, count := range []int{0, 1, 2, 3, 4} {
		err := validate(field, store.Values{"layout": rows(count)})
		if invalid := count < 2 || count > 3; (err != nil) != invalid {
			t.Fatalf("%d rows: %v", count, err)
		}
		if err != nil && !strings.Contains(err.Error(), "layout") {
			t.Fatalf("missing precise field path: %v", err)
		}
	}
	field = bind(true)
	if err := validate(field, store.Values{"layout": store.Object(store.Values{"en": rows(1)})}); err == nil || !strings.Contains(err.Error(), "layout.en") {
		t.Fatalf("localized bounds: %v", err)
	}
}
