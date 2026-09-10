package localization

import (
	"reflect"
	"testing"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestLocalizedStructuredStorageMergesPreserveOmittedSiblings(t *testing.T) {
	text := func(name string) schema.Field {
		return schema.Field{Name: name, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar}
	}
	fields := []schema.Field{
		{
			Name: "group", Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested, Localized: true,
			Nested: &schema.NestedField{Fields: []schema.Field{text("headline"), text("summary")}},
		},
		{
			Name: "rows", Type: schema.FieldTypeArray, Category: schema.FieldCategoryNested, Localized: true,
			Nested: &schema.NestedField{Fields: []schema.Field{text("headline"), text("summary")}},
		},
		{
			Name: "blocks", Type: schema.FieldTypeBlocks, Category: schema.FieldCategoryNested, Localized: true,
			Blocks: &schema.BlocksField{Types: []schema.BlockType{{
				Slug: "quote", Fields: []schema.Field{text("headline"), text("summary")},
			}}},
		},
	}
	current := store.Values{
		"group": store.Object(store.Values{
			"en": store.Object(store.Values{"headline": store.String("old"), "summary": store.String("keep")}),
			"fr": store.Object(store.Values{"headline": store.String("ancien"), "summary": store.String("garder")}),
		}),
		"rows": store.Object(store.Values{
			"en": store.List(store.Object(store.Values{
				"_key": store.String("row-en"), "headline": store.String("old"), "summary": store.String("keep"),
			})),
			"fr": store.List(store.Object(store.Values{
				"_key": store.String("row-fr"), "headline": store.String("ancien"), "summary": store.String("garder"),
			})),
		}),
		"blocks": store.Object(store.Values{
			"en": store.List(store.Object(store.Values{
				"_key": store.String("block-en"), "blockType": store.String("quote"),
				"headline": store.String("old"), "summary": store.String("keep"),
			})),
			"fr": store.List(store.Object(store.Values{
				"_key": store.String("block-fr"), "blockType": store.String("quote"),
				"headline": store.String("ancien"), "summary": store.String("garder"),
			})),
		}),
	}
	patch := store.Values{
		"group": store.Object(store.Values{
			"en": store.Object(store.Values{"headline": store.String("new")}),
		}),
		"rows": store.Object(store.Values{
			"en": store.List(store.Object(store.Values{
				"_key": store.String("row-en"), "headline": store.String("new"),
			})),
		}),
		"blocks": store.Object(store.Values{
			"en": store.List(store.Object(store.Values{
				"_key": store.String("block-en"), "blockType": store.String("quote"), "headline": store.String("new"),
			})),
		}),
	}
	want := store.Values{
		"group": store.Object(store.Values{
			"en": store.Object(store.Values{"headline": store.String("new"), "summary": store.String("keep")}),
			"fr": store.Object(store.Values{"headline": store.String("ancien"), "summary": store.String("garder")}),
		}),
		"rows": store.Object(store.Values{
			"en": store.List(store.Object(store.Values{
				"_key": store.String("row-en"), "headline": store.String("new"), "summary": store.String("keep"),
			})),
			"fr": store.List(store.Object(store.Values{
				"_key": store.String("row-fr"), "headline": store.String("ancien"), "summary": store.String("garder"),
			})),
		}),
		"blocks": store.Object(store.Values{
			"en": store.List(store.Object(store.Values{
				"_key": store.String("block-en"), "blockType": store.String("quote"),
				"headline": store.String("new"), "summary": store.String("keep"),
			})),
			"fr": store.List(store.Object(store.Values{
				"_key": store.String("block-fr"), "blockType": store.String("quote"),
				"headline": store.String("ancien"), "summary": store.String("garder"),
			})),
		}),
	}

	for _, merge := range []struct {
		name string
		call func([]schema.Field, store.Values, store.Values) store.Values
	}{
		{name: "storage patch", call: MergeStoragePatch},
		{name: "storage update", call: MergeStorageUpdate},
	} {
		t.Run(merge.name, func(t *testing.T) {
			got := merge.call(fields, store.CloneValues(current), store.CloneValues(patch))
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("merged localized containers = %#v, want %#v", got, want)
			}
		})
	}
}
