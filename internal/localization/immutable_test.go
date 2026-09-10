package localization

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestProjectionReusesUnaffectedBranchesAndPreservesLocaleSemantics(t *testing.T) {
	plain := schema.Field{Name: "plain", Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: []schema.Field{{Name: "title", Type: schema.FieldTypeText}}}}
	group := schema.Field{Name: "group", Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{
		{Name: "title", Localized: true}, {Name: "nullable", Localized: true}, {Name: "missing", Localized: true}, plain,
	}}}
	rows := store.List(store.Object(store.Values{"_key": store.String("one"), "title": store.String("shared"), "unknown": store.Null()}), store.String("invalid row"))
	canonical := store.Object(store.Values{
		"title":    store.Object(store.Values{"fr": store.String(""), "en": store.String("fallback")}),
		"nullable": store.Object(store.Values{"fr": store.Null(), "en": store.String("fallback null")}),
		"missing":  store.Object(store.Values{"de": store.String("absent locale")}),
		"plain":    rows, "unknown": store.String("nested"),
	})
	document := store.Document{Values: store.Values{"group": canonical, "plain": rows, "unknown": store.String("root")}, LocalizationSources: map[string]schema.LocaleCode{"stale": "de"}}
	selection := Selection{Locale: "fr", Chain: []schema.LocaleCode{"fr", "en"}, Configured: []schema.LocaleCode{"en", "fr"}}
	for _, preserveNull := range []bool{false, true} {
		selection.PreserveNull = preserveNull
		projected := ProjectDocument(document, []schema.Field{group, plain}, selection)
		got := projected.Values["group"]
		if text, _ := got.Get("title").StringValue(); text != "fallback" {
			t.Fatalf("fallback title = %q", text)
		}
		if _, exists := got.Lookup("missing"); exists {
			t.Fatal("missing locale was retained")
		}
		wantNullable := store.String("fallback null")
		wantLocale := schema.LocaleCode("en")
		if preserveNull {
			wantNullable, wantLocale = store.Null(), "fr"
		}
		if !reflect.DeepEqual(got.Get("nullable"), wantNullable) || !reflect.DeepEqual(projected.LocalizationSources, map[string]schema.LocaleCode{"group.title": "en", "group.nullable": wantLocale}) {
			t.Fatalf("null projection/sources = %#v / %#v", got.Get("nullable"), projected.LocalizationSources)
		}
		if !reflect.DeepEqual(got.Get("plain"), rows) || !reflect.DeepEqual(got.Get("unknown"), store.String("nested")) || !reflect.DeepEqual(projected.Values["unknown"], store.String("root")) {
			t.Fatal("projection changed an unaffected or unknown value")
		}
		projected.Values["unknown"] = store.String("detached")
		projected.LocalizationSources["group.title"] = "de"
	}
	if !reflect.DeepEqual(document.Values["group"], canonical) || !reflect.DeepEqual(document.LocalizationSources, map[string]schema.LocaleCode{"stale": "de"}) || !reflect.DeepEqual(document.Values["unknown"], store.String("root")) {
		t.Fatal("projection mutated its input snapshot")
	}
	_, visible, changed := projectValueAt(plain, rows, selection, "plain", map[string]schema.LocaleCode{})
	if !visible || changed {
		t.Fatal("ordinary branch unnecessarily transformed")
	}
	all := ProjectDocument(document, []schema.Field{group, plain}, Selection{All: true})
	if !reflect.DeepEqual(all.Values, document.Values) || all.LocalizationSources != nil {
		t.Fatal("all-locales projection changed canonical values or retained stale source metadata")
	}
}

func TestSparseNoOpMergeReusesCurrentGroupsAndLocales(t *testing.T) {
	content := schema.Field{Name: "content", Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{{Name: "title"}, {Name: "translation", Localized: true}}}}
	fields := []schema.Field{content}
	current := store.Object(store.Values{
		"_key": store.String("one"), "blockType": store.String("card"),
		"content": store.Object(store.Values{"title": store.String("shared"), "translation": store.Object(store.Values{"en": store.String("Hello"), "fr": store.String("Bonjour")})}),
	})
	for _, patch := range []store.Value{
		store.Object(store.Values{"_key": store.String("one"), "blockType": store.String("card"), "content": store.Object(store.Values{})}),
		store.Object(store.Values{"_key": store.String("one"), "content": store.Object(store.Values{"translation": store.Object(store.Values{"fr": store.String("Bonjour")})})}),
	} {
		if unchanged, complete := unchangedObjectPatch(fields, current, patch); !unchanged || complete {
			t.Fatal("identity-only row or unchanged sparse group/locale was not recognized")
		}
		got, expanded := mergeObjectValue(fields, current, patch)
		if !expanded || !reflect.DeepEqual(got, current) {
			t.Fatal("sparse no-op merge lost omitted current data")
		}
	}
	if unchanged, complete := unchangedObjectPatch(fields, current, current); !unchanged || !complete {
		t.Fatal("complete no-op patch was unnecessarily expanded")
	}
	if _, expanded := mergeObjectValue(fields, current, current); expanded {
		t.Fatal("complete no-op patch was marked changed")
	}
	localizedGroup := content
	localizedGroup.Localized = true
	canonical := store.Object(store.Values{"en": current.Get("content"), "fr": current.Get("content")})
	patch := store.Object(store.Values{"en": store.Object(store.Values{})})
	if got, expanded := mergeValue(localizedGroup, canonical, patch); !expanded || !reflect.DeepEqual(got, canonical) {
		t.Fatal("empty group patch inside a locale lost existing locale contents")
	}
}

func TestSparseNoOpMergeComparesScalarBitsAndMemberPresence(t *testing.T) {
	for _, test := range []struct {
		name    string
		current store.Value
		patch   store.Value
		same    bool
	}{
		{"string", store.String("same"), store.String("same"), true},
		{"changed string", store.String("old"), store.String("new"), false},
		{"number", store.Number(4), store.Number(4), true},
		{"signed zero", store.Number(0), store.Number(math.Copysign(0, -1)), false},
		{"boolean", store.Boolean(true), store.Boolean(true), true},
		{"changed boolean", store.Boolean(true), store.Boolean(false), false},
		{"null", store.Null(), store.Null(), true},
		{"different kind", store.Number(1), store.String("1"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := store.Object(store.Values{"value": test.current, "omitted": store.String("keep")})
			patch := store.Object(store.Values{"value": test.patch})
			if same, _ := unchangedObjectPatch(nil, current, patch); same != test.same {
				t.Fatalf("unchanged = %v, want %v", same, test.same)
			}
			got, _ := mergeObjectValue(nil, current, patch)
			if !reflect.DeepEqual(got.Get("value"), test.patch) {
				t.Fatal("merge lost submitted scalar")
			}
			if test.name == "signed zero" {
				number, _ := got.Get("value").NumberValue()
				if !math.Signbit(number) {
					t.Fatal("merge discarded the submitted sign of zero")
				}
			}
		})
	}
	current := store.Object(store.Values{"existing": store.Null()})
	patch := store.Object(store.Values{"missing": store.Null()})
	if same, _ := unchangedObjectPatch(nil, current, patch); same {
		t.Fatal("missing member was treated as explicit null")
	}
	got, _ := mergeObjectValue(nil, current, patch)
	if value, exists := got.Lookup("missing"); !exists || value.Kind() != store.ValueNull {
		t.Fatal("explicit null member was omitted")
	}
	localized := schema.Field{Localized: true}
	if same, _ := unchangedValuePatch(localized, current, patch); same {
		t.Fatal("missing locale was treated as explicit null")
	}
}

func TestSparseNoOpMergeKeepsOpaqueAndEmbeddedPathsConservative(t *testing.T) {
	for _, test := range []struct {
		name  string
		field schema.Field
		value store.Value
	}{
		{"opaque object", schema.Field{}, store.Object(store.Values{})},
		{"group without schema", schema.Field{Type: schema.FieldTypeGroup}, store.Object(store.Values{})},
		{"list", schema.Field{Type: schema.FieldTypeArray}, store.List()},
		{"populated document", schema.Field{}, store.Populated(store.Document{})},
		{"embedded envelope", immutableEmbeddedField(false), store.Object(store.Values{"outline": store.List(store.String("invalid node"))})},
	} {
		t.Run(test.name, func(t *testing.T) {
			if same, _ := unchangedValuePatch(test.field, test.value, test.value); same {
				t.Fatal("opaque or embedded value bypassed its normal merge/admission path")
			}
		})
	}
	current := store.Object(store.Values{"json": store.Object(store.Values{"old": store.String("remove")})})
	patch := store.Object(store.Values{"json": store.Object(store.Values{})})
	got, _ := mergeObjectValue(nil, current, patch)
	if got.Get("json").Len() != 0 || current.Get("json").Len() != 1 {
		t.Fatal("opaque JSON replacement was treated as a group merge or mutated current data")
	}
}

func TestWideSparseObjectMergeResolvesEveryCompoundMember(t *testing.T) {
	fields := make([]schema.Field, 0, 514)
	currentValues, patchValues := store.Values{}, store.Values{}
	for i := 0; i < 512; i++ {
		name := fmt.Sprintf("value_%d", i)
		fields = append(fields, schema.Field{Name: name})
		currentValues[name], patchValues[name] = store.Number(float64(i)), store.Number(float64(i))
	}
	fields = append(fields,
		schema.Field{Name: "group", Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{{Name: "title"}}}},
		schema.Field{Name: "translation", Localized: true},
	)
	currentValues["group"] = store.Object(store.Values{"title": store.String("keep")})
	currentValues["translation"] = store.Object(store.Values{"en": store.String("Hello"), "fr": store.String("Bonjour")})
	currentValues["opaque"] = store.Object(store.Values{"old": store.String("replace")})
	patchValues["group"] = store.Object(store.Values{})
	patchValues["translation"] = store.Object(store.Values{"en": store.String("Hello")})
	current, patch := store.Object(currentValues), store.Object(patchValues)
	if same, complete := unchangedObjectPatch(fields, current, patch); !same || complete {
		t.Fatal("wide scalar patch did not resolve its sparse group and locale members")
	}
	if got, _ := mergeObjectValue(fields, current, patch); !reflect.DeepEqual(got, current) {
		t.Fatal("wide sparse merge lost omitted values")
	}
	patchValues["opaque"] = store.Object(store.Values{})
	patch = store.Object(patchValues)
	if same, _ := unchangedObjectPatch(fields, current, patch); same {
		t.Fatal("resolved known objects hid an unresolved opaque object")
	}
	got, _ := mergeObjectValue(fields, current, patch)
	if got.Get("opaque").Len() != 0 || !reflect.DeepEqual(got.Get("group"), current.Get("group")) || !reflect.DeepEqual(got.Get("translation"), current.Get("translation")) {
		t.Fatal("wide merge lost opaque replacement or known compound merge semantics")
	}
}

func TestStorageProjectionKeepsFilteringAndAllLocaleValidation(t *testing.T) {
	field := schema.Field{Name: "rows", Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: []schema.Field{{Name: "title", Localized: true}}}}
	input := store.Values{"unknown": store.String("root"), "rows": store.List(store.Object(store.Values{"_key": store.String("one"), "title": store.Null(), "unknown": store.String("nested")}))}
	selected, err := StoragePatch([]schema.Field{field}, input, Selection{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := selected["unknown"]; exists {
		t.Fatal("storage patch retained unknown root field")
	}
	row, _ := selected["rows"].ListItem(0)
	if locale, exists := row.Get("title").Lookup("en"); !exists || locale.Kind() != store.ValueNull || !reflect.DeepEqual(row.Get("unknown"), store.String("nested")) {
		t.Fatal("selected-locale wrapping lost explicit null or unknown nested value")
	}
	all := Selection{All: true, Configured: []schema.LocaleCode{"en"}}
	_, changed, err := storageValue(field, selected["rows"], all)
	if err != nil || changed {
		t.Fatalf("valid all-locales branch changed=%v error=%v", changed, err)
	}
	bad := store.List(store.Object(store.Values{"title": store.Object(store.Values{"de": store.String("unknown")})}))
	if _, _, err := storageValue(field, bad, all); err == nil {
		t.Fatal("all-locales validation skipped unknown locale")
	}
	original, _ := input["rows"].ListItem(0)
	if original.Get("title").Kind() != store.ValueNull {
		t.Fatal("storage projection mutated authored input")
	}
}

func immutableEmbeddedField(localized bool) schema.Field {
	return schema.Field{Name: "body", Type: schema.FieldTypePlugin, Plugin: &schema.PluginField{EmbeddedTrees: []schema.EmbeddedTree{{Version: 1, Key: "cards", Root: []string{"outline"}, Children: "items", Tag: "kind", Cases: []schema.EmbeddedTreeCase{{TagValue: "widget", Payload: "content", Discriminator: "schema", Identity: "uid", Types: []schema.BlockType{{Slug: "card", Fields: []schema.Field{{Name: "title", Localized: localized}}}}}}}}}}
}

func immutableEmbeddedNode(key string, values store.Values) store.Value {
	payload := store.CloneValues(values)
	payload["schema"], payload["uid"] = store.String("card"), store.String(key)
	return store.Object(store.Values{"kind": store.String("widget"), "content": store.Object(payload)})
}

func TestLocalizationStillAdmitsUnlocalizedEmbeddedBranches(t *testing.T) {
	field := immutableEmbeddedField(false)
	bad := store.Object(store.Values{"outline": store.List(store.String("invalid node"))})
	if _, _, err := storageValue(field, bad, Selection{Locale: "en"}); err == nil {
		t.Fatal("unlocalized embedded branch escaped write validation")
	}
	if _, visible, _ := projectValueAt(field, bad, Selection{Locale: "en"}, "body", map[string]schema.LocaleCode{}); visible {
		t.Fatal("invalid embedded branch escaped projection validation")
	}
	issues := CopyLocaleIssues([]schema.Field{field}, store.Values{"body": bad})
	if len(issues) != 1 || issues[0].Path != "body.outline.0" {
		t.Fatalf("embedded issues = %#v", issues)
	}
	valid := store.Object(store.Values{"outline": store.List(immutableEmbeddedNode("one", store.Values{"title": store.String("shared")}))})
	if _, changed, err := storageValue(field, valid, Selection{Locale: "en"}); err != nil || changed {
		t.Fatalf("valid unlocalized embedded branch changed=%v error=%v", changed, err)
	}
}

func TestStorageMergeCompletesOnlyOmittedMembersByStableIdentity(t *testing.T) {
	fields := []schema.Field{{Name: "title", Localized: true}, {Name: "summary", Type: schema.FieldTypeText}}
	array := schema.Field{Name: "rows", Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: fields}}
	current := store.List(
		store.Object(store.Values{"_key": store.String("one"), "title": store.Object(store.Values{"en": store.String("first"), "fr": store.String("premier")}), "summary": store.String("keep first"), "unknown": store.String("old")}),
		store.Object(store.Values{"_key": store.String("two"), "title": store.Object(store.Values{"en": store.String("second")}), "summary": store.String("keep second")}),
	)
	patch := store.List(
		store.Object(store.Values{"_key": store.String("two"), "title": store.Object(store.Values{"en": store.String("new second")}), "summary": store.Null()}),
		store.Object(store.Values{"_key": store.String("one"), "title": store.Object(store.Values{"en": store.String("new first")})}),
	)
	merged, changed := mergeValue(array, current, patch)
	first, _ := merged.ListItem(0)
	second, _ := merged.ListItem(1)
	if !changed || !reflect.DeepEqual(first.Get("summary"), store.Null()) || !reflect.DeepEqual(second.Get("summary"), store.String("keep first")) || !reflect.DeepEqual(second.Get("title").Get("fr"), store.String("premier")) || !reflect.DeepEqual(second.Get("unknown"), store.String("old")) {
		t.Fatal("reordered row merge lost identity, omitted members, another locale, or explicit null")
	}
	original, _ := patch.ListItem(1)
	if _, exists := original.Lookup("summary"); exists {
		t.Fatal("merge mutated retained patch")
	}
	removed, _ := mergeValue(array, current, store.List(first))
	if removed.Len() != 1 {
		t.Fatal("merge restored an omitted row")
	}
	complete := store.Object(store.Values{"title": store.Object(store.Values{"en": store.String("new"), "fr": store.String("nouveau")}), "summary": store.Null()})
	old := store.Object(store.Values{"title": store.Object(store.Values{"en": store.String("old"), "fr": store.String("ancien")}), "summary": store.String("old")})
	if got, changed := mergeObjectValue(fields, old, complete); changed || !reflect.DeepEqual(got, complete) {
		t.Fatal("complete patch rebuilt despite requiring no omitted members")
	}

	embeddedField := immutableEmbeddedField(true)
	before := store.Object(store.Values{"outline": store.List(
		immutableEmbeddedNode("one", store.Values{"title": store.Object(store.Values{"en": store.String("one"), "fr": store.String("un")}), "summary": store.String("keep one")}),
		immutableEmbeddedNode("two", store.Values{"title": store.Object(store.Values{"en": store.String("two")}), "summary": store.String("keep two")}),
	)})
	after := store.Object(store.Values{"outline": store.List(
		immutableEmbeddedNode("two", store.Values{"title": store.Object(store.Values{"en": store.String("new two")})}),
		immutableEmbeddedNode("one", store.Values{"title": store.Object(store.Values{"en": store.String("new one")})}),
	)})
	result, changed := mergeValue(embeddedField, before, after)
	secondNode, _ := result.Get("outline").ListItem(1)
	if !changed || !reflect.DeepEqual(secondNode.Get("content").Get("summary"), store.String("keep one")) || !reflect.DeepEqual(secondNode.Get("content").Get("title").Get("fr"), store.String("un")) {
		t.Fatal("embedded merge matched position rather than stable occurrence identity")
	}
	retained, _ := after.Get("outline").ListItem(1)
	if _, exists := retained.Get("content").Lookup("summary"); exists {
		t.Fatal("embedded merge mutated retained patch")
	}
}
