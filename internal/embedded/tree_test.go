package embedded

import (
	"errors"
	"fmt"
	"testing"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func testField() schema.Field {
	return schema.Field{Name: "content", Type: schema.FieldTypePlugin, Plugin: &schema.PluginField{EmbeddedTrees: []schema.EmbeddedTree{{Version: 1, Key: "cards", Root: []string{"outline"}, Children: "items", Tag: "kind", Cases: []schema.EmbeddedTreeCase{{TagValue: "widget", Payload: "content", Discriminator: "schema", Identity: "uid", Types: []schema.BlockType{{Slug: "card"}}}}}}}}
}
func widget(key string) store.Value {
	return store.Object(store.Values{"kind": store.String("widget"), "content": store.Object(store.Values{"schema": store.String("card"), "uid": store.String(key)})})
}
func envelope(nodes ...store.Value) store.Value {
	return store.Object(store.Values{"outline": store.List(nodes...)})
}

func TestEnvelopeTransformsOnlyDeclaredPayloads(t *testing.T) {
	node, _ := widget("one").CopyObject()
	payload, _ := node["content"].CopyObject()
	payload["items"] = store.List(widget("decoy"))
	node["content"] = store.Object(payload)
	node["items"] = store.List(widget("two"))
	calls := 0
	updated, err := Transform(testField(), envelope(store.Object(node)), "body", nil, func(o Occurrence) (store.Values, error) {
		calls++
		o.Payload["title"] = store.String(o.Key)
		return o.Payload, nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("calls %d error %v", calls, err)
	}
	occurrences, err := Occurrences(testField(), updated, "body", nil)
	if err != nil || len(occurrences) != 2 {
		t.Fatal(err)
	}
	if occurrences[1].RuntimePath != "body.outline.0.items.0.content" {
		t.Fatal(occurrences[1].RuntimePath)
	}
	original, _ := node["content"].CopyObject()
	if _, exists := original["title"]; exists {
		t.Fatal("transform mutated source")
	}
}
func TestEnvelopeErrorsArePreciseAndBounded(t *testing.T) {
	tests := []struct {
		name  string
		value store.Value
		path  string
	}{
		{"missing root", store.Object(store.Values{}), "body.outline"},
		{"nonobject", envelope(store.String("bad")), "body.outline.0"},
		{"duplicate", envelope(widget("one"), widget("one")), "body.outline.1.content.uid"},
		{"missing payload", envelope(store.Object(store.Values{"kind": store.String("widget")})), "body.outline.0.content"},
		{"missing discriminator", envelope(store.Object(store.Values{"kind": store.String("widget"), "content": store.Object(store.Values{})})), "body.outline.0.content.schema"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Occurrences(testField(), test.value, "body", nil)
			var failure *Error
			if !errors.As(err, &failure) || failure.Issue.Path != test.path {
				t.Fatalf("error %v", err)
			}
		})
	}
	nodes := make([]store.Value, MaxNodes+1)
	for i := range nodes {
		nodes[i] = store.Object(store.Values{"kind": store.String(fmt.Sprint(i))})
	}
	_, err := Occurrences(testField(), envelope(nodes...), "body", nil)
	var failure *Error
	if !errors.As(err, &failure) || failure.Issue.Code != "embedded_limit" {
		t.Fatalf("budget: %v", err)
	}
}
func TestStableIdentityIndependentOfTreePosition(t *testing.T) {
	before, _ := Occurrences(testField(), envelope(widget("one"), widget("two")), "body", nil)
	after, _ := Occurrences(testField(), envelope(widget("two"), widget("one")), "body", nil)
	if before[0].Identity != after[1].Identity || before[0].RuntimePath == after[1].RuntimePath {
		t.Fatal("identity depends on position")
	}
}

func TestEnvelopeReferenceRewriteDoesNotReenterPayloads(t *testing.T) {
	field := testField()
	field.Plugin.ReferenceKeys = []string{"relationTo"}
	calls := 0
	value := envelope(widget("one"), widget("two"), store.Object(store.Values{"kind": store.String("link"), "relationTo": store.String("authors")}))
	updated, changed, err := RewriteCollectionReferences(field, value, "authors", "people", func(o Occurrence) (store.Values, error) {
		calls++
		o.Payload["raw"] = store.Object(store.Values{"relationTo": store.String("authors"), "items": store.List(widget("bait"))})
		return o.Payload, nil
	})
	if err != nil || !changed || calls != 2 {
		t.Fatalf("calls=%d changed=%v error=%v", calls, changed, err)
	}
	occurrences, err := Occurrences(field, updated, "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, occurrence := range occurrences {
		raw, _ := occurrence.Payload["raw"].CopyObject()
		if relation, _ := raw["relationTo"].StringValue(); relation != "authors" {
			t.Fatal("payload revisited by envelope reference scan")
		}
	}
	original, _ := Occurrences(field, value, "body", nil)
	if _, exists := original[0].Payload["raw"]; exists {
		t.Fatal("source mutated")
	}
}
