package localization

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func projectorFields() []schema.Field {
	return []schema.Field{
		{Name: "body", Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{{Name: "title", Localized: true}, {Name: "nullable", Localized: true}, {Name: "missing", Localized: true}}}},
		{Name: "headline"},
	}
}

func projectorBody(title string) store.Value {
	return store.Object(store.Values{
		"title":    store.Object(store.Values{"en": store.String(title), "fr": store.String("Français"), "empty": store.String("")}),
		"nullable": store.Object(store.Values{"en": store.String("fallback"), "fr": store.Null()}),
		"missing":  store.Object(store.Values{"other": store.String("unselected")}),
		"unknown":  store.String("keep"),
	})
}

func TestProjectorMatchesSelectionSemantics(t *testing.T) {
	fields := projectorFields()
	projector := NewProjector(fields)
	defer projector.Clear()
	input := store.Document{Values: store.Values{"body": projectorBody("English"), "unknown": store.String("root")}, LocalizationSources: map[string]schema.LocaleCode{"stale": "other"}}
	for _, selection := range []Selection{
		{},
		{Locale: "en", Chain: []schema.LocaleCode{"en", "fr"}},
		{Locale: "fr", Chain: []schema.LocaleCode{"fr", "en"}},
		{Locale: "fr", Chain: []schema.LocaleCode{"fr", "en"}, PreserveNull: true},
		{Locale: "fr", Chain: []schema.LocaleCode{"en", "fr"}},
		{Locale: "empty", Chain: []schema.LocaleCode{"empty", "en"}},
		{Locale: "empty", Chain: []schema.LocaleCode{"empty"}},
		{Locale: "missing", Chain: []schema.LocaleCode{"missing"}},
		{All: true, Configured: []schema.LocaleCode{"en", "fr"}},
	} {
		for range 2 {
			want := ProjectDocument(input, fields, selection)
			got := projector.Document(input, selection)
			if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(projector.Values(input.Values, selection), want.Values) {
				t.Fatalf("projection differs for %#v: got %#v, want %#v", selection, got, want)
			}
		}
	}
	selection := Selection{Locale: "en", Chain: []schema.LocaleCode{"en"}}
	projector.Values(input.Values, selection)
	if got := projector.Values(store.Values{}, selection); len(got) != 0 {
		t.Fatal("cached field was resurrected after removal")
	}
	withNull := store.Values{"body": store.Null()}
	if got := projector.Values(withNull, selection); got["body"].Kind() != store.ValueNull {
		t.Fatal("explicit null reused an earlier object projection")
	}
}

func TestProjectorReusesRootsAndDetachesCurrentDocumentMetadata(t *testing.T) {
	projector := NewProjector(projectorFields())
	defer projector.Clear()
	selection := Selection{Locale: "en", Chain: []schema.LocaleCode{"en"}}
	deletedAt := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	input := store.Document{ID: "first", Revision: 1, DeletedAt: &deletedAt, Values: store.Values{"body": projectorBody("English"), "headline": store.String("First")}}
	first := projector.Document(input, selection)
	retained := first.Values["body"]
	first.Values["body"] = store.Null()
	first.LocalizationSources["body.title"] = "wrong"
	*first.DeletedAt = first.DeletedAt.Add(time.Hour)
	input.ID, input.Revision, input.UpdatedAt = "current", 2, deletedAt.Add(2*time.Hour)
	input.Values["headline"] = store.String("Changed")
	second := projector.Document(input, selection)
	if !second.Values["body"].SameBacking(retained) {
		t.Fatal("unchanged body was reprojected after a root scalar change")
	}
	if second.ID != input.ID || second.Revision != input.Revision || !second.UpdatedAt.Equal(input.UpdatedAt) || !second.DeletedAt.Equal(deletedAt) || second.LocalizationSources["body.title"] != "en" {
		t.Fatal("cache retained stale or caller-mutated document metadata")
	}
	values := projector.Values(input.Values, selection)
	values["body"] = store.Null()
	if !projector.Values(input.Values, selection)["body"].SameBacking(retained) {
		t.Fatal("values-only result exposed its cached root map")
	}
	input.Values["body"] = projectorBody("Updated")
	third := projector.Document(input, selection)
	if third.Values["body"].SameBacking(retained) || third.Values["body"].Get("title").SameBacking(retained.Get("title")) {
		t.Fatal("changed source reused stale projection")
	}
	if text, _ := retained.Get("title").StringValue(); text != "English" {
		t.Fatal("a later projection mutated a retained snapshot")
	}
}

func TestProjectorOwnsSelectionKeysAndEvictsOldVariants(t *testing.T) {
	projector := NewProjector(projectorFields())
	selection := Selection{Locale: "en", Chain: []schema.LocaleCode{"en"}, Configured: []schema.LocaleCode{"en", "fr"}}
	body := projectorBody("English")
	first := projector.Values(store.Values{"body": body}, selection)["body"]
	selection.Chain[0], selection.Configured[0] = "fr", "changed"
	if got, _ := projector.Values(store.Values{"body": body}, selection)["body"].Get("title").StringValue(); got != "Français" {
		t.Fatal("mutated caller selection corrupted a cached key")
	}
	original := Selection{Locale: "en", Chain: []schema.LocaleCode{"en"}, Configured: []schema.LocaleCode{"en", "fr"}}
	if !projector.Values(store.Values{"body": body}, original)["body"].SameBacking(first) {
		t.Fatal("original selection no longer hit its owned cache key")
	}
	projector.Clear()
	var variants [projectionEntriesPerField + 1]Selection
	for i := range variants {
		variants[i] = Selection{Locale: schema.LocaleCode(fmt.Sprintf("locale-%d", i)), Chain: []schema.LocaleCode{"en"}}
	}
	for _, variant := range variants[:projectionEntriesPerField] {
		projector.Values(store.Values{"body": body}, variant)
	}
	projector.Values(store.Values{"body": body}, variants[0]) // Retain the recently used oldest variant.
	projector.Values(store.Values{"body": body}, variants[projectionEntriesPerField])
	for _, entry := range projector.roots[0].entries {
		if !entry.valid || !entry.source.SameBacking(body) || sameProjectionSelection(entry.selection, variants[1]) {
			t.Fatal("cache did not evict the least recently used selection within its bound")
		}
	}
	updated := projectorBody("Updated")
	projector.Values(store.Values{"body": updated}, original)
	valid := 0
	for _, entry := range projector.roots[0].entries {
		if entry.valid {
			valid++
			if !entry.source.SameBacking(updated) {
				t.Fatal("cache retained an earlier source generation")
			}
		}
	}
	if valid != 1 {
		t.Fatal("changed source did not replace all cached locale variants")
	}
	if text, _ := first.Get("title").StringValue(); text != "English" {
		t.Fatal("source eviction mutated an externally retained projection")
	}
	partial := projector.Values(store.Values{}, original)
	if _, exists := partial["body"]; exists {
		t.Fatal("partial projection resurrected a missing root")
	}
	if !projector.roots[0].entries[0].source.SameBacking(updated) {
		t.Fatal("partial projection discarded an unchanged cached root")
	}
	projector.Clear()
	for _, root := range projector.roots {
		for _, entry := range root.entries {
			if entry.valid || entry.source.Kind() != "" || entry.value.Kind() != "" || entry.sources != nil || entry.selection.Chain != nil || entry.selection.Configured != nil {
				t.Fatal("Clear retained cached values, metadata or selection slices")
			}
		}
	}
}

func TestProjectorSeparatesSchemasAndRetainsCheckedAdmission(t *testing.T) {
	localized := NewProjector([]schema.Field{{Name: "title", Localized: true}})
	ordinary := NewProjector([]schema.Field{{Name: "title"}})
	selection := Selection{Locale: "en", Chain: []schema.LocaleCode{"en"}}
	input := store.Values{"title": store.Object(store.Values{"en": store.String("English")})}
	if localized.Values(input, selection)["title"].Kind() != store.ValueString || ordinary.Values(input, selection)["title"].Kind() != store.ValueObject {
		t.Fatal("schema ownership was shared across projector instances")
	}
	projector := NewProjector([]schema.Field{immutableEmbeddedField(false)})
	bad := store.Document{Values: store.Values{"body": store.Object(store.Values{"outline": store.List(store.String("invalid node"))})}}
	projector.Document(bad, selection) // Populate an unchecked invisible projection.
	for range 2 {
		if _, err := projector.DocumentChecked(bad, selection); err == nil {
			t.Fatal("cached unchecked projection bypassed checked document admission")
		}
		if _, err := projector.ValuesChecked(bad.Values, selection); err == nil {
			t.Fatal("cached unchecked projection bypassed checked values admission")
		}
	}
}

func TestProjectorAllocatesEntriesOnlyForActiveProjection(t *testing.T) {
	projector := NewProjector(projectorFields())
	input := store.Document{Values: store.Values{"body": projectorBody("English")}}
	projector.Values(input.Values, Selection{})
	projector.Document(input, Selection{})
	if projector.roots != nil {
		t.Fatal("disabled localization allocated projection entries")
	}
	projector.Document(input, Selection{All: true})
	if len(projector.roots) != len(projector.fields) {
		t.Fatal("active projection did not initialize its bounded entries")
	}
	projector.Clear()
	if projector.roots != nil {
		t.Fatal("Clear retained cache storage")
	}
	selection := Selection{Locale: "en", Chain: []schema.LocaleCode{"en"}}
	if got := projector.Document(input, selection); got.LocalizationSources["body.title"] != "en" {
		t.Fatal("cleared projector could not project using its retained schema")
	}
}

func TestIndependentProjectorsShareOnlyImmutableInputs(t *testing.T) {
	input := store.Document{Values: store.Values{"body": projectorBody("English")}}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			projector := NewProjector(projectorFields())
			defer projector.Clear()
			for i := range 20 {
				locale := schema.LocaleCode("en")
				want := "English"
				if i%2 != 0 {
					locale, want = "fr", "Français"
				}
				selection := Selection{Locale: locale, Chain: []schema.LocaleCode{locale}}
				got := projector.Document(input, selection)
				if title, _ := got.Values["body"].Get("title").StringValue(); title != want || got.LocalizationSources["body.title"] != locale {
					t.Error("independent projector reused another locale or operation's output")
					return
				}
				got.Values["body"] = store.Null()
				got.LocalizationSources["body.title"] = "modified"
			}
		})
	}
	workers.Wait()
}
