package core

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/field"
	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func graphRuntimeBindings(t *testing.T, nodes field.Fields) []operationengine.FieldBinding {
	t.Helper()
	config, manifest, err := resolveConfig(Config{Name: "Graph adapter", Collections: []Collection{{Slug: "pages", Fields: nodes}}})
	if err != nil {
		t.Fatal(err)
	}
	var local *LocalAPI
	bindings, err := lowerFieldGraph(config.fieldGraph, "collection", "pages", manifest.Snapshot().Collections[0].Fields, &local)
	if err != nil {
		t.Fatal(err)
	}
	return bindings
}

func TestGraphRuntimeLoweringKeepsPlacementAndScopedSnapshots(t *testing.T) {
	var contexts []operation.Context
	code := field.Text("code").Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(ctx operation.Context, value operation.Value[string]) (operation.Change[string], error) {
		contexts = append(contexts, ctx)
		text, _ := value.Get()
		return operation.Replace(operation.Present("own:" + text)), nil
	}}})
	bindings := graphRuntimeBindings(t, field.Fields{code, field.Group("variant", field.Fields{code})})
	if len(bindings) != 2 || bindings[0].ID == bindings[1].ID {
		t.Fatalf("independent placements were not bound: %+v", bindings)
	}
	root := store.Values{"code": store.String("root"), "other": store.String("untouched")}
	siblings := store.Values{"code": store.String("nested"), "other": store.String("sibling")}
	prior := store.Values{"code": store.String("before")}
	ctx := operationengine.Context{Context: t.Context(), Value: siblings["code"], ValuePresent: true, Data: root, SiblingData: siblings, OriginalSiblingData: prior, OccurrenceID: bindings[1].ID + "/row-A", RuntimePath: "variant.code"}
	if err := bindings[1].Hooks.BeforeChange[0](ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := siblings["code"].StringValue(); got != "own:nested" {
		t.Fatalf("scoped replacement = %q", got)
	}
	if got, _ := root["code"].StringValue(); got != "root" {
		t.Fatalf("nested replacement mutated another occurrence: %q", got)
	}
	root["other"], siblings["other"], prior["code"] = store.Null(), store.Null(), store.Null()
	seen := contexts[0]
	if seen.OccurrenceID != operation.OccurrenceID(ctx.OccurrenceID) || seen.SchemaOccurrenceID != operation.OccurrenceID(bindings[1].ID) {
		t.Fatalf("callback identity = %+v", seen)
	}
	if value, _ := seen.Root.String("other"); value != "untouched" {
		t.Fatal("root view retained mutable operation map")
	}
	if value, _ := seen.Siblings.String("other"); value != "sibling" {
		t.Fatal("sibling view retained mutable operation map")
	}
	if value, _ := seen.Prior.String("code"); value != "before" {
		t.Fatal("prior view retained mutable operation map")
	}
}

func TestGraphRuntimeTypedGuardPrecedesEveryCallback(t *testing.T) {
	calls := 0
	text := field.Text("code").Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(operation.Context, operation.Value[string]) (operation.Change[string], error) {
		calls++
		return operation.Keep[string](), nil
	}}, AfterChange: []field.Observer[string]{func(operation.Context, operation.Value[string]) error { calls++; return nil }}}).
				AfterRead(func(operation.Context, operation.Value[string]) (operation.Change[string], error) {
			calls++
			return operation.Keep[string](), nil
		}).Validate(func(operation.Context, operation.Value[string]) ([]operation.Issue, error) {
		calls++
		return nil, nil
	})
	binding := graphRuntimeBindings(t, field.Fields{text})[0]
	ctx := operationengine.Context{Context: t.Context(), Value: store.Number(123), ValuePresent: true, SiblingData: store.Values{"code": store.Number(123)}, RuntimePath: "variants.0.code"}
	for _, callback := range []operationengine.Hook{binding.Hooks.BeforeChange[0], binding.Hooks.AfterChange[0], binding.Hooks.AfterRead[0]} {
		err := callback(ctx)
		if err == nil || !strings.Contains(err.Error(), "invalid_type") || !strings.Contains(err.Error(), ctx.RuntimePath) {
			t.Fatalf("unsafe callback guard = %v", err)
		}
	}
	if _, err := binding.Validators[0](ctx); err == nil || !strings.Contains(err.Error(), "invalid_type") {
		t.Fatalf("unsafe validator guard = %v", err)
	}
	if calls != 0 {
		t.Fatalf("%d typed callbacks received malformed value", calls)
	}
	ctx.Value = store.Null()
	if err := binding.Hooks.BeforeChange[0](ctx); err != nil || calls != 1 {
		t.Fatalf("portable empty typed value rejected: calls=%d error=%v", calls, err)
	}
}

func TestGraphRuntimeRawPresenceAndRelativeValidationIssues(t *testing.T) {
	var present []bool
	failure := errors.New("validator service unavailable")
	operational := false
	code := field.Text("code").Hooks(field.Hooks[string]{BeforeValidate: []field.RawTransform{func(_ operation.Context, value operation.Value[store.Value]) (operation.Change[store.Value], error) {
		_, exists := value.Get()
		present = append(present, exists)
		return operation.Keep[store.Value](), nil
	}}}).Validate(func(operation.Context, operation.Value[string]) ([]operation.Issue, error) {
		if operational {
			return nil, failure
		}
		return []operation.Issue{{Code: "custom", Message: "invalid code"}}, nil
	})
	binding := graphRuntimeBindings(t, field.Fields{code})[0]
	ctx := operationengine.Context{Context: t.Context(), SiblingData: store.Values{}, RuntimePath: "rows.1.code"}
	if err := binding.Hooks.BeforeValidate[0](ctx); err != nil {
		t.Fatal(err)
	}
	ctx.Value, ctx.ValuePresent = store.Null(), true
	if err := binding.Hooks.BeforeValidate[0](ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(present, []bool{false, true}) {
		t.Fatalf("raw omission/null presence = %v", present)
	}
	ctx.Value = store.String("final")
	issues, err := binding.Validators[0](ctx)
	if err != nil || len(issues) != 1 || issues[0].Path != "rows.1.code" {
		t.Fatalf("bound relative issues = %+v, %v", issues, err)
	}
	operational = true
	issues, err = binding.Validators[0](ctx)
	var validation *schema.ValidationError
	if !errors.Is(err, failure) || errors.As(err, &validation) || issues != nil {
		t.Fatalf("operational error became validation: issues=%+v error=%v", issues, err)
	}
}

func TestGraphRuntimeContainerAndNumberTypedAdmission(t *testing.T) {
	for _, test := range []struct {
		name string
		node func(*int) field.Node
		bad  store.Value
		code string
	}{
		{"number", func(calls *int) field.Node {
			return field.Number("value").Hooks(field.Hooks[float64]{BeforeChange: []field.Transform[float64]{func(operation.Context, operation.Value[float64]) (operation.Change[float64], error) {
				*calls++
				return operation.Keep[float64](), nil
			}}})
		}, store.Number(math.NaN()), "invalid_number"},
		{"group", func(calls *int) field.Node {
			return field.Group("value", field.Fields{field.Text("child")}).Hooks(field.Hooks[store.Value]{BeforeChange: []field.Transform[store.Value]{countFiniteTransform(calls)}})
		}, store.List(), "invalid_type"},
		{"array", func(calls *int) field.Node {
			return field.Array("value", field.Fields{field.Text("child")}).Hooks(field.Hooks[store.Value]{BeforeChange: []field.Transform[store.Value]{countFiniteTransform(calls)}})
		}, store.Object(store.Values{}), "invalid_type"},
		{"blocks", func(calls *int) field.Node {
			return field.Blocks("value", field.Block{Slug: "card", Fields: field.Fields{field.Text("child")}}).Hooks(field.Hooks[store.Value]{BeforeChange: []field.Transform[store.Value]{countFiniteTransform(calls)}})
		}, store.Object(store.Values{}), "invalid_type"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			binding := graphRuntimeBindings(t, field.Fields{test.node(&calls)})[0]
			ctx := operationengine.Context{Context: t.Context(), Value: test.bad, ValuePresent: true, SiblingData: store.Values{"value": test.bad}, RuntimePath: "value"}
			if err := binding.Hooks.BeforeChange[0](ctx); err == nil || !strings.Contains(err.Error(), test.code) || calls != 0 {
				t.Fatalf("unsafe %s admission: calls=%d error=%v", test.name, calls, err)
			}
		})
	}
}

func countFiniteTransform(calls *int) field.Transform[store.Value] {
	return func(operation.Context, operation.Value[store.Value]) (operation.Change[store.Value], error) {
		*calls++
		return operation.Keep[store.Value](), nil
	}
}

func TestGraphRuntimeReaderKeepsTransactionActorAndAuthorization(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(fmt.Sprintf("denied=%t", denied), func(t *testing.T) {
			denyRead := denied
			reads := 0
			actor := &store.Document{ID: "caller", Values: store.Values{"role": store.String("editor")}}
			config := Config{Name: "Bound reader", Collections: []Collection{{
				Slug: "suppliers", Fields: field.Fields{field.Text("code")},
				Access: CollectionAccess{Read: func(ctx AccessContext) (AccessDecision, error) {
					reads++
					if ctx.Actor == nil || ctx.Actor.ID != actor.ID {
						t.Fatalf("bound reader lost actor: %+v", ctx.Actor)
					}
					if denyRead {
						return Deny(), nil
					}
					return Allow(), nil
				}},
			}, {
				Slug: "pages", Fields: field.Fields{field.Text("supplier")}, Hooks: CollectionHooks{BeforeChange: []Hook{func(ctx HookContext) error {
					supplier, err := ctx.Local.Create(ctx.Context, "suppliers", store.Values{"code": store.String("uncommitted")}, MutationOptions{Actor: ctx.Actor})
					if err != nil {
						return err
					}
					local := ctx.Local
					reader := graphReader{local: &local, context: ctx.Context, actor: cloneDocument(ctx.Actor), actorCollection: ctx.ActorCollection, locale: ctx.Locale}
					// A caller-supplied Background must not detach the transaction.
					found, err := reader.FindByID(context.Background(), "suppliers", operation.ID(supplier.ID))
					if err != nil {
						return err
					}
					if got, _ := found.Values["code"].StringValue(); got != "uncommitted" {
						t.Fatalf("bound reader lost uncommitted state: %q", got)
					}
					ctx.Data["supplier"] = store.String(supplier.ID)
					return nil
				}}},
			}}}
			app, err := New(config, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			_, err = app.Local().Create(t.Context(), "pages", store.Values{}, MutationOptions{Actor: actor})
			if denied != (err != nil) || reads != 1 {
				t.Fatalf("reader access: denied=%t calls=%d error=%v", denied, reads, err)
			}
			denyRead = false
			page, err := app.Local().List(t.Context(), "suppliers", ListOptions{Actor: actor})
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if denied {
				want = 0
			}
			if len(page.Documents) != want {
				t.Fatalf("transaction documents=%d want=%d", len(page.Documents), want)
			}
		})
	}
}

func TestGraphRuntimeParentTransformAssignsIdentityBeforeChildDispatch(t *testing.T) {
	var beforeKey, afterKey string
	var beforeID, afterID operation.OccurrenceID
	code := field.Text("code").Hooks(field.Hooks[string]{
		BeforeChange: []field.Transform[string]{func(ctx operation.Context, value operation.Value[string]) (operation.Change[string], error) {
			if text, _ := value.Get(); ctx.Operation == operation.Update && text == "new" {
				beforeKey, _ = ctx.Siblings.String("_key")
				beforeID = ctx.OccurrenceID
			}
			return operation.Keep[string](), nil
		}},
		AfterChange: []field.Observer[string]{func(ctx operation.Context, value operation.Value[string]) error {
			if text, _ := value.Get(); ctx.Operation == operation.Update && text == "new" {
				afterKey, _ = ctx.Siblings.String("_key")
				afterID = ctx.OccurrenceID
			}
			return nil
		}},
	})
	rows := field.Array("rows", field.Fields{code}).Hooks(field.Hooks[store.Value]{BeforeChange: []field.Transform[store.Value]{func(ctx operation.Context, value operation.Value[store.Value]) (operation.Change[store.Value], error) {
		if ctx.Operation != operation.Update {
			return operation.Keep[store.Value](), nil
		}
		current, _ := value.Get()
		items, _ := current.CopyList()
		items = append(items, store.Object(store.Values{"code": store.String("new")}))
		return operation.Replace(operation.Present(store.List(items...))), nil
	}}})
	app, err := New(Config{Name: "Parent row identity", Collections: []Collection{{Slug: "pages", Fields: field.Fields{field.Text("trigger"), rows}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"rows": store.List(store.Object(store.Values{"_key": store.String("A"), "code": store.String("old")}))}, MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{"trigger": store.String("add")}, MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	items, _ := updated.Values["rows"].CopyList()
	if len(items) != 2 {
		t.Fatalf("transformed rows=%d", len(items))
	}
	last, _ := items[1].CopyObject()
	storedKey, _ := last["_key"].StringValue()
	if beforeKey == "" || beforeKey != storedKey || afterKey != storedKey || beforeID != afterID {
		t.Fatalf("new row identity changed across callbacks/storage: before=(%q,%q) after=(%q,%q) stored=%q", beforeKey, beforeID, afterKey, afterID, storedKey)
	}
}

func TestGraphRuntimeReaderUsesExactBoundLocaleAndCancellation(t *testing.T) {
	reads := 0
	app, err := New(Config{
		Name: "Exact reader", Localization: LocalizationConfig{DefaultLocale: "en", Locales: []Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}}}},
		Collections: []Collection{{Slug: "suppliers", Fields: field.Fields{field.Text("code").Localized()}, Access: CollectionAccess{Read: func(ctx AccessContext) (AccessDecision, error) {
			reads++
			if ctx.Locale != "fr" {
				t.Fatalf("reader locale=%q", ctx.Locale)
			}
			return Allow(), nil
		}}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	supplier, err := app.Local().Create(t.Context(), "suppliers", store.Values{"code": store.String("English only")}, MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	local := app.Local()
	reader := graphReader{local: &local, context: t.Context(), locale: "fr"}
	found, err := reader.FindByID(context.Background(), "suppliers", operation.ID(supplier.ID))
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := found.Values["code"].StringValue(); value != "" || reads != 1 {
		t.Fatalf("reader manufactured target-locale input from fallback: %q, reads=%d", value, reads)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = reader.FindByID(canceled, "suppliers", operation.ID(supplier.ID))
	if !errors.Is(err, context.Canceled) || reads != 1 {
		t.Fatalf("canceled reader dispatched: reads=%d error=%v", reads, err)
	}
}
