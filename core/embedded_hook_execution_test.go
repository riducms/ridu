package core_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
	outline "github.com/riducms/ridu/tests/contracts/embedded_plugin"
)

func TestEmbeddedHookExecutionUsesCurrentValuesAndRetainsSnapshots(t *testing.T) {
	for _, phase := range []string{"write", "read"} {
		t.Run(phase, func(t *testing.T) {
			active := false
			var order []string
			var snapshots []operation.Context
			var expectedRoots [][]string
			var expectedSiblings []string
			current := []string{"one", "two", "three"}
			observe := func(ctx operation.Context, input operation.Value[string], step int) (operation.Change[string], error) {
				if !active {
					return operation.Keep[string](), nil
				}
				key, _ := ctx.Siblings.String("uid")
				index := map[string]int{"A": 0, "B": 1, "C": 2}[key]
				order = append(order, fmt.Sprintf("%s%d", key, step))
				value, _ := input.Get()
				if value != current[index] {
					t.Fatalf("%s input=%q, want current value %q", order[len(order)-1], value, current[index])
				}
				if sibling, _ := ctx.Siblings.String("title"); sibling != value {
					t.Fatalf("%s sibling=%q, input=%q", order[len(order)-1], sibling, value)
				}
				if got := embeddedExecutionTitles(t, ctx.Root.Get("body")); !reflect.DeepEqual(got, current) {
					t.Fatalf("%s root=%v, want current values %v", order[len(order)-1], got, current)
				}
				snapshots = append(snapshots, ctx)
				expectedRoots = append(expectedRoots, append([]string(nil), current...))
				expectedSiblings = append(expectedSiblings, value)
				current[index] = fmt.Sprintf("%s|%d", value, step)
				return operation.Replace(operation.Present(current[index])), nil
			}
			title := field.Text("title").Required()
			if phase == "write" {
				title = title.Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{
					func(ctx operation.WriteContext, input operation.Value[string]) (operation.Change[string], error) {
						return observe(operation.Context(ctx), input, 1)
					},
					func(ctx operation.WriteContext, input operation.Value[string]) (operation.Change[string], error) {
						return observe(operation.Context(ctx), input, 2)
					},
				}})
			} else {
				title = title.ReadHooks(field.ReadHooks[string]{AfterRead: []field.OutputTransform[string]{
					func(ctx operation.ReadContext, input operation.Value[string]) (operation.Change[string], error) {
						return observe(operation.Context(ctx), input, 1)
					},
					func(ctx operation.ReadContext, input operation.Value[string]) (operation.Change[string], error) {
						return observe(operation.Context(ctx), input, 2)
					},
				}})
			}
			app, err := ridu.New(embeddedConfig(title), teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			created, err := app.Local().Create(t.Context(), "pages", store.Values{"body": outline.Value(
				outline.Widget("card", "A", store.Values{"title": store.String("one")}),
				outline.Widget("card", "B", store.Values{"title": store.String("two")}),
				outline.Widget("card", "C", store.Values{"title": store.String("three")}),
			)}, nil)
			if err != nil {
				t.Fatal(err)
			}
			active = true
			updated, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if want := []string{"A1", "A2", "B1", "B2", "C1", "C2"}; !reflect.DeepEqual(order, want) {
				t.Fatalf("hook order=%v, want %v", order, want)
			}
			if got := embeddedExecutionTitles(t, updated.Values["body"]); !reflect.DeepEqual(got, current) {
				t.Fatalf("response=%v, want %v", got, current)
			}
			// Retain the actual public views until all later writes have completed.
			// Refreshing one callback must never mutate a previous callback's root.
			for i, snapshot := range snapshots {
				if got := embeddedExecutionTitles(t, snapshot.Root.Get("body")); !reflect.DeepEqual(got, expectedRoots[i]) {
					t.Fatalf("retained root %d changed: %v, want %v", i, got, expectedRoots[i])
				}
				if got, _ := snapshot.Siblings.String("title"); got != expectedSiblings[i] {
					t.Fatalf("retained siblings %d changed: %q, want %q", i, got, expectedSiblings[i])
				}
			}
			active = false
			stored, err := app.Local().Find(t.Context(), "pages", created.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			wantStored := current
			if phase == "read" {
				wantStored = []string{"one", "two", "three"}
			}
			if got := embeddedExecutionTitles(t, stored.Values["body"]); !reflect.DeepEqual(got, wantStored) {
				t.Fatalf("persisted=%v, want %v", got, wantStored)
			}
		})
	}
}

func TestEmbeddedHookExecutionPreparesNewNestedIdentitiesBetweenCallbacks(t *testing.T) {
	active := false
	var order []string
	var parentKey, parentRowKey string
	var freshID operation.OccurrenceID
	retainedIDs := map[string]operation.OccurrenceID{}
	prior := map[string]string{}
	title := field.Text("title").Required().Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(ctx operation.WriteContext, input operation.Value[string]) (operation.Change[string], error) {
		key, _ := ctx.Siblings.String("uid")
		if active {
			order = append(order, "child:"+key)
			if old, ok := ctx.Prior.String("title"); ok {
				prior[key] = old
				if retainedIDs[key] != ctx.OccurrenceID {
					t.Fatalf("retained %q changed identity", key)
				}
			} else {
				prior[key] = "<new>"
				freshID = ctx.OccurrenceID
				if key == "" || key != parentKey || embeddedExecutionRowKey(t, ctx.Siblings.Get("links")) != parentRowKey {
					t.Fatalf("child did not receive prepared identities: uid=%q, parent uid=%q", key, parentKey)
				}
			}
		} else {
			retainedIDs[key] = ctx.OccurrenceID
		}
		text, _ := input.Get()
		return operation.Replace(operation.Present(strings.ToUpper(text))), nil
	}}, AfterChange: []field.Observer[string]{func(ctx operation.EventContext, _ operation.Value[string]) error {
		if active {
			key, _ := ctx.Siblings.String("uid")
			if key == parentKey && (ctx.OccurrenceID != freshID || embeddedExecutionRowKey(t, ctx.Siblings.Get("links")) != parentRowKey) {
				t.Fatal("prepared identities changed before AfterChange")
			}
		}
		return nil
	}}})
	body := outline.Field("body", embeddedCard(title)).Hooks(field.Hooks[store.Value]{BeforeChange: []field.Transform[store.Value]{
		func(_ operation.WriteContext, input operation.Value[store.Value]) (operation.Change[store.Value], error) {
			if !active {
				return operation.Keep[store.Value](), nil
			}
			order = append(order, "parent:replace")
			value, _ := input.Get()
			envelope, _ := value.CopyObject()
			nodes, _ := envelope["outline"].CopyList()
			fresh := outline.Widget("card", "", store.Values{"title": store.String("fresh"), "links": store.List(store.Object(store.Values{"label": store.String("new link")}))})
			section := store.Object(store.Values{"kind": store.String("section"), "items": store.List(fresh)})
			return operation.Replace(operation.Present(outline.Value(nodes[2], nodes[0], section))), nil
		},
		func(ctx operation.WriteContext, input operation.Value[store.Value]) (operation.Change[store.Value], error) {
			if !active {
				return operation.Keep[store.Value](), nil
			}
			order = append(order, "parent:observe")
			value, _ := input.Get()
			payloads := embeddedExecutionPayloads(t, value)
			if len(payloads) != 3 {
				t.Fatalf("parent replacement children=%d", len(payloads))
			}
			parentKey = embeddedString(payloads[2], "uid")
			parentRowKey = embeddedExecutionRowKey(t, payloads[2]["links"])
			if parentKey == "" || parentKey == "A" || parentKey == "B" || parentKey == "C" || parentRowKey == "" {
				t.Fatalf("next parent hook saw unprepared identities: uid=%q row=%q", parentKey, parentRowKey)
			}
			if root := embeddedExecutionPayloads(t, ctx.Root.Get("body")); embeddedString(root[2], "uid") != parentKey {
				t.Fatal("parent root and input disagree on prepared identity")
			}
			return operation.Keep[store.Value](), nil
		},
	}})
	config := embeddedConfig()
	config.Collections[1].Fields = field.Fields{body}
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"body": outline.Value(
		outline.Widget("card", "A", store.Values{"title": store.String("one")}),
		outline.Widget("card", "B", store.Values{"title": store.String("removed")}),
		outline.Widget("card", "C", store.Values{"title": store.String("three")}),
	)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	active = true
	updated, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"parent:replace", "parent:observe", "child:C", "child:A", "child:" + parentKey}; !reflect.DeepEqual(order, want) {
		t.Fatalf("hook order=%v, want %v", order, want)
	}
	if want := map[string]string{"C": "THREE", "A": "ONE", parentKey: "<new>"}; !reflect.DeepEqual(prior, want) {
		t.Fatalf("previous scopes=%v, want %v", prior, want)
	}
	stored, err := app.Local().Find(t.Context(), "pages", created.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range []store.Document{updated, stored} {
		payloads := embeddedExecutionPayloads(t, document.Values["body"])
		if len(payloads) != 3 || embeddedString(payloads[2], "uid") != parentKey || embeddedString(payloads[2], "title") != "FRESH" || embeddedExecutionRowKey(t, payloads[2]["links"]) != parentRowKey {
			t.Fatalf("prepared child was not preserved: %#v", payloads)
		}
	}
}

func TestEmbeddedHookExecutionRejectsInvalidReplacementBeforeFurtherCallbacks(t *testing.T) {
	deep := outline.Widget("card", "new", store.Values{"title": store.String("too deep")})
	for range 70 {
		deep = store.Object(store.Values{"kind": store.String("section"), "items": store.List(deep)})
	}
	for _, test := range []struct {
		name, code string
		value      store.Value
	}{
		{"duplicate embedded identity", "duplicate_row_key", outline.Value(
			outline.Widget("card", "same", store.Values{"title": store.String("one")}),
			outline.Widget("card", "same", store.Values{"title": store.String("two")}),
		)},
		{"structural depth", "embedded_limit", outline.Value(deep)},
		{"duplicate nested row identity", "duplicate_row_key", outline.Value(outline.Widget("card", "new", store.Values{
			"title": store.String("new"), "links": store.List(
				store.Object(store.Values{"_key": store.String("same"), "label": store.String("one")}),
				store.Object(store.Values{"_key": store.String("same"), "label": store.String("two")}),
			),
		}))},
	} {
		t.Run(test.name, func(t *testing.T) {
			active := false
			nextParentCalls, childCalls := 0, 0
			title := field.Text("title").Required().Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(operation.WriteContext, operation.Value[string]) (operation.Change[string], error) {
				if active {
					childCalls++
				}
				return operation.Keep[string](), nil
			}}})
			body := outline.Field("body", embeddedCard(title)).Hooks(field.Hooks[store.Value]{BeforeChange: []field.Transform[store.Value]{
				func(operation.WriteContext, operation.Value[store.Value]) (operation.Change[store.Value], error) {
					if active {
						return operation.Replace(operation.Present(test.value)), nil
					}
					return operation.Keep[store.Value](), nil
				},
				func(operation.WriteContext, operation.Value[store.Value]) (operation.Change[store.Value], error) {
					if active {
						nextParentCalls++
					}
					return operation.Keep[store.Value](), nil
				},
			}})
			config := embeddedConfig()
			config.Collections[1].Fields = field.Fields{body}
			app, err := ridu.New(config, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			created, err := app.Local().Create(t.Context(), "pages", store.Values{"body": outline.Value(outline.Widget("card", "saved", store.Values{"title": store.String("unchanged")}))}, nil)
			if err != nil {
				t.Fatal(err)
			}
			active = true
			_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{}, nil)
			foundIssue := false
			for cause := err; cause != nil; cause = errors.Unwrap(cause) {
				var failure *ridu.OperationError
				if errors.As(cause, &failure) {
					for _, issue := range failure.Issues {
						foundIssue = foundIssue || issue.Code == test.code
					}
				}
			}
			if !foundIssue {
				t.Fatalf("invalid replacement error=%v, want issue %q", err, test.code)
			}
			if nextParentCalls != 0 || childCalls != 0 {
				t.Fatalf("invalid replacement reached callbacks: next parent=%d children=%d", nextParentCalls, childCalls)
			}
			stored, err := app.Local().Find(t.Context(), "pages", created.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Revision != created.Revision || !reflect.DeepEqual(stored.Values, created.Values) {
				t.Fatal("invalid replacement changed persisted values or revision")
			}
		})
	}
}

func embeddedExecutionTitles(t *testing.T, value store.Value) []string {
	t.Helper()
	var titles []string
	for _, payload := range embeddedExecutionPayloads(t, value) {
		titles = append(titles, embeddedString(payload, "title"))
	}
	return titles
}

func embeddedExecutionPayloads(t *testing.T, value store.Value) []store.Values {
	t.Helper()
	envelope, ok := value.CopyObject()
	if !ok {
		t.Fatal("missing outline envelope")
	}
	nodes, ok := envelope["outline"].CopyList()
	if !ok {
		t.Fatal("missing outline nodes")
	}
	var payloads []store.Values
	var walk func([]store.Value)
	walk = func(nodes []store.Value) {
		for _, node := range nodes {
			object, _ := node.CopyObject()
			if embeddedString(object, "kind") == "widget" {
				payload, ok := object["content"].CopyObject()
				if !ok {
					t.Fatal("missing widget payload")
				}
				payloads = append(payloads, payload)
			}
			children, _ := object["items"].CopyList()
			walk(children)
		}
	}
	walk(nodes)
	return payloads
}

func embeddedExecutionRowKey(t *testing.T, value store.Value) string {
	t.Helper()
	rows, ok := value.CopyList()
	if !ok || len(rows) != 1 {
		t.Fatalf("expected one nested row, got %v", rows)
	}
	row, _ := rows[0].CopyObject()
	return embeddedString(row, "_key")
}
