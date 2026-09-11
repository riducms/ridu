package richtextblocks

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// RunNames proves that a configured block name remains an ordinary optional
// text child in both Blocks and schema-backed rich text. The same contract runs
// against every official adapter so no adapter-specific storage or backfill is
// needed for the presentation metadata.
func RunNames(t *testing.T, factory Factory) {
	t.Helper()
	backend, app := factory(t, nameConfiguration())
	handler := app.Handler(ridu.HandlerOptions{})

	for _, test := range []struct {
		name   string
		values store.Values
		path   string
		rest   bool
	}{
		{
			name: "ordinary validation",
			values: store.Values{"layout": store.List(store.Object(store.Values{
				"blockType": store.String("named"), "name": store.String("invalid"),
			}))},
			path: "layout.0.name",
		},
		{
			name: "rich text validation",
			values: store.Values{"body": Document(Block("named", "", store.Values{
				"name": store.String("invalid"),
			}))},
			path: "body.root.children.0.fields.name",
			rest: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.rest {
				response := nameRequest(t, handler, http.MethodPost, "/api/collections/named-pages?locale=en", test.values)
				if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), test.path) {
					t.Fatalf("REST validation = %d %s", response.Code, response.Body.String())
				}
				return
			}
			_, err := app.Local().Create(t.Context(), "named-pages", test.values, ridu.MutationOptions{Locale: "en"})
			issue(t, err, test.path)
		})
	}

	createdResponse := nameRequest(t, handler, http.MethodPost, "/api/collections/named-pages?locale=en", namedValues(" ordinary ", " rich "))
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("REST create = %d %s", createdResponse.Code, createdResponse.Body.String())
	}
	created := nameDocument(t, createdResponse)
	assertNames(t, created.Values, "ORDINARY", "RICH", false)

	english, err := app.Local().Find(t.Context(), "named-pages", created.ID, ridu.FindOptions{Locale: "en", DisableFallback: true})
	if err != nil {
		t.Fatal(err)
	}
	assertNames(t, english.Values, "ORDINARY", "RICH", false)

	updatedResponse := nameRequest(t, handler, http.MethodPatch, "/api/collections/named-pages/"+created.ID+"?locale=fr", namedPatch(" ordinaire ", " riche "))
	if updatedResponse.Code != http.StatusOK {
		t.Fatalf("REST localized update = %d %s", updatedResponse.Code, updatedResponse.Body.String())
	}
	assertNames(t, nameDocument(t, updatedResponse).Values, "ORDINAIRE", "RICHE", false)

	frenchResponse := nameRequest(t, handler, http.MethodGet, "/api/collections/named-pages/"+created.ID+"?locale=fr&fallback-locale=false", nil)
	if frenchResponse.Code != http.StatusOK {
		t.Fatalf("REST localized read = %d %s", frenchResponse.Code, frenchResponse.Body.String())
	}
	assertNames(t, nameDocument(t, frenchResponse).Values, "ORDINAIRE", "RICHE", false)

	_, err = app.Local().Update(t.Context(), "named-pages", created.ID, store.Values{"layout": store.List(
		nameRow("ordinary", "changed", "changed"),
		nameRow("ordinary-unnamed", "", ""),
	)}, ridu.MutationOptions{Locale: "fr"})
	issue(t, err, "layout.0.protected")

	denied := store.Values{"body": Document(
		Block("named", "rich", store.Values{"protected": store.String("changed")}),
		Block("named", "rich-unnamed", store.Values{}),
	)}
	deniedResponse := nameRequest(t, handler, http.MethodPatch, "/api/collections/named-pages/"+created.ID+"?locale=fr", denied)
	if deniedResponse.Code != http.StatusForbidden || !strings.Contains(deniedResponse.Body.String(), "body.root.children.0.fields.protected") {
		t.Fatalf("REST protected edit = %d %s", deniedResponse.Code, deniedResponse.Body.String())
	}

	all, err := app.Local().Find(t.Context(), "named-pages", created.ID, ridu.FindOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	assertLocalizedNames(t, all.Values)
	assertProtected(t, all.Values)

	collection := nameCollection(t, app.Manifest().Snapshot())
	transaction, err := backend.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	raw, findErr := transaction.Find(t.Context(), store.Request{Collection: collection, ID: created.ID, Locales: []schema.LocaleCode{"en", "fr"}})
	rollbackErr := transaction.Rollback(t.Context())
	if findErr != nil {
		t.Fatal(findErr)
	}
	if rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
	assertLocalizedNames(t, raw.Values)
	assertProtected(t, raw.Values)
}

func nameConfiguration() ridu.Config {
	name := field.Text("name").Localized().
		Validate(func(_ operation.Context, value operation.Value[string]) ([]operation.Issue, error) {
			text, present := value.Get()
			if present && strings.EqualFold(strings.TrimSpace(text), "invalid") {
				return []operation.Issue{{Code: "invalid_block_name", Message: "Choose another block name"}}, nil
			}
			return nil, nil
		}).
		Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(_ operation.Context, value operation.Value[string]) (operation.Change[string], error) {
			text, present := value.Get()
			if !present {
				return operation.Keep[string](), nil
			}
			return operation.Replace(operation.Present(strings.ToUpper(strings.TrimSpace(text)))), nil
		}}})
	protected := field.Text("protected").Access(field.Access{Update: func(operation.Context) (bool, error) { return false, nil }})
	named := field.Block{
		Slug:   "named",
		Admin:  field.BlockAdmin{NameField: "name"},
		Fields: field.Fields{name, protected},
	}
	return ridu.Config{
		Name:    "Block names",
		Plugins: []ridu.Plugin{richtext.New()},
		Localization: ridu.LocalizationConfig{
			DefaultLocale: "en",
			Locales: []ridu.Locale{
				{Code: "en", Label: "English"},
				{Code: "fr", Label: "French"},
			},
		},
		Collections: []ridu.Collection{{Slug: "named-pages", Fields: field.Fields{
			field.Blocks("layout", named),
			richtext.Field("body", richtext.Config{Blocks: []field.Block{named}}),
		}}},
	}
}

func namedValues(ordinary, embedded string) store.Values {
	return store.Values{
		"layout": store.List(
			nameRow("ordinary", ordinary, "ordinary protected"),
			nameRow("ordinary-unnamed", "", "unnamed ordinary protected"),
		),
		"body": Document(
			Block("named", "rich", nameChildren(embedded, "rich protected")),
			Block("named", "rich-unnamed", store.Values{"protected": store.String("unnamed rich protected")}),
		),
	}
}

func namedPatch(ordinary, embedded string) store.Values {
	return store.Values{
		"layout": store.List(
			nameRow("ordinary", ordinary, ""),
			nameRow("ordinary-unnamed", "", ""),
		),
		"body": Document(
			Block("named", "rich", nameChildren(embedded, "")),
			Block("named", "rich-unnamed", store.Values{}),
		),
	}
}

func nameRow(key, name, protected string) store.Value {
	values := store.Values{"_key": store.String(key), "blockType": store.String("named")}
	if name != "" {
		values["name"] = store.String(name)
	}
	if protected != "" {
		values["protected"] = store.String(protected)
	}
	return store.Object(values)
}

func nameChildren(name, protected string) store.Values {
	values := store.Values{}
	if name != "" {
		values["name"] = store.String(name)
	}
	if protected != "" {
		values["protected"] = store.String(protected)
	}
	return values
}

func nameRequest(t *testing.T, handler http.Handler, method, target string, values store.Values) *httptest.ResponseRecorder {
	t.Helper()
	var body *bytes.Reader
	if values == nil {
		body = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, target, body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func nameDocument(t *testing.T, response *httptest.ResponseRecorder) store.Document {
	t.Helper()
	var envelope struct {
		Doc map[string]json.RawMessage `json:"doc"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := json.Unmarshal(envelope.Doc["id"], &id); err != nil || id == "" {
		t.Fatalf("missing REST document: %s", response.Body.String())
	}
	values := store.Values{}
	for _, name := range []string{"layout", "body"} {
		raw, present := envelope.Doc[name]
		if !present {
			continue
		}
		var value store.Value
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatalf("decode REST %s: %v", name, err)
		}
		values[name] = value
	}
	return store.Document{ID: id, Values: values}
}

func assertNames(t *testing.T, values store.Values, ordinary, embedded string, localized bool) {
	t.Helper()
	rows, _ := values["layout"].CopyList()
	first, _ := rows[0].CopyObject()
	if got := stringValue(first["name"]); !localized && got != ordinary {
		t.Fatalf("ordinary name = %q, want %q", got, ordinary)
	}
	second, _ := rows[1].CopyObject()
	if _, present := second["name"]; present {
		t.Fatal("optional ordinary name was materialized")
	}
	if got := stringValue(payload(t, values["body"], 0)["name"]); !localized && got != embedded {
		t.Fatalf("rich-text name = %q, want %q", got, embedded)
	}
	if _, present := payload(t, values["body"], 1)["name"]; present {
		t.Fatal("optional rich-text name was materialized")
	}
}

func assertLocalizedNames(t *testing.T, values store.Values) {
	t.Helper()
	rows, _ := values["layout"].CopyList()
	ordinary, _ := rows[0].CopyObject()
	ordinaryNames, ok := ordinary["name"].CopyObject()
	if !ok || stringValue(ordinaryNames["en"]) != "ORDINARY" || stringValue(ordinaryNames["fr"]) != "ORDINAIRE" {
		t.Fatalf("ordinary localized names = %#v", ordinary["name"])
	}
	richNames, ok := payload(t, values["body"], 0)["name"].CopyObject()
	if !ok || stringValue(richNames["en"]) != "RICH" || stringValue(richNames["fr"]) != "RICHE" {
		t.Fatalf("rich-text localized names = %#v", payload(t, values["body"], 0)["name"])
	}
	assertNames(t, values, "", "", true)
}

func assertProtected(t *testing.T, values store.Values) {
	t.Helper()
	rows, _ := values["layout"].CopyList()
	first, _ := rows[0].CopyObject()
	second, _ := rows[1].CopyObject()
	if stringValue(first["protected"]) != "ordinary protected" || stringValue(second["protected"]) != "unnamed ordinary protected" {
		t.Fatalf("ordinary protected siblings changed: %#v", values["layout"])
	}
	if stringValue(payload(t, values["body"], 0)["protected"]) != "rich protected" || stringValue(payload(t, values["body"], 1)["protected"]) != "unnamed rich protected" {
		t.Fatalf("rich-text protected siblings changed: %#v", values["body"])
	}
}

func nameCollection(t *testing.T, snapshot schema.Snapshot) schema.Collection {
	t.Helper()
	for _, collection := range snapshot.Collections {
		if collection.Slug == "named-pages" {
			return collection
		}
	}
	t.Fatal("missing named-pages collection")
	return schema.Collection{}
}
