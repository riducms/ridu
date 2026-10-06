package core_test

import (
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

// A document saved while a field was optional keeps its stored null after the
// field becomes required. Reading and listing it must not fail; a read hook
// that replaces a stored value with null still breaks the output contract.
func TestStoredNullStaysReadableAfterFieldBecomesRequired(t *testing.T) {
	backend := teststore.New()
	optional, err := ridu.New(ridu.Config{Name: "Stored nulls", Collections: []ridu.Collection{{
		Slug: "posts", Fields: field.Fields{field.Text("title"), field.Text("summary"), field.Group("seo", field.Fields{field.Text("title")})},
	}}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	created, err := optional.Local().Create(t.Context(), "posts", store.Values{
		"title": store.Null(), "summary": store.String("Kept"), "seo": store.Object(store.Values{"title": store.Null()}),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	clearSummary := false
	required, err := ridu.New(ridu.Config{Name: "Stored nulls", Collections: []ridu.Collection{{
		Slug: "posts", Fields: field.Fields{
			field.Text("title").Required(),
			field.Text("summary").Required().ReplaceAfterRead(func(operation.Context, operation.Value[string]) (operation.Change[string], error) {
				if clearSummary {
					return operation.Clear[string](), nil
				}
				return operation.Keep[string](), nil
			}),
			field.Group("seo", field.Fields{field.Text("title").Required()}),
		},
	}}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	document, err := required.Local().Find(t.Context(), "posts", created.ID, ridu.FindOptions{})
	if err != nil || document.Values["title"].Kind() != store.ValueNull {
		t.Fatalf("stored null failed a read: %#v, %v", document.Values, err)
	}
	if page, err := required.Local().List(t.Context(), "posts", ridu.ListOptions{}); err != nil || len(page.Documents) != 1 {
		t.Fatalf("stored null failed a list: %#v, %v", page, err)
	}
	clearSummary = true
	if _, err := required.Local().Find(t.Context(), "posts", created.ID, ridu.FindOptions{}); !operationCode(err, "invalid_field_output") {
		t.Fatalf("a read hook returned null for a required field: %v", err)
	}
}
