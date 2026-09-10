package operation

import (
	"testing"

	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestCompleteUpdatePreservesLocalizedNullEmptyAndInvalidInput(t *testing.T) {
	fields := []schema.Field{{Name: "layout", Type: schema.FieldTypeBlocks, Blocks: &schema.BlocksField{Types: []schema.BlockType{{Slug: "hero", Fields: []schema.Field{
		{Name: "secret", Type: schema.FieldTypeText, Localized: true},
	}}}}}}
	selection := localization.Selection{Locale: "fr", Chain: []schema.LocaleCode{"fr", "en"}, Configured: []schema.LocaleCode{"en", "fr"}}
	for _, value := range []store.Value{store.Null(), store.String("")} {
		for _, supplied := range []bool{false, true} {
			previous := store.String("previous")
			if !supplied {
				previous = value
			}
			current := store.Values{"layout": store.List(store.Object(store.Values{
				"_key": store.String("row"), "blockType": store.String("hero"),
				"secret": store.Object(store.Values{"en": store.String("fallback"), "fr": previous}),
			}))}
			row := store.Values{"_key": store.String("row"), "blockType": store.String("hero")}
			if supplied {
				row["secret"] = value
			}
			patch := store.Values{"layout": store.List(store.Object(row)), "undeclared": store.String("reject me")}
			completed, err := completeUpdateForValidation(fields, current, patch, selection, nil)
			if err != nil {
				t.Fatal(err)
			}
			rows, _ := completed["layout"].CopyList()
			actual, _ := rows[0].CopyObject()
			secret, exists := actual["secret"]
			if !exists || secret.Kind() != value.Kind() {
				t.Fatalf("null/empty value changed: supplied=%v secret=%#v", supplied, secret)
			}
			if text, _ := secret.StringValue(); value.Kind() == store.ValueString && text != "" {
				t.Fatal("empty value used fallback")
			}
			if _, exists := completed["undeclared"]; !exists {
				t.Fatal("write completion hid invalid input from validation")
			}
		}
	}
}

func TestNewGroupsValidateMissingLocalizedChildren(t *testing.T) {
	fields := []schema.Field{{Name: "settings", Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{
		{Name: "required", Type: schema.FieldTypeText, Localized: true, Required: true},
	}}}}
	for _, previous := range []store.Values{nil, {}, {"settings": store.Null()}} {
		_, issues := validateWithOptions(fields, store.Values{"settings": store.Object(store.Values{})}, validationOptions{previous: previous}, nil)
		if len(issues) != 1 || issues[0].Code != "required" || issues[0].Path != "settings.required" {
			t.Fatalf("new group bypassed localized validation: previous=%#v issues=%#v", previous, issues)
		}
	}
}
