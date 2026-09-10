package core_test

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/primitivelists"
	richtextblocks "github.com/riducms/ridu/tests/contracts/richtext_blocks"
)

func TestPrimitiveListsLocalRESTNestedAndLocalized(t *testing.T) {
	points := store.List(store.String(""), store.String("Oak"), store.String("Oak"))
	sizes := store.List(store.Number(0), store.Number(8.5), store.Number(8.5))
	children := store.Values{"points": points, "sizes": sizes}
	row := store.CloneValues(children)
	row["_key"] = store.String("row-A")
	block := store.CloneValues(children)
	block["_key"] = store.String("block-A")
	block["blockType"] = store.String("card")
	values := store.Values{"title": store.String("Product"), "sellingPoints": points, "availableSizes": sizes, "details": store.Object(children), "variants": store.List(store.Object(row)), "content": store.List(store.Object(block)), "body": richtextblocks.Document(richtextblocks.Block("card", "embed-A", children)), "localizedPoints": points, "localizedSizes": sizes, "localizedBody": richtextblocks.Document(richtextblocks.Block("card", "embed-fr", children))}
	config := primitivelists.Config()
	config.Collections[0].Access.Publish = config.Collections[0].Access.Update
	app, err := core.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().CreateWithOptions(t.Context(), "primitive-products", values, core.MutationOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range values {
		primitiveRuntimeEqual(t, created.Values[key], value)
	}
	encoded, _ := json.Marshal(values)
	response := requestJSON(t, handlerClient(app.Handler(core.HandlerOptions{})), http.MethodPost, "http://ridu.test/api/collections/primitive-products?locale=fr", strings.NewReader(string(encoded)), "")
	if response.StatusCode != 201 {
		t.Fatalf("REST %d %s", response.StatusCode, readBody(t, response))
	}
	var wire struct {
		Doc map[string]json.RawMessage `json:"doc"`
	}
	decodeResponse(t, response, &wire)
	for key, value := range values {
		var actual store.Value
		if err := json.Unmarshal(wire.Doc[key], &actual); err != nil {
			t.Fatalf("REST %s: %v", key, err)
		}
		primitiveRuntimeEqual(t, actual, value)
	}
	updated, err := app.Local().PublishChangesWithOptions(t.Context(), "primitive-products", created.ID, store.Values{"sellingPoints": store.List(store.String("Replacement")), "availableSizes": store.List(), "localizedPoints": store.List()}, core.MutationOptions{Locale: "fr", ExpectedRevision: created.Revision})
	if err != nil {
		t.Fatal(err)
	}
	primitiveRuntimeEqual(t, updated.Values["sellingPoints"], store.List(store.String("Replacement")))
	primitiveRuntimeEqual(t, updated.Values["availableSizes"], store.List())
	duplicate, err := app.Local().DuplicateWithOptions(t.Context(), "primitive-products", created.ID, nil, core.MutationOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	primitiveRuntimeEqual(t, duplicate.Values["sellingPoints"], updated.Values["sellingPoints"])
	primitiveRuntimeEqual(t, duplicate.Values["availableSizes"], store.List())
	restored, err := app.Local().RestoreVersionWithOptions(t.Context(), "primitive-products", created.ID, created.Revision, false, core.MutationOptions{Locale: "fr", ExpectedRevision: updated.Revision})
	if err != nil {
		t.Fatal(err)
	}
	primitiveRuntimeEqual(t, restored.Values["sellingPoints"], points)
	primitiveRuntimeEqual(t, restored.Values["availableSizes"], sizes)
}

func TestPrimitiveListIssuesRemainFieldLevelAcrossTransports(t *testing.T) {
	invalid := store.List(store.String("ok"), store.String(strings.Repeat("x", 121)))
	children := store.Values{"points": invalid}
	row := store.CloneValues(children)
	row["_key"] = store.String("row-A")
	block := store.CloneValues(children)
	block["_key"] = store.String("block-A")
	block["blockType"] = store.String("card")
	sentinelRow := store.CloneValues(row)
	sentinelRow["_key"] = store.String("@locale")
	sentinelBlock := store.CloneValues(block)
	sentinelBlock["_key"] = store.String("@locale")
	for _, test := range []struct {
		name, path, key string
		values          store.Values
	}{
		{"root", "sellingPoints", "", store.Values{"sellingPoints": invalid}},
		{"array", "variants.0.points", "row-A", store.Values{"variants": store.List(store.Object(row))}},
		{"block", "content.0.points", "block-A", store.Values{"content": store.List(store.Object(block))}},
		{"array reserved-looking key", "variants.0.points", "@locale", store.Values{"variants": store.List(store.Object(sentinelRow))}},
		{"block reserved-looking key", "content.0.points", "@locale", store.Values{"content": store.List(store.Object(sentinelBlock))}},
		{"locale", "localizedPoints", "", store.Values{"localizedPoints": store.List(store.String("ok"), store.Null())}},
		{"embedded", "body.root.children.0.fields.points", "embed-A", store.Values{"body": richtextblocks.Document(richtextblocks.Block("card", "embed-A", children))}},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, err := core.New(primitivelists.Config(), teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			values := store.Values{"title": store.String("Product"), "sellingPoints": store.List(store.String("valid"))}
			for key, value := range test.values {
				values[key] = value
			}
			_, err = app.Local().CreateWithOptions(t.Context(), "primitive-products", values, core.MutationOptions{Locale: "fr"})
			var failure *core.OperationError
			if !errors.As(err, &failure) || len(failure.Issues) != 1 {
				t.Fatalf("issues %v", err)
			}
			issue := failure.Issues[0]
			if issue.Path != test.path || issue.Locale != "fr" || issue.FieldID == "" || issue.CollectionID != "primitive-products" || !strings.Contains(issue.Message, "item 2") {
				t.Fatalf("issue=%#v", issue)
			}
			if issue.Target == "" || test.key != "" && !strings.Contains(issue.Target, test.key) {
				t.Fatalf("field correlation=%#v", issue)
			}
			encoded, _ := json.Marshal(values)
			response := requestJSON(t, handlerClient(app.Handler(core.HandlerOptions{})), http.MethodPost, "http://ridu.test/api/collections/primitive-products?locale=fr", strings.NewReader(string(encoded)), "")
			if response.StatusCode != 422 {
				t.Fatalf("REST %d %s", response.StatusCode, readBody(t, response))
			}
			var wire protocol.ErrorEnvelope
			decodeResponse(t, response, &wire)
			a, _ := json.Marshal(failure.Issues)
			b, _ := json.Marshal(wire.Error.Issues)
			if string(a) != string(b) {
				t.Fatalf("Local/REST mismatch %s %s", a, b)
			}
		})
	}
}

func TestPrimitiveListCallbacksDefaultsAccessAndCopies(t *testing.T) {
	source := []string{"oak", "oak"}
	numberSource := []float64{0, 8.5}
	defaults := 0
	raw := 0
	typed := 0
	validated := 0
	read := 0
	texts := field.TextList("points").DefaultFrom(func(operation.DefaultContext) (operation.Value[[]string], error) {
		defaults++
		return operation.Present(source), nil
	}).Hooks(field.Hooks[[]string]{BeforeValidate: []field.RawTransform{func(operation.WriteContext, operation.Value[store.Value]) (operation.Change[store.Value], error) {
		raw++
		return operation.Keep[store.Value](), nil
	}}, BeforeChange: []field.Transform[[]string]{func(ctx operation.WriteContext, value operation.Value[[]string]) (operation.Change[[]string], error) {
		typed++
		items, _ := value.Get()
		items[0] = "ignored mutation"
		return operation.Keep[[]string](), nil
	}}}).Validate(func(ctx operation.ValidationContext, value operation.Value[[]string]) ([]operation.Issue, error) {
		validated++
		items, _ := value.Get()
		if !reflect.DeepEqual(items, source) {
			t.Fatalf("validator observed mutation: %v", items)
		}
		items[0] = "ignored validation mutation"
		return nil, nil
	}).ReadHooks(field.ReadHooks[[]string]{AfterRead: []field.OutputTransform[[]string]{func(operation.ReadContext, operation.Value[[]string]) (operation.Change[[]string], error) {
		read++
		return operation.Keep[[]string](), nil
	}}})
	numbers := field.NumberList("sizes").DefaultFrom(func(operation.DefaultContext) (operation.Value[[]float64], error) {
		return operation.Present(numberSource), nil
	})
	deny := func(operation.AccessContext) (bool, error) { return false, nil }
	app, err := core.New(core.Config{Name: "Lists", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{texts, numbers, field.TextList("empty").Default(), field.NumberList("literal").Default(0, 0), field.TextList("secret").Default("hidden").Access(field.Access{Read: deny}), field.NumberList("locked").Access(field.Access{Update: deny})}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "products", store.Values{"locked": store.List(store.Number(1))}, nil)
	if err != nil {
		t.Fatal(err)
	}
	primitiveRuntimeEqual(t, created.Values["points"], store.List(store.String("oak"), store.String("oak")))
	primitiveRuntimeEqual(t, created.Values["sizes"], store.List(store.Number(0), store.Number(8.5)))
	primitiveRuntimeEqual(t, created.Values["empty"], store.List())
	primitiveRuntimeEqual(t, created.Values["literal"], store.List(store.Number(0), store.Number(0)))
	if _, present := created.Values["secret"]; present {
		t.Fatal("read denied list leaked")
	}
	if defaults != 1 || typed != 1 || validated != 1 || raw == 0 || read == 0 {
		t.Fatalf("callback counts %d %d %d %d %d", defaults, typed, validated, raw, read)
	}
	source[0] = "source changed"
	numberSource[0] = 99

	found, err := app.Local().FindWithOptions(t.Context(), "products", created.ID, core.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	primitiveRuntimeEqual(t, found.Values["points"], store.List(store.String("oak"), store.String("oak")))
	primitiveRuntimeEqual(t, found.Values["sizes"], store.List(store.Number(0), store.Number(8.5)))
	_, err = app.Local().Update(t.Context(), "products", created.ID, store.Values{"locked": store.List(store.Number(2))}, nil)
	var failure *core.OperationError
	if !errors.As(err, &failure) || failure.Status != 403 {
		t.Fatalf("update access=%v", err)
	}
}

func primitiveRuntimeEqual(t *testing.T, actual, want store.Value) {
	t.Helper()
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(want)
	if string(a) != string(b) {
		t.Fatalf("value %s, want %s", a, b)
	}
}

func TestPrimitiveListTypedReplacementsAndInvalidDynamicDefaults(t *testing.T) {
	replacement := []float64{0, 2, 2}
	app, err := core.New(core.Config{Name: "List hooks", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{
		field.TextList("points").Hooks(field.Hooks[[]string]{BeforeValidate: []field.RawTransform{func(ctx operation.WriteContext, value operation.Value[store.Value]) (operation.Change[store.Value], error) {
			return operation.Replace(operation.Present(store.List(store.String(""), store.String("from raw hook")))), nil
		}}}),
		field.NumberList("sizes").Hooks(field.Hooks[[]float64]{BeforeChange: []field.Transform[[]float64]{func(ctx operation.WriteContext, value operation.Value[[]float64]) (operation.Change[[]float64], error) {
			return operation.Replace(operation.Present(replacement)), nil
		}}}),
	}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "products", store.Values{"points": store.List(), "sizes": store.List()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	primitiveRuntimeEqual(t, created.Values["points"], store.List(store.String(""), store.String("from raw hook")))
	primitiveRuntimeEqual(t, created.Values["sizes"], store.List(store.Number(0), store.Number(2), store.Number(2)))
	replacement[0] = 99
	primitiveRuntimeEqual(t, created.Values["sizes"], store.List(store.Number(0), store.Number(2), store.Number(2)))
	for _, node := range []field.Node{
		field.TextList("value").Required().DefaultFrom(func(operation.DefaultContext) (operation.Value[[]string], error) {
			return operation.Present([]string(nil)), nil
		}),
		field.TextList("value").MaxLength(2).DefaultFrom(func(operation.DefaultContext) (operation.Value[[]string], error) {
			return operation.Present([]string{"too long"}), nil
		}),
		field.NumberList("value").Min(0).DefaultFrom(func(operation.DefaultContext) (operation.Value[[]float64], error) {
			return operation.Present([]float64{-1}), nil
		}),
	} {
		bad, err := core.New(core.Config{Name: "List defaults", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{node}}}}, teststore.New())
		if err != nil {
			t.Fatal(err)
		}
		_, err = bad.Local().Create(t.Context(), "products", store.Values{}, nil)
		var failure *core.OperationError
		if !errors.As(err, &failure) || failure.Status != 422 {
			t.Fatalf("invalid dynamic list default: %v", err)
		}
	}
}

func TestPrimitiveListQueryRejectsNonfiniteMembership(t *testing.T) {
	app, err := core.New(core.Config{Name: "Lists", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{field.NumberList("sizes")}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	path, _ := query.ParsePath("sizes")
	for _, number := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := app.Local().List(t.Context(), "products", core.ListOptions{Where: query.In(path, query.Number(number))})
		var failure *core.OperationError
		if !errors.As(err, &failure) || failure.Status != 400 || !strings.Contains(err.Error(), "finite numbers") {
			t.Fatalf("nonfinite query = %v", err)
		}
	}
}

func TestPrimitiveListDynamicDefaultSliceBoundaries(t *testing.T) {
	tests := []struct {
		name             string
		node             field.Node
		present, invalid bool
	}{
		{"nil text slice", field.TextList("value").DefaultFrom(func(operation.DefaultContext) (operation.Value[[]string], error) {
			return operation.Present([]string(nil)), nil
		}), true, false},
		{"empty text slice", field.TextList("value").DefaultFrom(func(operation.DefaultContext) (operation.Value[[]string], error) {
			return operation.Present([]string{}), nil
		}), true, false},
		{"missing text list", field.TextList("value").DefaultFrom(func(operation.DefaultContext) (operation.Value[[]string], error) {
			return operation.Empty[[]string](), nil
		}), false, false},
		{"nil number slice", field.NumberList("value").DefaultFrom(func(operation.DefaultContext) (operation.Value[[]float64], error) {
			return operation.Present([]float64(nil)), nil
		}), true, false},
		{"missing number list", field.NumberList("value").DefaultFrom(func(operation.DefaultContext) (operation.Value[[]float64], error) {
			return operation.Empty[[]float64](), nil
		}), false, false},
		{"item length", field.TextList("value").MaxLength(2).DefaultFrom(func(operation.DefaultContext) (operation.Value[[]string], error) {
			return operation.Present([]string{"long"}), nil
		}), false, true},
		{"item count", field.NumberList("value").MaxRows(1).DefaultFrom(func(operation.DefaultContext) (operation.Value[[]float64], error) {
			return operation.Present([]float64{0, 0}), nil
		}), false, true},
		{"NaN", field.NumberList("value").DefaultFrom(func(operation.DefaultContext) (operation.Value[[]float64], error) {
			return operation.Present([]float64{math.NaN()}), nil
		}), false, true},
		{"infinity", field.NumberList("value").DefaultFrom(func(operation.DefaultContext) (operation.Value[[]float64], error) {
			return operation.Present([]float64{math.Inf(1)}), nil
		}), false, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app, err := core.New(core.Config{Name: "Defaults", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{test.node}}}}, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			created, err := app.Local().Create(t.Context(), "products", store.Values{}, nil)
			if test.invalid {
				if err == nil {
					t.Fatal("invalid list default persisted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if test.present {
				primitiveRuntimeEqual(t, created.Values["value"], store.List())
			} else if value, present := created.Values["value"]; present && value.Kind() != store.ValueNull {
				t.Fatalf("Empty supplied a value: %#v", value)
			}
		})
	}
	textCalls, numberCalls := 0, 0
	app, err := core.New(core.Config{Name: "Defaults", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{
		field.Text("title"), field.TextList("points").DefaultFrom(func(operation.DefaultContext) (operation.Value[[]string], error) {
			textCalls++
			return operation.Present([]string{"default"}), nil
		}), field.NumberList("sizes").DefaultFrom(func(operation.DefaultContext) (operation.Value[[]float64], error) {
			numberCalls++
			return operation.Present([]float64{0}), nil
		}),
	}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []store.Value{store.Null(), store.List()} {
		created, err := app.Local().Create(t.Context(), "products", store.Values{"points": value, "sizes": value}, nil)
		if err != nil {
			t.Fatal(err)
		}
		updated, err := app.Local().Update(t.Context(), "products", created.ID, store.Values{"title": store.String("Changed")}, nil)
		if err != nil {
			t.Fatal(err)
		}
		primitiveRuntimeEqual(t, updated.Values["points"], value)
		primitiveRuntimeEqual(t, updated.Values["sizes"], value)
	}
	if textCalls != 0 || numberCalls != 0 {
		t.Fatalf("explicit empty values invoked defaults %d/%d", textCalls, numberCalls)
	}
	created, err := app.Local().Create(t.Context(), "products", store.Values{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Local().Update(t.Context(), "products", created.ID, store.Values{"title": store.String("Changed")}, nil); err != nil {
		t.Fatal(err)
	}
	if textCalls != 1 || numberCalls != 1 {
		t.Fatalf("ordinary update reran defaults %d/%d", textCalls, numberCalls)
	}
}

func TestPrimitiveListReadHooksRespectOutputShape(t *testing.T) {
	for _, clear := range []bool{false, true} {
		texts := field.TextList("points").Required().MaxRows(1).MaxLength(2).ReadHooks(field.ReadHooks[[]string]{AfterRead: []field.OutputTransform[[]string]{func(operation.ReadContext, operation.Value[[]string]) (operation.Change[[]string], error) {
			if clear {
				return operation.Replace(operation.Empty[[]string]()), nil
			}
			return operation.Replace(operation.Present([]string{"formatted longer", "extra"})), nil
		}}})
		numbers := field.NumberList("sizes").Required().Max(1).MaxRows(1).ReadHooks(field.ReadHooks[[]float64]{AfterRead: []field.OutputTransform[[]float64]{func(operation.ReadContext, operation.Value[[]float64]) (operation.Change[[]float64], error) {
			if clear {
				return operation.Replace(operation.Empty[[]float64]()), nil
			}
			return operation.Replace(operation.Present([]float64{9, 10})), nil
		}}})
		app, err := core.New(core.Config{Name: "Read lists", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{texts, numbers}}}}, teststore.New())
		if err != nil {
			t.Fatal(err)
		}
		created, err := app.Local().Create(t.Context(), "products", store.Values{"points": store.List(store.String("ok")), "sizes": store.List(store.Number(0))}, nil)
		if clear {
			var failure *core.OperationError
			if !errors.As(err, &failure) || failure.Code != "invalid_field_output" {
				t.Fatalf("required null output accepted: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		primitiveRuntimeEqual(t, created.Values["points"], store.List(store.String("formatted longer"), store.String("extra")))
		primitiveRuntimeEqual(t, created.Values["sizes"], store.List(store.Number(9), store.Number(10)))
	}
}

func TestPrimitiveListReadOutputRejectsMalformedValues(t *testing.T) {
	tests := []struct {
		name, want string
		node       field.Node
		initial    store.Value
		invalid    store.Value
		number     float64
		typed      bool
	}{
		{name: "typed NaN", want: "item 1", node: field.NumberList("value"), initial: store.List(store.Number(0)), number: math.NaN(), typed: true},
		{name: "typed infinity", want: "item 1", node: field.NumberList("value"), initial: store.List(store.Number(0)), number: math.Inf(1), typed: true},
		{name: "text scalar", want: "array of strings", node: field.TextList("value"), initial: store.List(store.String("ok")), invalid: store.String("wrong")},
		{name: "number scalar", want: "array of finite numbers", node: field.NumberList("value"), initial: store.List(store.Number(0)), invalid: store.Number(1)},
		{name: "text mixed", want: "item 2", node: field.TextList("value"), initial: store.List(store.String("ok")), invalid: store.List(store.String("ok"), store.Number(1))},
		{name: "number mixed", want: "item 2", node: field.NumberList("value"), initial: store.List(store.Number(0)), invalid: store.List(store.Number(1), store.String("wrong"))},
		{name: "null item", want: "item 2", node: field.TextList("value"), initial: store.List(store.String("ok")), invalid: store.List(store.String("ok"), store.Null())},
		{name: "object item", want: "item 1", node: field.NumberList("value"), initial: store.List(store.Number(0)), invalid: store.List(store.Object(store.Values{}))},
		{name: "resource NaN", want: "item 1", node: field.NumberList("value"), initial: store.List(store.Number(0)), invalid: store.List(store.Number(math.NaN()))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			active := false
			collection := core.Collection{Slug: "products", Fields: field.Fields{test.node}}
			if test.typed {
				collection.Fields = field.Fields{field.NumberList("value").ReadHooks(field.ReadHooks[[]float64]{AfterRead: []field.OutputTransform[[]float64]{func(operation.ReadContext, operation.Value[[]float64]) (operation.Change[[]float64], error) {
					if active {
						return operation.Replace(operation.Present([]float64{test.number})), nil
					}
					return operation.Keep[[]float64](), nil
				}}})}
			} else {
				collection.Hooks.AfterRead = []core.Hook{func(ctx core.HookContext) error {
					if active {
						ctx.Document.Values["value"] = test.invalid
					}
					return nil
				}}
			}
			app, err := core.New(core.Config{Name: "Invalid list output", Collections: []core.Collection{collection}}, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			created, err := app.Local().Create(t.Context(), "products", store.Values{"value": test.initial}, nil)
			if err != nil {
				t.Fatal(err)
			}
			active = true
			_, err = app.Local().Find(t.Context(), "products", created.ID, nil)
			var failure *core.OperationError
			if !errors.As(err, &failure) || failure.Code != "invalid_field_output" || !strings.Contains(err.Error(), `"value"`) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("malformed list output = %v", err)
			}
			response := requestJSON(t, handlerClient(app.Handler(core.HandlerOptions{})), http.MethodGet, "http://ridu.test/api/collections/products/"+created.ID, nil, "")
			body := readBody(t, response)
			// Server faults retain the transport's established redacted envelope;
			// the actionable field/item diagnostic remains in the local error/log.
			if response.StatusCode != 500 || !strings.Contains(body, `"code":"internal"`) {
				t.Fatalf("REST %d: %s", response.StatusCode, body)
			}
		})
	}
}

func TestPrimitiveListReadOutputChecksNestedOccurrences(t *testing.T) {
	invalid := store.List(store.String("ok"), store.Null())
	for _, test := range []struct {
		name, path string
		values     store.Values
		allLocales bool
	}{
		{"group", "details.points", store.Values{"details": store.Object(store.Values{"points": invalid})}, false},
		{"array", "variants.0.points", store.Values{"variants": store.List(store.Object(store.Values{"_key": store.String("A"), "points": invalid}))}, false},
		{"block", "content.0.points", store.Values{"content": store.List(store.Object(store.Values{"_key": store.String("A"), "blockType": store.String("card"), "points": invalid}))}, false},
		{"locale", "localizedPoints", store.Values{"localizedPoints": invalid}, false},
		{"all locales", "localizedPoints.fr", store.Values{"localizedPoints": store.Object(store.Values{"fr": invalid})}, true},
		{"embedded", "body.root.children.0.fields.points", store.Values{"body": richtextblocks.Document(richtextblocks.Block("card", "A", store.Values{"points": invalid}))}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			active := false
			config := primitivelists.Config()
			config.Collections[0].Hooks.AfterRead = []core.Hook{func(ctx core.HookContext) error {
				if active {
					for name, value := range test.values {
						ctx.Document.Values[name] = value
					}
				}
				return nil
			}}
			app, err := core.New(config, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			created, err := app.Local().CreateWithOptions(t.Context(), "primitive-products", primitivelists.Values(), core.MutationOptions{Locale: "fr"})
			if err != nil {
				t.Fatal(err)
			}
			active = true
			options := core.FindOptions{AllLocales: test.allLocales}
			if !test.allLocales {
				options.Locale = "fr"
			}
			_, err = app.Local().FindWithOptions(t.Context(), "primitive-products", created.ID, options)
			var failure *core.OperationError
			if !errors.As(err, &failure) || failure.Code != "invalid_field_output" || !strings.Contains(err.Error(), `"`+test.path+`"`) || !strings.Contains(err.Error(), "item 2") {
				t.Fatalf("malformed nested output = %v", err)
			}
		})
	}
}
