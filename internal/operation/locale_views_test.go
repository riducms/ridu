package operation

import (
	"context"
	"testing"

	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestLocaleProjectionRefreshesTransactionResultWithoutChangingHookSnapshot(t *testing.T) {
	var retained store.Value
	engine, err := New(Config{
		Store:        teststore.New(),
		Localization: &schema.LocalizationSettings{DefaultLocale: "en", Locales: []schema.Locale{{Code: "en"}, {Code: "fr"}}},
		Collections: []Collection{{
			Schema: schema.Collection{ID: "pages", Slug: "pages", Capabilities: schema.Capabilities{Versions: true}, Versions: &schema.VersionSettings{MaxPerDocument: 10}, Fields: []schema.Field{{Name: "title", Path: mustPopulationPath(t, "title"), Type: schema.FieldTypeText, Localized: true}}},
			Hooks: Hooks{AfterChange: []Hook{func(ctx Context) error {
				retained = ctx.Document.Values["title"]
				return nil
			}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := engine.Execute(t.Context(), Request{Operation: operation.Create, Collection: "pages", Locale: "en", Data: store.Values{"title": store.String("English")}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Execute(t.Context(), Request{
		Operation: operation.Publish, Collection: "pages", ID: created.Document.ID, AllLocales: true,
		TransactionMutation: func(_ context.Context, _ store.Transaction, _ schema.Collection, document store.Document) error {
			// This mutable result map is shared with Execute. The hook snapshot was
			// already taken; response projection must inspect the replacement value.
			document.Values["title"] = store.Object(store.Values{"en": store.String("Changed"), "fr": store.Null()})
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := retained.StringValue(); got != "English" {
		t.Fatalf("hook snapshot = %q", got)
	}
	if got, _ := result.Document.Values["title"].Get("en").StringValue(); got != "Changed" {
		t.Fatalf("response title = %q", got)
	}
	if got := result.Document.Values["title"].Get("fr").Kind(); got != store.ValueNull {
		t.Fatalf("response French kind = %s", got)
	}
	persisted, err := engine.Execute(t.Context(), Request{Operation: operation.Read, Collection: "pages", ID: result.Document.ID, AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := persisted.Document.Values["title"].Get("en").StringValue(); got != "English" {
		t.Fatalf("response mutation changed persisted title = %q", got)
	}
}
