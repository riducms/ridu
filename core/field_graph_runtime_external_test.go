package core_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	richtextblocks "github.com/riducms/ridu/tests/contracts/richtext_blocks"
)

type graphRuntimeObservation struct {
	phase, key, kind, value, prior string
	id                             operation.OccurrenceID
	locale                         schema.LocaleCode
}

func graphRuntimeCode(t *testing.T, observations *[]graphRuntimeObservation) field.TextField {
	t.Helper()
	observe := func(phase string, ctx operation.Context, value string) {
		root, _ := ctx.Root.String("tenant")
		if phase != "raw" && root != "tenant-one" {
			t.Errorf("%s root tenant = %q", phase, root)
		}
		kind, _ := ctx.Siblings.String("kind")
		if phase != "raw" && kind == "" {
			t.Errorf("%s lost nearest sibling scope", phase)
		}
		key, _ := ctx.Siblings.String("_key")
		prior, ok := ctx.Prior.String("code")
		if !ok {
			prior = "<new>"
		}
		*observations = append(*observations, graphRuntimeObservation{phase: phase, key: key, kind: kind, value: value, prior: prior, id: ctx.OccurrenceID, locale: ctx.Locale})
	}
	return field.Text("code").Required().Hooks(field.Hooks[string]{
		BeforeValidate: []field.RawTransform{func(ctx operation.Context, input operation.Value[store.Value]) (operation.Change[store.Value], error) {
			value, present := input.Get()
			if !present {
				return operation.Keep[store.Value](), nil
			}
			text, ok := value.StringValue()
			if !ok {
				return operation.Keep[store.Value](), nil
			}
			observe("raw", ctx, text)
			return operation.Replace(operation.Present(store.String(strings.ToLower(strings.TrimSpace(text))))), nil
		}},
		BeforeChange: []field.Transform[string]{func(ctx operation.Context, input operation.Value[string]) (operation.Change[string], error) {
			value, _ := input.Get()
			observe("write", ctx, value)
			return operation.Replace(operation.Present("code:" + strings.TrimPrefix(value, "code:"))), nil
		}},
		AfterChange: []field.Observer[string]{func(ctx operation.Context, input operation.Value[string]) error {
			value, _ := input.Get()
			observe("changed", ctx, value)
			return nil
		}},
	}).ReplaceAfterRead(func(ctx operation.Context, input operation.Value[string]) (operation.Change[string], error) {
		value, _ := input.Get()
		observe("read", ctx, value)
		return operation.Keep[string](), nil
	}).
		Validate(func(ctx operation.Context, input operation.Value[string]) ([]operation.Issue, error) {
			value, _ := input.Get()
			observe("validate", ctx, value)
			if !strings.HasPrefix(value, "code:") {
				return []operation.Issue{{Code: "code_prefix", Message: "Code must have its final prefix."}}, nil
			}
			return nil, nil
		}).
		Access(field.Access{Update: func(ctx operation.Context) (bool, error) {
			value, _ := ctx.Siblings.String("code")
			observe("access", ctx, value)
			return true, nil
		}})
}
func graphRuntimeRow(key, kind, code string) store.Value {
	values := store.Values{"kind": store.String(kind), "code": store.String(code)}
	if key != "" {
		values["_key"] = store.String(key)
	}
	return store.Object(values)
}
func graphRuntimeBlock(key, blockType, kind, code string) store.Value {
	values, _ := graphRuntimeRow(key, kind, code).CopyObject()
	values["blockType"] = store.String(blockType)
	return store.Object(values)
}
func graphRuntimePhase(observed []graphRuntimeObservation, phase string) []graphRuntimeObservation {
	var result []graphRuntimeObservation
	for _, o := range observed {
		if o.phase == phase {
			result = append(result, o)
		}
	}
	return result
}

func TestFieldGraphRuntimeReusedRootGroupArrayAndBlocks(t *testing.T) {
	var observed []graphRuntimeObservation
	code := graphRuntimeCode(t, &observed)
	children := field.Fields{field.Text("kind"), code}
	app, err := ridu.New(ridu.Config{Name: "Graph runtime occurrences", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
		field.Text("tenant"), field.Text("kind"), code, field.Group("meta", children), field.Array("variants", children),
		field.Blocks("layout", field.Block{Slug: "hero", Fields: children}, field.Block{Slug: "note", Fields: children}),
	}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{
		"tenant": store.String("tenant-one"), "kind": store.String("root"), "code": store.String(" ROOT "),
		"meta":     store.Object(store.Values{"kind": store.String("group"), "code": store.String(" GROUP ")}),
		"variants": store.List(graphRuntimeRow("A", "array-a", " ONE "), graphRuntimeRow("B", "array-b", " TWO ")),
		"layout":   store.List(graphRuntimeBlock("H", "hero", "hero", " HERO "), graphRuntimeBlock("N", "note", "note", " NOTE ")),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	initial := map[string]graphRuntimeObservation{}
	ids := map[operation.OccurrenceID]bool{}
	for _, phase := range []string{"raw", "write", "validate", "changed", "read"} {
		calls := graphRuntimePhase(observed, phase)
		if len(calls) != 6 {
			t.Fatalf("%s calls = %#v; want six occurrences", phase, calls)
		}
		for _, call := range calls {
			if call.id == "" || call.prior != "<new>" {
				t.Fatalf("create identity/prior = %#v", call)
			}
			if phase == "validate" {
				initial[call.kind] = call
			}
		}
	}
	for _, call := range initial {
		if ids[call.id] {
			t.Fatalf("reused placements share concrete identity %s", call.id)
		}
		ids[call.id] = true
	}
	observed = nil
	_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{
		"code": store.String(" ROOT "), "meta": store.Object(store.Values{"code": store.String(" GROUP ")}),
		"variants": store.List(graphRuntimeRow("B", "array-b", " TWO "), graphRuntimeRow("A", "array-a", " ONE "), graphRuntimeRow("C", "array-c", " THREE ")),
		"layout":   store.List(graphRuntimeBlock("N", "note", "note", " NOTE "), graphRuntimeBlock("H", "hero", "hero", " HERO ")),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"write", "validate", "changed", "read"} {
		calls := graphRuntimePhase(observed, phase)
		if len(calls) != 7 {
			t.Fatalf("reorder %s calls = %#v", phase, calls)
		}
		for _, call := range calls {
			old, retained := initial[call.kind]
			if retained && (call.id != old.id || call.prior != old.value) {
				t.Errorf("reorder %s lost identity/prior: %#v; prior %#v", phase, call, old)
			}
			if !retained && (call.kind != "array-c" || call.prior != "<new>" || ids[call.id]) {
				t.Errorf("new row fabricated prior: %#v", call)
			}
		}
	}
	if len(graphRuntimePhase(observed, "access")) < 7 {
		t.Fatal("attached update access missed occurrences")
	}
	observed = nil
	_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{"layout": store.List(graphRuntimeBlock("H", "note", "invalid-case-reuse", "FRESH"))}, ridu.MutationOptions{})
	graphRuntimeIssue(t, err, "layout.0.blockType")
	if len(observed) != 0 {
		t.Fatal("invalid case identity reached attached callbacks")
	}
	// Existing semantics require a fresh key when replacing the block case.
	updated, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{
		"variants": store.List(graphRuntimeRow("B", "array-b", " TWO ")),
		"layout":   store.List(graphRuntimeBlock("replacement", "note", "replacement", " FRESH ")),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatalf("replacement: %#v", err)
	}
	rows, _ := updated.Values["layout"].CopyList()
	replacement, _ := rows[0].CopyObject()
	if stringValue(replacement["_key"]) == "H" {
		t.Fatal("block case replacement reused old key")
	}
	for _, phase := range []string{"write", "validate", "changed", "read"} {
		calls := graphRuntimePhase(observed, phase)
		if len(calls) != 4 {
			t.Fatalf("removal %s calls = %#v", phase, calls)
		}
		for _, call := range calls {
			if call.kind == "array-a" || call.kind == "array-c" || call.kind == "hero" || call.kind == "note" {
				t.Errorf("removed occurrence dispatched: %#v", call)
			}
			if call.kind == "replacement" && (call.prior != "<new>" || ids[call.id]) {
				t.Errorf("replacement inherited prior: %#v", call)
			}
		}
	}
}

func TestFieldGraphRuntimeTypedChildGuardAfterParentOrResourceTransform(t *testing.T) {
	for _, owner := range []string{"parent", "resource"} {
		t.Run(owner, func(t *testing.T) {
			calls := 0
			child := field.Text("code").Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(operation.Context, operation.Value[string]) (operation.Change[string], error) {
				calls++
				return operation.Keep[string](), nil
			}}})
			malformed := store.Object(store.Values{"code": store.Number(123)})
			parent := field.Group("variant", field.Fields{child})
			var hooks ridu.CollectionHooks
			if owner == "parent" {
				parent = parent.Hooks(field.Hooks[store.Value]{BeforeChange: []field.Transform[store.Value]{func(operation.Context, operation.Value[store.Value]) (operation.Change[store.Value], error) {
					return operation.Replace(operation.Present(malformed)), nil
				}}})
			} else {
				hooks.BeforeChange = []ridu.Hook{func(ctx ridu.HookContext) error { ctx.Data["variant"] = malformed; return nil }}
			}
			app, err := ridu.New(ridu.Config{Name: "Typed child safety", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{parent}, Hooks: hooks}}}, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			_, err = app.Local().Create(t.Context(), "pages", store.Values{"variant": store.Object(store.Values{"code": store.String("valid")})}, ridu.MutationOptions{})
			graphRuntimeIssue(t, err, "variant.code")
			if calls != 0 {
				t.Fatalf("unsafe typed callback ran %d times", calls)
			}
		})
	}
}

func TestFieldGraphRuntimeCustomValidationUsesFinalCandidateOnce(t *testing.T) {
	calls := 0
	operational := errors.New("validator lookup unavailable")
	mode := ""
	code := field.Text("code").Required().MaxLength(16).Validate(func(ctx operation.Context, input operation.Value[string]) ([]operation.Issue, error) {
		calls++
		value, _ := input.Get()
		source, _ := ctx.Siblings.String("source")
		if source != "FINAL" || value != "sku" {
			t.Errorf("validator saw nonfinal candidate: source=%q code=%q", source, value)
		}
		if mode == "operational" {
			return nil, operational
		}
		if mode == "issue" {
			return []operation.Issue{{Code: "sku_reserved", Message: "This SKU is reserved."}}, nil
		}
		return nil, nil
	})
	source := field.Text("source").Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(operation.Context, operation.Value[string]) (operation.Change[string], error) {
		return operation.Replace(operation.Present("FINAL")), nil
	}}})
	app, err := ridu.New(ridu.Config{Name: "Final application validation", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{code, source}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"code": store.String("sku"), "source": store.String("initial")}, ridu.MutationOptions{})
	if err != nil || calls != 1 {
		t.Fatalf("create count=%d: %v", calls, err)
	}
	calls = 0
	_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{"source": store.String("changed")}, ridu.MutationOptions{})
	if err != nil || calls != 1 {
		t.Fatalf("retained field count=%d: %v", calls, err)
	}
	for _, failure := range []string{"issue", "operational"} {
		mode, calls = failure, 0
		_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{"source": store.String("changed")}, ridu.MutationOptions{})
		if calls != 1 {
			t.Fatalf("%s count=%d", mode, calls)
		}
		if mode == "operational" {
			if !errors.Is(err, operational) {
				t.Fatalf("operational error flattened: %v", err)
			}
		} else {
			graphRuntimeIssue(t, err, "code")
		}
	}
	mode = ""
	for _, bad := range []store.Value{store.Null(), store.Number(5), store.String(strings.Repeat("x", 17))} {
		calls = 0
		_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{"code": bad}, ridu.MutationOptions{})
		graphRuntimeIssue(t, err, "code")
		if calls != 0 {
			t.Fatalf("custom validators replaced builtins for %v", bad)
		}
	}
}
func graphRuntimeIssue(t *testing.T, err error, path string) {
	t.Helper()
	var failure *ridu.OperationError
	if !errors.As(err, &failure) {
		t.Fatalf("expected operation issue at %s, got %T: %v", path, err, err)
	}
	for _, issue := range failure.Issues {
		if issue.Path == path {
			return
		}
	}
	t.Fatalf("expected issue at %s: %#v", path, failure.Issues)
}

func TestFieldGraphRuntimeLocalizationExactWritesAndAllLocalesReadCounts(t *testing.T) {
	var writes, validations, reads []graphRuntimeObservation
	makeText := func(name string) field.TextField {
		record := func(dst *[]graphRuntimeObservation, ctx operation.Context, input operation.Value[string]) {
			key, _ := ctx.Siblings.String("_key")
			value, _ := input.Get()
			prior, ok := ctx.Prior.String(name)
			if !ok {
				prior = "<absent>"
			}
			*dst = append(*dst, graphRuntimeObservation{kind: name, key: key, value: value, prior: prior, id: ctx.OccurrenceID, locale: ctx.Locale})
		}
		return field.Text(name).Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(ctx operation.Context, input operation.Value[string]) (operation.Change[string], error) {
			record(&writes, ctx, input)
			return operation.Keep[string](), nil
		}}}).Validate(func(ctx operation.Context, input operation.Value[string]) ([]operation.Issue, error) {
			record(&validations, ctx, input)
			return nil, nil
		}).ReplaceAfterRead(func(ctx operation.Context, input operation.Value[string]) (operation.Change[string], error) {
			record(&reads, ctx, input)
			return operation.Keep[string](), nil
		})
	}
	app, err := ridu.New(ridu.Config{Name: "Graph exact locales", Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}}}}, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
		field.Array("sections", field.Fields{makeText("key"), makeText("heading").Localized()}), field.Array("translations", field.Fields{makeText("label")}).Localized(),
	}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	row := func(key, heading string) store.Value {
		return store.Object(store.Values{"_key": store.String(key), "key": store.String(key), "heading": store.String(heading)})
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"sections": store.List(row("A", "Hello"), row("B", "Goodbye")), "translations": store.List(store.Object(store.Values{"_key": store.String("EN"), "label": store.String("English")}))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	englishIDs := map[string]operation.OccurrenceID{}
	for _, call := range validations {
		englishIDs[call.kind+call.key] = call.id
	}
	writes, validations, reads = nil, nil, nil
	_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{"sections": store.List(row("B", "Au revoir"), row("A", "Bonjour")), "translations": store.List(store.Object(store.Values{"_key": store.String("FR"), "label": store.String("Français")}))}, ridu.MutationOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	for _, calls := range [][]graphRuntimeObservation{writes, validations} {
		if len(calls) != 5 {
			t.Fatalf("exact locale count: %#v", calls)
		}
		for _, call := range calls {
			if call.locale != "fr" {
				t.Errorf("write dispatched other locale: %#v", call)
			}
			if call.kind == "heading" && (call.prior != "<absent>" || call.id == englishIDs[call.kind+call.key]) {
				t.Errorf("fallback became exact prior/identity: %#v", call)
			}
			if call.kind == "label" && (call.key != "FR" || call.prior != "<absent>") {
				t.Errorf("localized container reused other locale: %#v", call)
			}
		}
	}
	reads = nil
	all, err := app.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	shared, headings, labels := 0, 0, 0
	for _, call := range reads {
		switch call.kind {
		case "key":
			shared++
		case "heading":
			headings++
		case "label":
			labels++
		}
	}
	if shared != 2 || headings != 4 || labels != 2 {
		t.Fatalf("all locales counts shared=%d heading=%d labels=%d: %#v", shared, headings, labels, reads)
	}
	rows, _ := all.Values["sections"].CopyList()
	first, _ := rows[0].CopyObject()
	heading, _ := first["heading"].CopyObject()
	if stringValue(first["_key"]) != "B" || stringValue(heading["en"]) != "Goodbye" || stringValue(heading["fr"]) != "Au revoir" {
		t.Fatalf("localized reorder lost values: %#v", first)
	}
	translations, _ := all.Values["translations"].CopyObject()
	if len(blockRows(translations["en"])) != 1 || len(blockRows(translations["fr"])) != 1 {
		t.Fatalf("localized container lost values: %#v", translations)
	}
	writes, validations = nil, nil
	_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{"sections": store.List(row("A", "Bonsoir"), row("B", "Salut"))}, ridu.MutationOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range validations {
		if call.kind == "heading" {
			expected := map[string]string{"A": "Bonjour", "B": "Au revoir"}[call.key]
			if call.prior != expected {
				t.Errorf("locale prior matched index: %#v", call)
			}
		}
	}
}

func TestFieldGraphRuntimeRichTextEmbeddedReuse(t *testing.T) {
	var observations []graphRuntimeObservation
	code := graphRuntimeCode(t, &observations)
	secret := field.Text("secret").Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }})
	block := field.Block{Slug: "card", Fields: field.Fields{field.Text("kind"), code, secret}}
	// The official embedded host carries the same attached field nodes.
	// Its ordinary children carry the same graph policies without owner maps.
	body := richtext.Field("body", richtext.Config{Blocks: []field.Block{block}})
	app, err := ridu.New(ridu.Config{Name: "Graph rich text", Plugins: []ridu.Plugin{richtext.New()}, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Text("tenant"), field.Text("kind"), code, body}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	payload := func(kind, code string) store.Values {
		return store.Values{"kind": store.String(kind), "code": store.String(code), "secret": store.String("private")}
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"tenant": store.String("tenant-one"), "kind": store.String("root"), "code": store.String(" ROOT "), "body": richtextblocks.Document(richtextblocks.Block("card", "A", payload("embedded-a", " ONE ")), richtextblocks.Block("card", "B", payload("embedded-b", " TWO ")))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	initial := map[string]graphRuntimeObservation{}
	for _, phase := range []string{"raw", "write", "validate", "changed", "read"} {
		calls := graphRuntimePhase(observations, phase)
		if len(calls) != 3 {
			t.Fatalf("embedded %s count: %#v", phase, calls)
		}
		if phase == "validate" {
			for _, call := range calls {
				initial[call.kind] = call
			}
		}
	}
	for _, child := range graphRuntimeRichPayloads(t, created.Values["body"]) {
		if _, exists := child["secret"]; exists {
			t.Fatal("attached embedded read access leaked data")
		}
		if !strings.HasPrefix(stringValue(child["code"]), "code:") {
			t.Fatal("embedded scoped transform missing")
		}
	}
	observations = nil
	_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{"body": richtextblocks.Document(richtextblocks.Block("card", "B", payload("embedded-b", " TWO ")), richtextblocks.Block("card", "A", payload("embedded-a", " ONE ")), richtextblocks.Block("card", "C", payload("embedded-c", " THREE ")))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	calls := graphRuntimePhase(observations, "validate")
	if len(calls) != 4 {
		t.Fatalf("embedded reorder count: %#v", calls)
	}
	for _, call := range calls {
		if old, exists := initial[call.kind]; exists {
			if call.prior != old.value || call.id != old.id {
				t.Errorf("embedded prior/identity lost: %#v; old %#v", call, old)
			}
		} else if call.kind != "embedded-c" || call.prior != "<new>" {
			t.Errorf("new embedded fabricated prior: %#v", call)
		}
	}
	if len(graphRuntimePhase(observations, "access")) < 4 {
		t.Fatal("embedded attached access did not execute")
	}
}
func graphRuntimeRichPayloads(t *testing.T, value store.Value) []store.Values {
	t.Helper()
	envelope, _ := value.CopyObject()
	root, _ := envelope["root"].CopyObject()
	nodes, _ := root["children"].CopyList()
	var payloads []store.Values
	for _, node := range nodes {
		object, _ := node.CopyObject()
		payload, ok := object["fields"].CopyObject()
		if !ok {
			t.Fatal("missing embedded payload")
		}
		payloads = append(payloads, payload)
	}
	return payloads
}

func TestFieldGraphRuntimeResourceLifecycleAndRollbackBoundaries(t *testing.T) {
	var order []string
	fail := false
	resource := func(phase string) ridu.Hook {
		return func(ridu.HookContext) error { order = append(order, "resource:"+phase); return nil }
	}
	observer := func(phase string) field.Observer[string] {
		return func(operation.Context, operation.Value[string]) error {
			order = append(order, "field:"+phase)
			if phase == "change" && fail {
				return errors.New("rollback after change")
			}
			return nil
		}
	}
	code := field.Text("code").Hooks(field.Hooks[string]{BeforeValidate: []field.RawTransform{func(operation.Context, operation.Value[store.Value]) (operation.Change[store.Value], error) {
		order = append(order, "field:validate")
		return operation.Keep[store.Value](), nil
	}}, BeforeChange: []field.Transform[string]{func(operation.Context, operation.Value[string]) (operation.Change[string], error) {
		order = append(order, "field:before-change")
		return operation.Keep[string](), nil
	}}, AfterChange: []field.Observer[string]{observer("change")}}).Validate(func(operation.Context, operation.Value[string]) ([]operation.Issue, error) {
		order = append(order, "custom")
		return nil, nil
	})
	app, err := ridu.New(ridu.Config{Name: "Graph lifecycle", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{code}, Hooks: ridu.CollectionHooks{BeforeValidate: []ridu.Hook{resource("validate")}, BeforeChange: []ridu.Hook{resource("before-change")}, BeforeOperation: []ridu.Hook{resource("before-operation")}, AfterChange: []ridu.Hook{resource("change")}, AfterOperation: []ridu.Hook{resource("operation")}, AfterRead: []ridu.Hook{resource("read")}, AfterCommit: []ridu.Hook{resource("commit")}}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"code": store.String("one")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"resource:validate", "field:validate", "resource:before-change", "field:before-change", "resource:before-operation", "custom", "resource:change", "field:change", "resource:operation", "resource:read", "resource:commit"}
	if !reflect.DeepEqual(order, expected) {
		t.Fatalf("lifecycle reordered: %v; want %v", order, expected)
	}
	order, fail = nil, true
	_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{"code": store.String("changed")}, ridu.MutationOptions{})
	if err == nil {
		t.Fatal("AfterChange failure did not abort")
	}
	for _, phase := range order {
		if phase == "resource:commit" {
			t.Fatal("AfterCommit ran for rollback")
		}
	}
	stored, err := app.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{})
	if err != nil || stringValue(stored.Values["code"]) != "one" {
		t.Fatalf("AfterChange failure escaped transaction: %#v: %v", stored, err)
	}
}

func TestFieldTransformsPreserveExpectedNormalization(t *testing.T) {
	code := field.Text("code").Required().Hooks(field.Hooks[string]{BeforeValidate: []field.RawTransform{func(_ operation.Context, input operation.Value[store.Value]) (operation.Change[store.Value], error) {
		value, _ := input.Get()
		text, _ := value.StringValue()
		return operation.Replace(operation.Present(store.String(strings.TrimSpace(text)))), nil
	}}, BeforeChange: []field.Transform[string]{func(_ operation.Context, input operation.Value[string]) (operation.Change[string], error) {
		value, _ := input.Get()
		return operation.Replace(operation.Present(strings.ToUpper(value))), nil
	}}})
	app, err := ridu.New(ridu.Config{Name: "Field normalization", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{code}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"code": store.String(" code ")}, ridu.MutationOptions{})
	if err != nil || stringValue(created.Values["code"]) != "CODE" {
		t.Fatalf("normalization: %#v: %v", created, err)
	}
}

func TestFieldGraphRuntimeAttachedReadDenialAcrossEveryHost(t *testing.T) {
	calls := 0
	secret := field.Text("secret").Access(field.Access{Read: func(operation.Context) (bool, error) { calls++; return false, nil }})
	body := richtext.Field("body", richtext.Config{Blocks: []field.Block{{Slug: "card", Fields: field.Fields{secret}}}})
	app, err := ridu.New(ridu.Config{Name: "Graph protected outputs", Plugins: []ridu.Plugin{richtext.New()}, Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
		secret, field.Group("group", field.Fields{secret}), field.Array("rows", field.Fields{secret}), field.Blocks("blocks", field.Block{Slug: "card", Fields: field.Fields{secret}}), field.Group("localized", field.Fields{secret.Localized()}), field.Array("localizedRows", field.Fields{secret}).Localized(), body,
	}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	values := store.Values{"secret": store.String("root private"), "group": store.Object(store.Values{"secret": store.String("group private")}), "rows": store.List(store.Object(store.Values{"_key": store.String("row"), "secret": store.String("row private")})), "blocks": store.List(store.Object(store.Values{"_key": store.String("block"), "blockType": store.String("card"), "secret": store.String("block private")})), "localized": store.Object(store.Values{"secret": store.String("localized private")}), "localizedRows": store.List(store.Object(store.Values{"_key": store.String("translated"), "secret": store.String("container private")})), "body": richtextblocks.Document(richtextblocks.Block("card", "embedded", store.Values{"secret": store.String("embedded private")}))}
	created, err := app.Local().Create(t.Context(), "pages", values, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertRedacted := func(document store.Document) {
		t.Helper()
		var walk func(store.Value)
		walk = func(value store.Value) {
			if object, ok := value.CopyObject(); ok {
				if secret, exists := object["secret"]; exists {
					// Existing all-locales redaction removes each protected locale,
					// preserving the empty localization envelope.
					locales, localized := secret.CopyObject()
					if !localized || len(locales) != 0 {
						t.Fatalf("protected field survived graph redaction: %#v", object)
					}
				}
				for _, child := range object {
					walk(child)
				}
			}
			if list, ok := value.CopyList(); ok {
				for _, child := range list {
					walk(child)
				}
			}
		}
		walk(store.Object(document.Values))
	}
	assertRedacted(created)
	if calls != 7 {
		t.Fatalf("create read denial called %d times; want seven hosts", calls)
	}
	calls = 0
	read, err := app.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	assertRedacted(read)
	if calls != 7 {
		t.Fatalf("all-locales read denial duplicated host: %d", calls)
	}
}

func TestFieldGraphRuntimeContainerTransformDispatchesSurvivingOccurrences(t *testing.T) {
	var writes, after []string
	var prior = map[string]string{}
	change := false
	code := field.Text("code").Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(ctx operation.Context, input operation.Value[string]) (operation.Change[string], error) {
		key, _ := ctx.Siblings.String("_key")
		writes = append(writes, key)
		old, ok := ctx.Prior.String("code")
		if !ok {
			old = "<new>"
		}
		prior[key] = old
		value, _ := input.Get()
		return operation.Replace(operation.Present(strings.ToUpper(value))), nil
	}}, AfterChange: []field.Observer[string]{func(ctx operation.Context, _ operation.Value[string]) error {
		key, _ := ctx.Siblings.String("_key")
		after = append(after, key)
		return nil
	}}})
	rows := field.Array("rows", field.Fields{field.Text("kind"), code}).Hooks(field.Hooks[store.Value]{BeforeChange: []field.Transform[store.Value]{func(_ operation.Context, input operation.Value[store.Value]) (operation.Change[store.Value], error) {
		if !change {
			return operation.Keep[store.Value](), nil
		}
		value, _ := input.Get()
		list, _ := value.CopyList()
		return operation.Replace(operation.Present(store.List(list[2], list[0], graphRuntimeRow("D", "new", "four")))), nil
	}}})
	app, err := ridu.New(ridu.Config{Name: "Scoped container replacement", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{rows}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"rows": store.List(graphRuntimeRow("A", "first", "one"), graphRuntimeRow("B", "removed", "two"), graphRuntimeRow("C", "third", "three"))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	change, writes, after, prior = true, nil, nil, map[string]string{}
	updated, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(writes, []string{"C", "A", "D"}) || !reflect.DeepEqual(after, []string{"C", "A", "D"}) {
		t.Fatalf("container survivors writes=%v after=%v", writes, after)
	}
	if !reflect.DeepEqual(prior, map[string]string{"A": "ONE", "C": "THREE", "D": "<new>"}) {
		t.Fatalf("container replacement prior identities = %v", prior)
	}
	output := blockRows(updated.Values["rows"])
	if len(output) != 3 || stringValue(output[2]["code"]) != "FOUR" || stringValue(output[0]["_key"]) != "C" {
		t.Fatalf("scoped transformed writeback lost: %#v", output)
	}
}

func TestFieldGraphRuntimeTypedContainerAssignsIdentityBeforeChildDispatch(t *testing.T) {
	for _, host := range []string{"array", "blocks"} {
		t.Run(host, func(t *testing.T) {
			var calls []graphRuntimeObservation
			observe := func(phase string, ctx operation.Context) {
				key, _ := ctx.Siblings.String("_key")
				calls = append(calls, graphRuntimeObservation{phase: phase, key: key, id: ctx.OccurrenceID})
			}
			code := field.Text("code").Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(ctx operation.Context, _ operation.Value[string]) (operation.Change[string], error) {
				observe("write", ctx)
				return operation.Keep[string](), nil
			}}, AfterChange: []field.Observer[string]{func(ctx operation.Context, _ operation.Value[string]) error {
				observe("changed", ctx)
				return nil
			}}}).Validate(func(ctx operation.Context, _ operation.Value[string]) ([]operation.Issue, error) {
				observe("validate", ctx)
				return nil, nil
			})
			add := false
			hooks := field.Hooks[store.Value]{BeforeChange: []field.Transform[store.Value]{func(_ operation.Context, input operation.Value[store.Value]) (operation.Change[store.Value], error) {
				if !add {
					return operation.Keep[store.Value](), nil
				}
				value, _ := input.Get()
				rows, _ := value.CopyList()
				fresh := store.Values{"code": store.String("fresh")}
				if host == "blocks" {
					fresh["blockType"] = store.String("card")
				}
				return operation.Replace(operation.Present(store.List(append(rows, store.Object(fresh))...))), nil
			}}}
			var container field.Node = field.Array("rows", field.Fields{code}).Hooks(hooks)
			initial := store.Values{"_key": store.String("retained"), "code": store.String("old")}
			if host == "blocks" {
				container = field.Blocks("rows", field.Block{Slug: "card", Fields: field.Fields{code}}).Hooks(hooks)
				initial["blockType"] = store.String("card")
			}
			app, err := ridu.New(ridu.Config{Name: "Typed fresh identities", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{container}}}}, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			created, err := app.Local().Create(t.Context(), "pages", store.Values{"rows": store.List(store.Object(initial))}, ridu.MutationOptions{})
			if err != nil {
				t.Fatal(err)
			}
			add, calls = true, nil
			updated, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{}, ridu.MutationOptions{})
			if err != nil {
				t.Fatal(err)
			}
			rows := blockRows(updated.Values["rows"])
			if len(rows) != 2 {
				t.Fatalf("typed added row missing: %#v", rows)
			}
			freshKey := stringValue(rows[1]["_key"])
			if freshKey == "" || freshKey == "retained" {
				t.Fatal("fresh identity missing")
			}
			var freshID operation.OccurrenceID
			for _, phase := range []string{"write", "validate", "changed"} {
				selected := graphRuntimePhase(calls, phase)
				if len(selected) != 2 {
					t.Fatalf("%s count=%d: %#v", phase, len(selected), selected)
				}
				if selected[1].key != freshKey {
					t.Fatalf("%s identity %q differs from persisted %q", phase, selected[1].key, freshKey)
				}
				if freshID == "" {
					freshID = selected[1].id
				} else if freshID != selected[1].id {
					t.Fatalf("%s concrete identity changed: %q vs %q", phase, selected[1].id, freshID)
				}
			}
		})
	}
}
