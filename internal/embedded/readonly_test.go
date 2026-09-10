package embedded

import (
	"errors"
	"reflect"
	"testing"

	"github.com/riducms/ridu/store"
)

func TestImmutableVisitPreservesOrderMetadataAndRetainedPayloads(t *testing.T) {
	parent, _ := widget("parent").CopyObject()
	parent["items"] = store.List(widget("child"))
	input := envelope(store.Object(parent), widget("sibling"))
	legacy, err := Occurrences(testField(), input, "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	var observed []ReadOccurrence
	if err := Visit(testField(), input, "body", nil, func(o ReadOccurrence) error {
		observed = append(observed, o)
		copy, _ := o.Payload.CopyObject()
		copy["uid"] = store.String("edited copy")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := TransformValue(testField(), input, "body", nil, func(o ReadOccurrence) (store.Value, bool, error) {
		if o.Key != "child" {
			// The flag is authoritative; this replacement must be ignored.
			return store.Null(), false, nil
		}
		payload, _ := o.Payload.CopyObject()
		payload["title"] = store.String("changed child")
		return store.Object(payload), true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(observed) != 3 {
		t.Fatalf("observed=%d", len(observed))
	}
	for i, o := range observed {
		if !reflect.DeepEqual(o.detached(), legacy[i]) {
			t.Fatalf("retained occurrence %d changed or metadata differs: %#v", i, o)
		}
	}
	var titles []string
	if err := Visit(testField(), updated, "body", nil, func(o ReadOccurrence) error {
		title, _ := o.Payload.Get("title").StringValue()
		titles = append(titles, title)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(titles, []string{"", "changed child", ""}) {
		t.Fatalf("mapped titles=%v", titles)
	}
}

func TestImmutableTransformLaterTreesSeeEarlierUpdates(t *testing.T) {
	field := testField()
	second := field.Plugin.EmbeddedTrees[0]
	second.Key = "second"
	field.Plugin.EmbeddedTrees = append(field.Plugin.EmbeddedTrees, second)
	input := envelope(widget("one"))
	var retained store.Value
	var seen []string
	updated, err := TransformValue(field, input, "body", nil, func(o ReadOccurrence) (store.Value, bool, error) {
		seen = append(seen, o.Tree.Key)
		if o.Tree.Key == "second" {
			if title, _ := o.Payload.Get("title").StringValue(); title != "first tree" {
				t.Fatalf("later tree title=%q", title)
			}
			return o.Payload, false, nil
		}
		retained = o.Payload
		copy, _ := o.Payload.CopyObject()
		copy["title"] = store.String("first tree")
		return store.Object(copy), true, nil
	})
	if err != nil || !reflect.DeepEqual(seen, []string{"cards", "second"}) {
		t.Fatalf("seen=%v error=%v", seen, err)
	}
	if _, exists := retained.Lookup("title"); exists {
		t.Fatal("retained earlier snapshot changed")
	}
	if reflect.DeepEqual(updated, input) {
		t.Fatal("first tree update was lost")
	}
}

func TestImmutableTraversalPreservesPartialCallbacksAndErrorRollback(t *testing.T) {
	callbackError := errors.New("callback stopped")
	for _, test := range []struct {
		name     string
		input    store.Value
		budget   func() *Budget
		callback error
		path     string
		code     string
	}{
		{"later duplicate key", envelope(widget("one"), widget("one")), NewBudget, nil, "body.outline.1.content.uid", "duplicate_row_key"},
		{"later malformed node", envelope(widget("one"), store.String("bad")), NewBudget, nil, "body.outline.1", "invalid_embedded_node"},
		{"shared node budget", envelope(widget("one"), widget("two")), func() *Budget { return &Budget{nodes: MaxNodes - 1} }, nil, "body.outline.1", "embedded_limit"},
		{"callback error", envelope(widget("one"), widget("two")), NewBudget, callbackError, "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, mapping := range []bool{false, true} {
				budget := test.budget()
				calls := 0
				var result store.Value
				var err error
				if mapping {
					result, err = TransformValue(testField(), test.input, "body", budget, func(o ReadOccurrence) (store.Value, bool, error) {
						calls++
						copy, _ := o.Payload.CopyObject()
						copy["title"] = store.String("must roll back")
						return store.Object(copy), true, test.callback
					})
				} else {
					err = Visit(testField(), test.input, "body", budget, func(ReadOccurrence) error {
						calls++
						return test.callback
					})
				}
				if calls != 1 || budget.depth != 0 {
					t.Fatalf("mapping=%v callbacks=%d depth=%d", mapping, calls, budget.depth)
				}
				if test.callback != nil {
					if !errors.Is(err, callbackError) {
						t.Fatalf("callback error=%v", err)
					}
				} else {
					var failure *Error
					if !errors.As(err, &failure) || failure.Issue.Path != test.path || failure.Issue.Code != test.code {
						t.Fatalf("mapping=%v error=%v", mapping, err)
					}
				}
				if mapping && !reflect.DeepEqual(result, test.input) {
					t.Fatal("partially transformed value escaped after error")
				}
			}
		})
	}
}

func TestImmutableTransformNoopRetainsOriginalValue(t *testing.T) {
	input := envelope(widget("one"), widget("two"))
	calls := 0
	updated, err := TransformValue(testField(), input, "body", nil, func(ReadOccurrence) (store.Value, bool, error) {
		calls++
		return store.Null(), false, nil
	})
	if err != nil || calls != 2 || !reflect.DeepEqual(updated, input) {
		t.Fatalf("callbacks=%d error=%v updated=%#v", calls, err, updated)
	}
}
