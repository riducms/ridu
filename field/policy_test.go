package field

import (
	"errors"
	"reflect"
	"testing"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func TestPolicyHooksOwnEverySupportedPhaseAndComposeInOrder(t *testing.T) {
	var trace []string
	raw := func(label string) RawTransform {
		return func(operation.WriteContext, operation.Value[store.Value]) (operation.Change[store.Value], error) {
			trace = append(trace, label)
			return operation.Keep[store.Value](), nil
		}
	}
	write := func(label string) Transform[string] {
		return func(operation.WriteContext, operation.Value[string]) (operation.Change[string], error) {
			trace = append(trace, label)
			return operation.Keep[string](), nil
		}
	}
	event := func(label string) Observer[string] {
		return func(operation.EventContext, operation.Value[string]) error {
			trace = append(trace, label)
			return nil
		}
	}
	policy := func(label string) Hooks[string] {
		return Hooks[string]{
			BeforeDuplicate: []RawTransform{raw(label)}, BeforeValidate: []RawTransform{raw(label)},
			BeforeChange: []Transform[string]{write(label)}, BeforeOperation: []Transform[string]{write(label)},
			BeforeDelete: []Observer[string]{event(label)}, AfterChange: []Observer[string]{event(label)},
			AfterDelete: []Observer[string]{event(label)}, AfterOperation: []Observer[string]{event(label)},
			AfterCommit: []Observer[string]{event(label)},
		}
	}
	borrowed := policy("base")
	base := cloneHooks(borrowed)
	composed := prependHooks(appendHooks(base, policy("last")), policy("first"))
	borrowed.BeforeDuplicate[0] = raw("mutated")
	borrowed.BeforeValidate[0] = raw("mutated")
	borrowed.BeforeChange[0] = write("mutated")
	borrowed.BeforeOperation[0] = write("mutated")
	for _, phase := range [][]Observer[string]{borrowed.BeforeDelete, borrowed.AfterChange, borrowed.AfterDelete, borrowed.AfterOperation, borrowed.AfterCommit} {
		phase[0] = event("mutated")
	}
	assertTrace := func(t *testing.T) {
		t.Helper()
		if !reflect.DeepEqual(trace, []string{"first", "base", "last"}) {
			t.Fatalf("phase ordering or ownership changed: %v", trace)
		}
		trace = nil
	}
	for _, phase := range [][]RawTransform{composed.BeforeDuplicate, composed.BeforeValidate} {
		for _, callback := range phase {
			if _, err := callback(operation.WriteContext{}, operation.Empty[store.Value]()); err != nil {
				t.Fatal(err)
			}
		}
		assertTrace(t)
	}
	for _, phase := range [][]Transform[string]{composed.BeforeChange, composed.BeforeOperation} {
		for _, callback := range phase {
			if _, err := callback(operation.WriteContext{}, operation.Empty[string]()); err != nil {
				t.Fatal(err)
			}
		}
		assertTrace(t)
	}
	for _, phase := range [][]Observer[string]{composed.BeforeDelete, composed.AfterChange, composed.AfterDelete, composed.AfterOperation, composed.AfterCommit} {
		for _, callback := range phase {
			if err := callback(operation.EventContext{}, operation.Empty[string]()); err != nil {
				t.Fatal(err)
			}
		}
		assertTrace(t)
	}
	if len(base.BeforeChange) != 1 || len(base.AfterCommit) != 1 {
		t.Fatal("composition mutated the base lists")
	}
}

func TestAdminPolicyOwnsBorrowedAndEscapedDraftMaps(t *testing.T) {
	labels := map[string]string{"en": "Original"}
	extensions := map[string]store.Value{"example.color": store.String("blue")}
	base := cloneAdmin(Admin{LabelTranslations: labels, Extensions: extensions, Editor: Component("app:Text", store.Object(store.Values{"enabled": store.Boolean(true)}))})
	labels["en"] = "Changed input"
	extensions["example.color"] = store.String("red")
	var escaped *Admin
	var draftLabels map[string]string
	edited := editAdmin(base, func(draft *Admin) {
		escaped = draft
		draft.Description = "Edited"
		draft.LabelTranslations["en"] = "Edited label"
		draftLabels = draft.LabelTranslations
	})
	escaped.Description = "Escaped"
	draftLabels["en"] = "Escaped label"
	escaped.Extensions["example.color"] = store.Null()
	if base.LabelTranslations["en"] != "Original" || base.Description != "" {
		t.Fatal("input or edit mutated base")
	}
	if edited.Description != "Edited" || edited.LabelTranslations["en"] != "Edited label" {
		t.Fatal("escaped draft mutated committed admin")
	}
	if color, _ := edited.Extensions["example.color"].StringValue(); color != "blue" {
		t.Fatal("extension was dropped or aliased")
	}
	props, _ := edited.Editor.Config.CopyObject()
	props["enabled"] = store.Boolean(false)
	props, _ = edited.Editor.Config.CopyObject()
	if enabled, _ := props["enabled"].BooleanValue(); !enabled {
		t.Fatal("component config escaped immutable ownership")
	}
}

func TestAccessRestrictionPreservesOtherOperationsAndShortCircuits(t *testing.T) {
	failure := errors.New("lookup failed")
	called := 0
	allow := AccessRule(func(operation.AccessContext) (bool, error) { return true, nil })
	deny := AccessRule(func(operation.AccessContext) (bool, error) { return false, nil })
	extra := AccessRule(func(operation.AccessContext) (bool, error) { called++; return true, nil })
	base := Access{Create: allow, Read: allow, Update: deny}
	restricted := restrictAccess(base, Access{Update: extra})
	if allowed, err := restricted.Create(operation.AccessContext{}); !allowed || err != nil {
		t.Fatal("update restriction changed create")
	}
	if allowed, err := restricted.Read(operation.AccessContext{}); !allowed || err != nil {
		t.Fatal("update restriction changed read")
	}
	if allowed, err := restricted.Update(operation.AccessContext{}); allowed || err != nil || called != 0 {
		t.Fatal("denial did not short circuit")
	}
	restricted = restrictAccess(Access{Update: func(operation.AccessContext) (bool, error) { return false, failure }}, Access{Update: extra})
	if _, err := restricted.Update(operation.AccessContext{}); !errors.Is(err, failure) || called != 0 {
		t.Fatal("operational error did not short circuit")
	}
	restricted = restrictAccess(Access{}, Access{Update: extra})
	if allowed, err := restricted.Update(operation.AccessContext{}); !allowed || err != nil || called != 1 {
		t.Fatal("restriction without previous policy was discarded")
	}
}
