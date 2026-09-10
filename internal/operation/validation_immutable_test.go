package operation

import (
	"encoding/json"
	"testing"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestImmutableValidationRetainsLocalizedRowMembershipAndSnapshots(t *testing.T) {
	for _, shape := range []string{"array", "blocks", "embedded"} {
		t.Run(shape, func(t *testing.T) {
			fallback := "Default translation"
			translation := schema.Field{Name: "translation", Type: schema.FieldTypeText, Localized: true, Default: &fallback}
			children := []schema.Field{translation}
			owner := schema.Field{Name: "items", Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: children}}
			row := func(key string, values store.Values) store.Value {
				values = store.CloneValues(values)
				values["_key"] = store.String(key)
				if shape == "blocks" {
					values["blockType"] = store.String("card")
				}
				if shape == "embedded" {
					delete(values, "_key")
					values["uid"], values["schema"] = store.String(key), store.String("card")
					return store.Object(store.Values{"kind": store.String("widget"), "content": store.Object(values)})
				}
				return store.Object(values)
			}
			list := func(rows ...store.Value) store.Value {
				if shape == "embedded" {
					return store.Object(store.Values{"items": store.List(rows...)})
				}
				return store.List(rows...)
			}
			if shape == "blocks" {
				owner.Type, owner.Nested = schema.FieldTypeBlocks, nil
				owner.Blocks = &schema.BlocksField{Types: []schema.BlockType{{Slug: "card", Fields: children}}}
			} else if shape == "embedded" {
				owner.Type, owner.Nested = schema.FieldTypePlugin, nil
				owner.Plugin = &schema.PluginField{Key: "fixture", EmbeddedTrees: []schema.EmbeddedTree{{Version: 1, Key: "widgets", Children: "items", Tag: "kind", Cases: []schema.EmbeddedTreeCase{{TagValue: "widget", Payload: "content", Discriminator: "schema", Identity: "uid", Types: []schema.BlockType{{Slug: "card", Fields: children}}}}}}}
			}
			fields := []schema.Field{owner}
			validators := map[string]PluginValidator{"fixture": func(schema.Field, store.Value, string) []schema.Issue { return nil }}
			previous := store.Values{"items": list(row("first", nil), row("second", nil))}
			input := store.Values{"items": list(row("second", store.Values{"translation": store.Null()}), row("new", nil), row("first", nil))}
			before, _ := json.Marshal(input)
			priorBefore, _ := json.Marshal(previous)
			options := validationOptions{requireMissing: true, previous: previous}
			validated, issues := validateWithOptions(fields, input, options, validators)
			if len(issues) != 0 {
				t.Fatalf("validation issues: %#v", issues)
			}
			rows := validated["items"]
			if shape == "embedded" {
				rows = rows.Get("items")
			}
			for index := 0; index < 3; index++ {
				item, _ := rows.ListItem(index)
				if shape == "embedded" {
					item = item.Get("content")
				}
				value, present := item.Lookup("translation")
				switch index {
				case 0:
					if !present || value.Kind() != store.ValueNull {
						t.Fatal("explicit null translation changed")
					}
				case 1:
					if text, _ := value.StringValue(); !present || text != fallback {
						t.Fatal("new occurrence did not receive its localized default")
					}
				case 2:
					if present {
						t.Fatal("reordered retained occurrence acquired an omitted translation")
					}
				}
			}
			// Revalidating a complete immutable candidate needs no container writes.
			collector := &validationIssueCollector{}
			_, changed := validateObject(fields, store.Object(validated), options, validators, collector)
			if changed || len(collector.values) != 0 {
				t.Fatalf("complete candidate changed: changed=%v issues=%#v", changed, collector.values)
			}
			validated["outside"] = store.String("caller edit")
			after, _ := json.Marshal(input)
			priorAfter, _ := json.Marshal(previous)
			if string(before) != string(after) || string(priorBefore) != string(priorAfter) {
				t.Fatal("validation mutated retained candidate or prior snapshot")
			}
		})
	}
}

func TestImmutableValidationPreservesRejectedRowsAndUnknownNames(t *testing.T) {
	field := schema.Field{Name: "items", Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: []schema.Field{{Name: "name", Type: schema.FieldTypeText, Required: true}}}}
	input := store.Values{"items": store.List(store.Number(1), store.Object(store.Values{"_key": store.String("same"), "name": store.String("first")}), store.Object(store.Values{"_key": store.String("same"), "name": store.String("second")}))}
	validated, issues := validate([]schema.Field{field}, input, true, nil)
	if len(issues) != 2 || issues[0].Code != "invalid_type" || issues[0].Path != "items.0" || issues[1].Code != "duplicate_row_key" || issues[1].Path != "items.2._key" {
		t.Fatalf("rejected-row issues = %#v", issues)
	}
	first, _ := validated["items"].ListItem(0)
	if first.Kind() != "" {
		t.Fatal("invalid row result lost its zero-value representation")
	}
	original, _ := input["items"].ListItem(0)
	if original.Kind() != store.ValueNumber {
		t.Fatal("rejected validation changed input")
	}
	_, issues = validate(nil, store.Values{"": store.Null()}, false, nil)
	if len(issues) != 1 || issues[0].Code != "unknown_field" {
		t.Fatalf("empty unknown name was treated as metadata: %#v", issues)
	}
}

func TestDefaultGroupPreparationKeepsRetainedInputsAndScopeRules(t *testing.T) {
	literal := "literal"
	group := schema.Field{Name: "settings", Type: schema.FieldTypeGroup, Localized: true, Nested: &schema.NestedField{Fields: []schema.Field{{Name: "title", Type: schema.FieldTypeText, Default: &literal}}}}
	rows := schema.Field{Name: "items", Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: []schema.Field{group}}}
	input := store.Values{"items": store.List(store.Object(store.Values{"_key": store.String("new")}), store.Object(store.Values{"_key": store.String("old")}))}
	previous := store.Values{"items": store.List(store.Object(store.Values{"_key": store.String("old")}))}
	before, _ := json.Marshal(input)
	prepared, err := prepareDefaultGroups([]schema.Field{rows}, input, validationOptions{requireMissing: true, previous: previous})
	if err != nil {
		t.Fatal(err)
	}
	newRow, _ := prepared["items"].ListItem(0)
	oldRow, _ := prepared["items"].ListItem(1)
	if title, _ := newRow.Get("settings").Get("title").StringValue(); title != literal {
		t.Fatal("new row group literal was not prepared")
	}
	if _, exists := oldRow.Lookup("settings"); exists {
		t.Fatal("retained row acquired absent localized group")
	}
	prepared["outside"] = store.String("caller edit")
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("group preparation mutated its retained input")
	}
}
