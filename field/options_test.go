package field_test

import (
	"reflect"
	"testing"

	"github.com/riducms/ridu/field"
)

func TestOptionInputsAreDetachedAndReplaceConstructorValues(t *testing.T) {
	for _, test := range []struct {
		name    string
		strings func(...string) field.Node
		options func(...field.Option) field.Node
	}{
		{"select", func(values ...string) field.Node { return field.Select("status", values...) }, func(options ...field.Option) field.Node {
			return field.Select("status", "discarded").Options(options...)
		}},
		{"radio", func(values ...string) field.Node { return field.Radio("status", values...) }, func(options ...field.Option) field.Node {
			return field.Radio("status", "discarded").Options(options...)
		}},
		{"multi-select", func(values ...string) field.Node { return field.MultiSelect("status", values...) }, func(options ...field.Option) field.Node {
			return field.MultiSelect("status", "discarded").Options(options...)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := []string{"draft", "in-review"}
			shorthand := test.strings(values...)
			values[0] = "changed"
			if got := field.Snapshot(shorthand).Options(); !reflect.DeepEqual(got, []field.Option{{Value: "draft"}, {Value: "in-review"}}) {
				t.Fatalf("constructor options = %#v", got)
			}
			options := []field.Option{{Value: "published", Label: "Live", LabelTranslations: map[string]string{"fr": "En ligne"}}}
			detailed := test.options(options...)
			options[0].Value = "changed"
			options[0].LabelTranslations["fr"] = "changed"
			got := field.Snapshot(detailed).Options()
			want := []field.Option{{Value: "published", Label: "Live", LabelTranslations: map[string]string{"fr": "En ligne"}}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("replacement options = %#v, want %#v", got, want)
			}
			if got := field.Snapshot(test.options()).Options(); len(got) != 0 {
				t.Fatalf("empty replacement retained constructor options: %#v", got)
			}
		})
	}
}
