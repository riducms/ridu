package primitivelists

import (
	"encoding/json"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/store"
	richtextblocks "github.com/riducms/ridu/tests/contracts/richtext_blocks"
)

// Values is one product containing every supported placement of primitive lists.
func Values() store.Values {
	points := store.List(store.String(""), store.String("Oak"), store.String("Oak"))
	sizes := store.List(store.Number(0), store.Number(8.5), store.Number(8.5))
	children := store.Values{"points": points, "sizes": sizes}
	row := store.CloneValues(children)
	row["_key"] = store.String("variant-A")
	block := store.CloneValues(children)
	block["_key"] = store.String("block-A")
	block["blockType"] = store.String("card")
	document := func(key string) store.Value {
		value, _ := richtextblocks.Document(richtextblocks.Block("card", key, children)).CopyObject()
		root, _ := value["root"].CopyObject()
		// Omit unrelated optional-null editor metadata: PostgreSQL's existing
		// localized projection removes null object properties. Lists still
		// compare exactly, including order, duplicates, empty strings and zero.
		delete(root, "direction")
		value["root"] = store.Object(root)
		return store.Object(value)
	}
	return store.Values{"title": store.String("Product"), "sellingPoints": points, "availableSizes": sizes, "details": store.Object(children), "variants": store.List(store.Object(row)), "content": store.List(store.Object(block)), "body": document("embed-A"), "localizedPoints": points, "localizedSizes": sizes, "localizedBody": document("embed-fr")}
}

// Exercise verifies the ordinary operation engine against a prepared adapter.
func Exercise(t *testing.T, app *core.App) {
	t.Helper()
	values := Values()
	created, err := app.Local().CreateWithOptions(t.Context(), "primitive-products", values, core.MutationOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	found, err := app.Local().FindWithOptions(t.Context(), "primitive-products", created.ID, core.FindOptions{Locale: "fr", DisableFallback: true})
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range values {
		equalValue(t, found.Values[key], value)
	}
	updated, err := app.Local().PublishChangesWithOptions(t.Context(), "primitive-products", created.ID, store.Values{"sellingPoints": store.List(store.String("Replacement")), "availableSizes": store.List()}, core.MutationOptions{Locale: "fr", ExpectedRevision: created.Revision})
	if err != nil {
		t.Fatal(err)
	}
	equalValue(t, updated.Values["availableSizes"], store.List())
	copied, err := app.Local().DuplicateWithOptions(t.Context(), "primitive-products", created.ID, nil, core.MutationOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	equalValue(t, copied.Values["sellingPoints"], updated.Values["sellingPoints"])
	equalValue(t, copied.Values["availableSizes"], store.List())
	restored, err := app.Local().RestoreVersionWithOptions(t.Context(), "primitive-products", created.ID, created.Revision, false, core.MutationOptions{Locale: "fr", ExpectedRevision: updated.Revision})
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range values {
		equalValue(t, restored.Values[key], value)
	}
}

func equalValue(t *testing.T, actual, want store.Value) {
	t.Helper()
	a, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("stored value %s, want %s", a, b)
	}
}
