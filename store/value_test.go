package store_test

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestValueConstructorsAndAccessorsIsolateNestedContainers(t *testing.T) {
	child := store.Values{"title": store.String("original")}
	rows := []store.Value{store.Object(child)}
	outer := store.Values{"rows": store.List(rows...)}
	value := store.Object(outer)
	want := encodedValue(t, value)
	child["title"] = store.String("constructor child mutation")
	rows[0] = store.Null()
	outer["rows"] = store.Null()
	if encodedValue(t, value) != want {
		t.Fatal("constructor retained mutable caller container")
	}

	object, _ := value.CopyObject()
	items, _ := object["rows"].CopyList()
	nested, _ := items[0].CopyObject()
	nested["title"] = store.String("accessor mutation")
	items[0] = store.Object(nested)
	object["rows"] = store.List(items...)
	if encodedValue(t, value) != want {
		t.Fatal("accessor mutation reached original value")
	}
	updated := store.Object(object)
	if encodedValue(t, updated) == want {
		t.Fatal("explicit replacement did not apply edit")
	}

	// Unmarshal replaces a copied Value, never a private shared subtree.
	copy := value
	if err := json.Unmarshal([]byte(`{"rows":[{"title":"decoded"}]}`), &copy); err != nil {
		t.Fatal(err)
	}
	if encodedValue(t, value) != want || encodedValue(t, copy) == want {
		t.Fatal("JSON decoding mutated a retained value")
	}
	local := object["rows"]
	if err := json.Unmarshal([]byte(`[]`), &local); err != nil {
		t.Fatal(err)
	}
	if encodedValue(t, updated) == `{"rows":[]}` {
		t.Fatal("decoding local child changed enclosing value")
	}
}

func TestClonedAndPopulatedDocumentsKeepDetachedSnapshots(t *testing.T) {
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	document := store.Document{ID: "source", DeletedAt: &now, Values: store.Values{
		"nested": store.Object(store.Values{"title": store.String("snapshot")}),
	}, LocalizationSources: map[string]schema.LocaleCode{"nested.title": "en"}}
	populated := store.Populated(document)
	want := encodedValue(t, populated)
	snapshot := store.CloneDocument(document)
	*document.DeletedAt = now.Add(time.Hour)
	document.LocalizationSources["nested.title"] = "fr"
	document.Values["nested"] = store.Null()
	if encodedValue(t, populated) != want {
		t.Fatal("Populated retained mutable document metadata or values")
	}
	if snapshot.DeletedAt.Equal(*document.DeletedAt) || snapshot.LocalizationSources["nested.title"] != "en" {
		t.Fatal("CloneDocument retained mutable metadata")
	}

	extracted, ok := populated.CopyDocument()
	if !ok {
		t.Fatal("not a populated document")
	}
	fields, _ := extracted.Values["nested"].CopyObject()
	fields["title"] = store.String("edited")
	extracted.Values["nested"] = store.Object(fields)
	extracted.LocalizationSources["nested.title"] = "de"
	*extracted.DeletedAt = extracted.DeletedAt.Add(time.Hour)
	if encodedValue(t, populated) != want {
		t.Fatal("DocumentValue exposed mutable backing document")
	}
	values := store.CloneValues(snapshot.Values)
	values["nested"] = store.Null()
	if snapshot.Values["nested"].Kind() != store.ValueObject {
		t.Fatal("cloned top-level values alias snapshot")
	}
}

func TestSharedImmutableValuesSupportConcurrentDetachedEdits(t *testing.T) {
	value := store.Object(store.Values{"rows": store.List(store.Object(store.Values{"title": store.String("original")}))})
	want := encodedValue(t, value)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			for range 100 {
				object, _ := value.CopyObject()
				rows, _ := object["rows"].CopyList()
				row, _ := rows[0].CopyObject()
				row["title"] = store.String("local")
				rows[0] = store.Object(row)
				object["rows"] = store.List(rows...)
				encoded, err := json.Marshal(value)
				if err != nil || string(encoded) != want {
					t.Errorf("parallel detached edit changed snapshot: %s %v", encoded, err)
					return
				}
			}
		})
	}
	workers.Wait()
}

func encodedValue(t *testing.T, value store.Value) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
