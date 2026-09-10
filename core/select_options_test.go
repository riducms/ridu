package core_test

import (
	"bytes"
	"errors"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

func TestStringOptionsResolveLikeTypedOptions(t *testing.T) {
	for _, constructor := range []struct {
		name    string
		strings func(...string) field.Node
		options func(...field.Option) field.Node
	}{
		{"select", func(values ...string) field.Node { return field.Select("status", values...).Default("draft") }, func(options ...field.Option) field.Node {
			return field.Select("status").Options(options...).Default("draft")
		}},
		{"radio", func(values ...string) field.Node { return field.Radio("status", values...).Default("draft") }, func(options ...field.Option) field.Node {
			return field.Radio("status").Options(options...).Default("draft")
		}},
		{"multi-select", func(values ...string) field.Node { return field.MultiSelect("status", values...).Default("draft") }, func(options ...field.Option) field.Node {
			return field.MultiSelect("status").Options(options...).Default("draft")
		}},
	} {
		for _, test := range []struct {
			name   string
			values []string
			code   string
		}{
			{"valid", []string{"draft", "in-review", "published"}, ""},
			{"missing", nil, "missing_select_options"},
			{"empty", []string{"draft", ""}, "missing_select_value"},
			{"duplicate", []string{"draft", "draft"}, "duplicate_select_value"},
			{"invalid-default", []string{"published"}, "invalid_select_default"},
		} {
			t.Run(constructor.name+"/"+test.name, func(t *testing.T) {
				options := make([]field.Option, len(test.values))
				for i, v := range test.values {
					options[i] = field.Option{Value: v}
				}
				resolve := func(node field.Node) (schema.Manifest, error) {
					return ridu.Resolve(ridu.Config{Name: "Options", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{node}}}})
				}
				shorthand, stringErr := resolve(constructor.strings(test.values...))
				detailed, optionErr := resolve(constructor.options(options...))
				if test.code != "" {
					if stringErr == nil || optionErr == nil || stringErr.Error() != optionErr.Error() {
						t.Fatalf("validation differs: strings=%v, options=%v", stringErr, optionErr)
					}
					var validation *schema.ValidationError
					if !errors.As(stringErr, &validation) {
						t.Fatal(stringErr)
					}
					for _, issue := range validation.Issues {
						if issue.Code == test.code {
							return
						}
					}
					t.Fatalf("missing %s in %v", test.code, stringErr)
				}
				if stringErr != nil || optionErr != nil {
					t.Fatalf("strings=%v, options=%v", stringErr, optionErr)
				}
				a, err := shorthand.Bytes()
				if err != nil {
					t.Fatal(err)
				}
				b, err := detailed.Bytes()
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(a, b) {
					t.Fatalf("manifests differ:\n%s\n%s", a, b)
				}
				if !bytes.Contains(a, []byte(`"options":`)) || bytes.Contains(a, []byte(`"choices":`)) {
					t.Fatalf("unexpected select metadata property: %s", a)
				}
				resolved := shorthand.Snapshot().Collections[0].Fields[0].Select.Options
				if resolved[1].Value != "in-review" || resolved[1].Label != "In review" {
					t.Fatalf("stored value/generated label = %#v", resolved[1])
				}
			})
		}
	}
}
