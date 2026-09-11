package operation_test

import (
	"context"
	"testing"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestRawCarrierRetainsFiniteInputAndSubmissionPresence(t *testing.T) {
	if _, exists := operation.Empty[store.Value]().Get(); exists {
		t.Fatal("omitted raw input unexpectedly present")
	}
	explicitNull, exists := operation.Present(store.Null()).Get()
	if !exists || explicitNull.Kind() != store.ValueNull {
		t.Fatal("explicit null was confused with omitted raw input")
	}
	// A raw string-field transform can receive an object before typed admission.
	input := store.Values{"bad": store.Number(7)}
	raw := operation.Present(store.Object(input))
	input["bad"] = store.String("mutated")
	value, _ := raw.Get()
	detached, valid := value.CopyObject()
	if !valid {
		t.Fatal("raw carrier discarded a malformed logical string input")
	}
	if number, ok := detached["bad"].NumberValue(); !ok || number != 7 {
		t.Fatal("raw value aliases caller map")
	}
	detached["bad"] = store.Null()
	again, _ := value.CopyObject()
	if again["bad"].Kind() != store.ValueNumber {
		t.Fatal("raw value aliases returned map")
	}
}

func TestChangeDistinguishesKeepFromReplaceWithEmptyOrZero(t *testing.T) {
	if _, replace := operation.Keep[string]().Replacement(); replace {
		t.Fatal("keep was a replacement")
	}
	empty, replace := operation.Replace(operation.Empty[string]()).Replacement()
	if _, present := empty.Get(); !replace || present {
		t.Fatal("replacing with empty was confused with keep")
	}
	zero, replace := operation.Replace(operation.Present("")).Replacement()
	if value, present := zero.Get(); !replace || !present || value != "" {
		t.Fatal("explicit zero string was confused with empty or keep")
	}
}

func TestCallbackViewsDetachOwnedMaps(t *testing.T) {
	input := store.Values{"supplier": store.String("original")}
	view := operation.Snapshot(input)
	input["supplier"] = store.String("mutated")
	if value, ok := view.String("supplier"); !ok || value != "original" {
		t.Fatal("view aliases caller data")
	}
	if got := view.Get("absent").Kind(); got != store.ValueNull {
		t.Fatalf("absent snapshot value kind = %q; want null", got)
	}
}

func TestPopulatedReferenceHasDistinctLogicalTypeAndDetachedDocument(t *testing.T) {
	input := store.Document{
		ID:                  "user-1",
		Values:              store.Values{"name": store.String("Original")},
		LocalizationSources: map[string]schema.LocaleCode{"name": "en"},
	}
	output := operation.Populated(input)
	input.Values["name"] = store.String("Mutated")
	input.LocalizationSources["name"] = "fr"
	if output.ID() != operation.ID("user-1") {
		t.Fatal("populated output lost its typed reference ID")
	}
	document, populated := output.Document()
	if !populated {
		t.Fatal("document missing from populated output")
	}
	if name, _ := document.Values["name"].StringValue(); name != "Original" || document.LocalizationSources["name"] != "en" {
		t.Fatal("output aliases the supplied document")
	}
	document.Values["name"] = store.String("Returned mutation")
	document.LocalizationSources["name"] = "de"
	again, _ := output.Document()
	if name, _ := again.Values["name"].StringValue(); name != "Original" || again.LocalizationSources["name"] != "en" {
		t.Fatal("output aliases the returned document")
	}
	if _, populated := operation.Unpopulated("user-1").Document(); populated {
		t.Fatal("bare reference unexpectedly contains a document")
	}
}

func TestRelativeIssueUsesImmutableTarget(t *testing.T) {
	segments := []string{"label"}
	issue := operation.Issue{Code: "invalid_label", Message: "Choose a label.", Target: operation.At(segments...)}
	segments[0] = "mutated"
	returned := issue.Target.Segments()
	returned[0].Field = "changed"
	if issue.Target.Segments()[0].Field != "label" {
		t.Fatal("issue target aliases mutable segments")
	}
}

func TestPhaseContextsRetainDirectCancellationField(t *testing.T) {
	// Defining each named context over Context's struct avoids the selector
	// collision that embedding a field also named Context would introduce.
	ctx := operation.Context{Context: context.Background()}
	var cancellation context.Context = ctx.Context
	if cancellation == nil {
		t.Fatal("cancellation context missing")
	}
}
