package operation

import (
	"errors"
	"testing"

	"github.com/riducms/ridu/internal/teststore"
	operationkind "github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestCollectionListAccessUsesFullDocumentSnapshots(t *testing.T) {
	titlePath, _ := query.NewPath("title")
	lockedPath, _ := query.NewPath("locked")
	title := schema.Field{ID: "posts-title", Name: "title", Path: titlePath, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar}
	locked := schema.Field{ID: "posts-locked", Name: "locked", Path: lockedPath, Type: schema.FieldTypeCheckbox, Category: schema.FieldCategoryScalar}
	backend := &transactionIntentRecordingStore{Store: teststore.New()}
	collection := Collection{
		Schema: schema.Collection{ID: "collection-posts", Slug: "posts", Fields: []schema.Field{title, locked}},
		Access: map[operationkind.Kind]Access{
			operationkind.Update: func(ctx Context) (Decision, error) {
				isLocked, _ := ctx.Data["locked"].BooleanValue()
				if isLocked {
					return Decision{Kind: Deny}, nil
				}
				return Decision{Kind: Allow}, nil
			},
		},
		Bindings: []FieldBinding{{
			ID: "title", Field: title,
			Access: FieldRules{Read: func(ctx Context) (bool, error) {
				isLocked, _ := ctx.SiblingData["locked"].BooleanValue()
				return !isLocked, nil
			}},
		}},
	}
	engine, err := New(Config{Store: backend, Collections: []Collection{collection}})
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		id     string
		locked bool
	}{
		{id: "editable", locked: false},
		{id: "locked", locked: true},
	} {
		_, err := engine.Execute(t.Context(), Request{
			Operation: operationkind.Create, Collection: "posts", ImportID: fixture.id,
			Data: store.Values{"title": store.String(fixture.id), "locked": store.Boolean(fixture.locked)},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	backend.writeBegins, backend.snapshotBegins = 0, 0

	result, err := engine.Execute(t.Context(), Request{
		Operation: operationkind.Read, Collection: "posts", Page: 1, Limit: 10,
		Select: []query.Path{titlePath}, IncludeAccess: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if backend.writeBegins != 0 || backend.snapshotBegins != 1 {
		t.Fatalf("enriched list transactions = write:%d snapshot:%d, want write:0 snapshot:1", backend.writeBegins, backend.snapshotBegins)
	}
	if result.Page == nil || result.PageAccess == nil || len(result.Page.Documents) != 2 || len(result.PageAccess.Documents) != 2 {
		t.Fatalf("enriched page = page:%#v access:%#v", result.Page, result.PageAccess)
	}
	if !result.PageAccess.Collection.Operations.Update {
		t.Fatal("collection-level update capability was not returned")
	}
	if !result.PageAccess.Documents["editable"].Operations.Update || result.PageAccess.Documents["locked"].Operations.Update {
		t.Fatalf("document update capabilities = %#v", result.PageAccess.Documents)
	}
	if !result.PageAccess.Documents["editable"].Fields["title"].Read || result.PageAccess.Documents["locked"].Fields["title"].Read {
		t.Fatalf("document field capabilities = %#v", result.PageAccess.Documents)
	}
}

func TestCollectionListAccessFailureFailsTheRequest(t *testing.T) {
	titlePath, _ := query.NewPath("title")
	collection := Collection{
		Schema: schema.Collection{
			ID: "collection-posts", Slug: "posts",
			Fields: []schema.Field{{ID: "posts-title", Name: "title", Path: titlePath, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar}},
		},
		Access: map[operationkind.Kind]Access{
			operationkind.Update: func(ctx Context) (Decision, error) {
				if ctx.ID == "broken" {
					return Decision{}, errors.New("capability backend failed")
				}
				return Decision{Kind: Allow}, nil
			},
		},
	}
	engine, err := New(Config{Store: teststore.New(), Collections: []Collection{collection}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(t.Context(), Request{Operation: operationkind.Create, Collection: "posts", ImportID: "broken", Data: store.Values{"title": store.String("Broken")}}); err != nil {
		t.Fatal(err)
	}

	_, err = engine.Execute(t.Context(), Request{Operation: operationkind.Read, Collection: "posts", Page: 1, Limit: 10, IncludeAccess: true})
	var operationError *Error
	if !errors.As(err, &operationError) || operationError.Code != "access_failed" {
		t.Fatalf("enriched list error = %#v", err)
	}

	ordinary, err := engine.Execute(t.Context(), Request{Operation: operationkind.Read, Collection: "posts", Page: 1, Limit: 10})
	if err != nil || ordinary.Page == nil || ordinary.PageAccess != nil {
		t.Fatalf("ordinary list = %#v, %v", ordinary, err)
	}
}

func TestCollectionListAccessRespectsLocaleAndTrashContext(t *testing.T) {
	collection := Collection{
		Schema: schema.Collection{
			ID: "collection-posts", Slug: "posts",
			Capabilities: schema.Capabilities{Trash: true},
		},
		Access: map[operationkind.Kind]Access{
			operationkind.Delete: func(ctx Context) (Decision, error) {
				if ctx.Operation != operationkind.RestoreDeleted || ctx.Locale == "fr" {
					return Decision{Kind: Allow}, nil
				}
				return Decision{Kind: Deny}, nil
			},
		},
	}
	engine, err := New(Config{
		Store: teststore.New(), Collections: []Collection{collection},
		Localization: &schema.LocalizationSettings{
			DefaultLocale: "en", Locales: []schema.Locale{{Code: "en"}, {Code: "fr"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(t.Context(), Request{Operation: operationkind.Create, Collection: "posts", ImportID: "deleted"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(t.Context(), Request{Operation: operationkind.Delete, Collection: "posts", ID: "deleted"}); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		locale string
		want   bool
	}{
		{locale: "en", want: false},
		{locale: "fr", want: true},
	} {
		result, err := engine.Execute(t.Context(), Request{
			Operation: operationkind.Read, Collection: "posts", Page: 1, Limit: 10,
			TrashOnly: true, IncludeAccess: true, Locale: test.locale,
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.PageAccess == nil || result.PageAccess.Documents["deleted"].Operations.RestoreDeleted != test.want {
			t.Fatalf("%s trash capabilities = %#v, want restore %t", test.locale, result.PageAccess, test.want)
		}
	}
}
