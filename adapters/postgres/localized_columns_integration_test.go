package postgres

import (
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Localized values are read as native per-locale columns and assembled in Go.
// A locale with a stored value is present, including empty strings, zero and
// false; a NULL locale is absent; a field with no stored locale is an empty
// object. Nested JSON values are returned exactly as stored.
func TestPostgresLocalizedColumnsAssembleExactLocaleWrappers(t *testing.T) {
	ctx := t.Context()
	backend, collection := publishedReadFixture(t, field.Fields{
		field.Text("title").Localized(),
		field.Number("rank").Localized(),
		field.Checkbox("featured").Localized(),
		field.Group("details", field.Fields{field.Text("caption"), field.Text("note")}).Localized(),
		field.Text("subtitle").Localized(),
	}, ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}, {Code: "ar", Label: "Arabic"}}})
	locales := []schema.LocaleCode{"en", "fr", "ar"}
	statement := selectColumns(collection, storedSchemaFields(collection), locales)
	if strings.Contains(statement, "jsonb_build_object") || strings.Contains(statement, "jsonb_strip_nulls") {
		t.Fatalf("localized projection assembles JSON in SQL: %s", statement)
	}
	values := store.Values{
		"title":    store.Object(store.Values{"en": store.String("Hello"), "fr": store.String("")}),
		"rank":     store.Object(store.Values{"en": store.Number(0), "ar": store.Number(2.5)}),
		"featured": store.Object(store.Values{"fr": store.Boolean(false)}),
		"details": store.Object(store.Values{
			"en": store.Object(store.Values{"caption": store.String("Kept"), "note": store.Null()}),
		}),
		"subtitle": store.Object(store.Values{}),
	}
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer write.Rollback(ctx)
	created, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: "localized", Status: store.StatusPublished, Locales: locales, Values: values})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(created.Values, values) {
		t.Fatalf("created localized values = %#v, want %#v", created.Values, values)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	read, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback(ctx)
	for _, publishedOnly := range []bool{false, true} {
		document, err := read.Find(ctx, store.Request{Collection: collection, ID: "localized", PublishedOnly: publishedOnly, Locales: locales})
		if err != nil || !reflect.DeepEqual(document.Values, values) {
			t.Fatalf("publishedOnly=%t localized values = %#v, %v; want %#v", publishedOnly, document.Values, err, values)
		}
		page, err := read.List(ctx, store.Request{Collection: collection, PublishedOnly: publishedOnly, Locales: locales, Page: 1, Limit: 10})
		if err != nil || len(page.Documents) != 1 || !reflect.DeepEqual(page.Documents[0].Values, values) {
			t.Fatalf("publishedOnly=%t localized list = %#v, %v", publishedOnly, page.Documents, err)
		}
	}
	// A read of fewer configured locales selects only their columns.
	english, err := read.Find(ctx, store.Request{Collection: collection, ID: "localized", Locales: []schema.LocaleCode{"en"}})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := english.Values["title"].Get("en").StringValue(); title != "Hello" || english.Values["title"].Len() != 1 || english.Values["featured"].Len() != 0 {
		t.Fatalf("single-locale read = %#v", english.Values)
	}
}
