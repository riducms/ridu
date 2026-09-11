package core_test

import (
	"fmt"
	"reflect"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
	outline "github.com/riducms/ridu/tests/contracts/embedded_plugin"
)

// The long structural child list retains many successive immutable versions
// while hooks also replace values below the ordinary array and plugin envelope.
func TestEmbeddedValueSnapshotsAcrossNestedListUpdatesAndReads(t *testing.T) {
	type retainedSnapshot struct {
		context operation.Context
		root    []embeddedValueSnapshotEntry
		sibling string
		prior   string
	}
	var snapshots []retainedSnapshot
	var expected []embeddedValueSnapshotEntry
	var observed []string
	phase := ""
	originalTitles := map[string]string{}
	originalIDs := map[string]operation.OccurrenceID{}
	writeIDs := map[string]operation.OccurrenceID{}
	newKey := ""
	observe := func(ctx operation.Context, input operation.Value[string]) (operation.Change[string], error) {
		key, _ := ctx.Siblings.String("uid")
		if phase == "" {
			if ctx.Operation == operation.Create {
				originalIDs[key] = ctx.OccurrenceID
			}
			return operation.Keep[string](), nil
		}
		actual := embeddedValueSnapshotEntries(t, ctx.Root.Get("sections"))
		if len(actual) != len(expected) {
			t.Fatalf("%s root occurrences=%d, want %d", phase, len(actual), len(expected))
		}
		if len(observed) == 0 && phase == "write" {
			// The fresh node's identity must be prepared before the first child
			// callback, even though its own turn occurs much later in the batch.
			for i := range expected {
				if expected[i].key != "" {
					continue
				}
				newKey = actual[i].key
				if newKey == "" || originalTitles[newKey] != "" {
					t.Fatalf("new occurrence has missing or reused identity %q", newKey)
				}
				expected[i].key = newKey
			}
		}
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("%s callback %d root lost earlier changes or ordering:\ngot  %v\nwant %v", phase, len(observed), actual, expected)
		}
		index := len(observed)
		if index >= len(expected) || key != expected[index].key {
			t.Fatalf("%s callback %d targeted unexpected identity %q", phase, index, key)
		}
		value, present := input.Get()
		if !present || value != expected[index].title {
			t.Fatalf("%s callback %d input=%q, want %q", phase, index, value, expected[index].title)
		}
		if sibling, _ := ctx.Siblings.String("title"); sibling != value {
			t.Fatalf("%s current sibling=%q, input=%q", phase, sibling, value)
		}
		prior, hasPrior := ctx.Prior.String("title")
		if phase == "write" {
			if prior != originalTitles[key] || hasPrior != (key != newKey) {
				t.Fatalf("prior for %q=%q (present=%t), want persisted %q", key, prior, hasPrior, originalTitles[key])
			}
			if key != newKey && originalIDs[key] != ctx.OccurrenceID {
				t.Fatalf("reorder changed concrete identity for %q", key)
			}
			writeIDs[key] = ctx.OccurrenceID
		} else if hasPrior || ctx.OccurrenceID != writeIDs[key] {
			t.Fatalf("standalone read changed identity or acquired prior for %q", key)
		}
		snapshots = append(snapshots, retainedSnapshot{ctx, append([]embeddedValueSnapshotEntry(nil), expected...), value, prior})
		observed = append(observed, key)
		expected[index].title = phase + ":" + value
		return operation.Replace(operation.Present(expected[index].title)), nil
	}
	title := field.Text("title").Required().Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(ctx operation.Context, input operation.Value[string]) (operation.Change[string], error) {
		if phase == "read" {
			return operation.Keep[string](), nil
		}
		return observe(ctx, input)
	}}}).ReplaceAfterRead(func(ctx operation.Context, input operation.Value[string]) (operation.Change[string], error) {
		if phase != "read" {
			return operation.Keep[string](), nil
		}
		return observe(ctx, input)
	})
	config := embeddedConfig()
	config.Collections[1].Fields = field.Fields{field.Array("sections", field.Fields{outline.Field("body", embeddedCard(title))})}
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	makeNodes := func(prefix string, count int) []store.Value {
		nodes := make([]store.Value, count)
		for i := range nodes {
			key := fmt.Sprintf("%s-%02d", prefix, i)
			text := "original:" + key
			originalTitles[key] = text
			nodes[i] = outline.Widget("card", key, store.Values{"title": store.String(text)})
		}
		return nodes
	}
	section := func(key string, nodes []store.Value) store.Value {
		branch := store.Object(store.Values{"kind": store.String("section"), "items": store.List(nodes...)})
		return store.Object(store.Values{"_key": store.String(key), "body": outline.Value(branch)})
	}
	long, short := makeNodes("long", 65), makeNodes("short", 3)
	input := store.List(section("long-row", long), section("short-row", short))
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"sections": input}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(originalIDs) != 68 {
		t.Fatalf("create callback identities=%d, want 68", len(originalIDs))
	}
	reverse := func(values []store.Value) []store.Value {
		result := make([]store.Value, len(values))
		for i := range values {
			result[i] = values[len(values)-1-i]
		}
		return result
	}
	long = reverse(long)
	long = append(long[:32], append([]store.Value{outline.Widget("card", "", store.Values{"title": store.String("fresh")})}, long[32:]...)...)
	reordered := store.List(section("short-row", reverse(short)), section("long-row", long))
	expected = embeddedValueSnapshotEntries(t, reordered)
	phase = "write"
	updated, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{"sections": reordered}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(observed) != len(expected) || len(writeIDs) != len(expected) || newKey == "" {
		t.Fatalf("write callbacks=%d identities=%d, want %d; fresh=%q", len(observed), len(writeIDs), len(expected), newKey)
	}
	if got := embeddedValueSnapshotEntries(t, updated.Values["sections"]); !reflect.DeepEqual(got, expected) {
		t.Fatalf("write response=%v, want %v", got, expected)
	}
	persisted := append([]embeddedValueSnapshotEntry(nil), expected...)
	phase, observed = "read", nil
	read, err := app.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(observed) != len(expected) {
		t.Fatalf("read callbacks=%d, want %d", len(observed), len(expected))
	}
	if got := embeddedValueSnapshotEntries(t, read.Values["sections"]); !reflect.DeepEqual(got, expected) {
		t.Fatalf("read response=%v, want %v", got, expected)
	}
	// Check all retained write and read versions after the entire second batch.
	for i, snapshot := range snapshots {
		if got := embeddedValueSnapshotEntries(t, snapshot.context.Root.Get("sections")); !reflect.DeepEqual(got, snapshot.root) {
			t.Fatalf("retained root %d changed after later callbacks", i)
		}
		if got, _ := snapshot.context.Siblings.String("title"); got != snapshot.sibling {
			t.Fatalf("retained siblings %d changed: %q, want %q", i, got, snapshot.sibling)
		}
		if got, _ := snapshot.context.Prior.String("title"); got != snapshot.prior {
			t.Fatalf("retained prior %d changed: %q, want %q", i, got, snapshot.prior)
		}
	}
	phase = ""
	stored, err := app.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := embeddedValueSnapshotEntries(t, stored.Values["sections"]); !reflect.DeepEqual(got, persisted) {
		t.Fatalf("read transformed persisted data: got %v, want %v", got, persisted)
	}
	if got := embeddedValueSnapshotEntries(t, created.Values["sections"]); !reflect.DeepEqual(got, embeddedValueSnapshotEntries(t, input)) {
		t.Fatal("later operations mutated the earlier create response or caller input")
	}
}

type embeddedValueSnapshotEntry struct {
	row, key, title string
}

func embeddedValueSnapshotEntries(t *testing.T, value store.Value) []embeddedValueSnapshotEntry {
	t.Helper()
	if value.Kind() != store.ValueList {
		t.Fatal("sections is not an array")
	}
	var entries []embeddedValueSnapshotEntry
	for row := range value.Elements() {
		key, _ := row.Get("_key").StringValue()
		for _, payload := range embeddedExecutionPayloads(t, row.Get("body")) {
			entries = append(entries, embeddedValueSnapshotEntry{key, embeddedString(payload, "uid"), embeddedString(payload, "title")})
		}
	}
	return entries
}
