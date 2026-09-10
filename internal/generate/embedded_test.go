package generate

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/internal/typescript"
	"github.com/riducms/ridu/store"
	outline "github.com/riducms/ridu/tests/contracts/embedded_plugin"
)

func embeddedContractApp(t *testing.T) *core.App {
	t.Helper()
	card := field.Block{TypeName: "OutlineCard", Slug: "card", Fields: field.Fields{field.Text("title").Required(), field.Text("translation").Localized(), field.Relationship("target", "targets"), field.Group("style", field.Fields{field.Text("tone")}), field.Array("links", field.Fields{field.Text("label").Required()}), field.Blocks("children", field.Block{Slug: "note", Fields: field.Fields{field.Text("text")}})}}
	app, err := core.New(core.Config{Name: "Embedded contracts", Plugins: []core.Plugin{outline.Plugin{}}, Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []core.Collection{{Slug: "targets", Fields: field.Fields{field.Text("name")}}, {Slug: "pages", Fields: field.Fields{outline.Field("body", card, field.Block{TypeName: "OutlineNotice", Slug: "notice", Fields: field.Fields{field.Text("message").Required()}}), outline.Field("secondary", card)}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestEmbeddedGeneratedContractsAndHTTP(t *testing.T) {
	app := embeddedContractApp(t)
	generated, err := goClient(app.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	repeat, _ := goClient(app.Manifest())
	if string(repeat) != string(generated) {
		t.Fatal("unstable Go generation")
	}
	ts, err := typescript.Client(app.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ts), ".Outline<PagesBodyWidgetsWidget[number]>") || !strings.Contains(string(ts), `schema: "card"; uid: string`) {
		t.Fatalf("missing generic/custom discriminator types:\n%s", ts)
	}
	if strings.Count(string(ts), "export type OutlineCard =") != 1 {
		t.Fatal("reusable embedded types duplicated")
	}
	api, err := openAPI(app.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	var contract openAPIDocument
	if err := json.Unmarshal(api, &contract); err != nil {
		t.Fatal(err)
	}
	validator := resolvedResourceSchema(t, contract, "pages")
	target, err := app.Local().Create(t.Context(), "targets", store.Values{"name": store.String("Target")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := app.Local().Create(t.Context(), "pages", store.Values{"body": outline.Value(outline.Widget("card", "", store.Values{"title": store.String("Hello"), "translation": store.String("English"), "target": store.String(target.ID)}))}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "?locale=all"} {
		response := httptest.NewRecorder()
		app.Handler(core.HandlerOptions{}).ServeHTTP(response, httptest.NewRequest("GET", "/api/collections/pages/"+doc.ID+suffix, nil))
		if response.Code != 200 {
			t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
		}
		var envelope struct {
			Doc map[string]any `json:"doc"`
		}
		json.Unmarshal(response.Body.Bytes(), &envelope)
		if err := validator.Validate(envelope.Doc); err != nil {
			t.Fatalf("valid embedded response: %v\n%s", err, response.Body.String())
		}
		body := envelope.Doc["body"].(map[string]any)
		payload := body["outline"].([]any)[0].(map[string]any)["content"].(map[string]any)
		payload["schema"] = "retired"
		if validator.Validate(envelope.Doc) == nil {
			t.Fatal("unknown payload schema accepted")
		}
	}
	root, _ := filepath.Abs("../..")
	dir := t.TempDir()
	files := map[string]string{"go.mod": fmt.Sprintf("module example.com/embedded-consumer\n\ngo 1.25.13\nrequire github.com/riducms/ridu v0.0.0\nreplace github.com/riducms/ridu => %s\n", root), "ridu.generated.go": string(generated), "embedded_test.go": embeddedGoConsumer}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.CommandContext(t.Context(), "go", "test", "-mod=mod", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("external embedded Go consumer: %v\n%s", err, output)
	}
	dir, err = os.MkdirTemp(filepath.Join(root, ".ridu"), "embedded-ts-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	files = map[string]string{"generated.ts": string(ts), "tsconfig.json": `{"extends":"../../tsconfig.base.json","include":["*.ts"]}`, "consumer.ts": embeddedTSConsumer, "outline.d.ts": `declare module "@ridu-test/outline" { export interface Outline<T>{outline: {kind:string;items?:{kind:string;content?:T}[];content?:T}[]} export type OutlineInput<T>=Outline<T>; }`}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd = exec.CommandContext(t.Context(), "bun", filepath.Join(root, "node_modules/typescript/bin/tsc"), "-p", filepath.Join(dir, "tsconfig.json"))
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("external embedded TS consumer: %v\n%s", err, output)
	}
}

const embeddedGoConsumer = `package generated_test
import("encoding/json";"errors";"strings";"testing";g "example.com/embedded-consumer";outline "github.com/riducms/ridu/tests/contracts/embedded_plugin")
func TestEnvelopeCodecs(t *testing.T){
 payload:=g.PagesBodyWidgetsWidgetInputPayload{Value:&g.OutlineCardInput{Title:"Hello"}}
 input:=outline.Document[g.PagesBodyWidgetsWidgetInputPayload]{Outline:[]outline.Node[g.PagesBodyWidgetsWidgetInputPayload]{{Kind:"widget",Content:&payload}}}
 data,err:=json.Marshal(input);if err!=nil{t.Fatal(err)};if !strings.Contains(string(data),"\"schema\":\"card\"")||strings.Contains(string(data),"blockType"){t.Fatal(string(data))}
 var decoded outline.Document[g.PagesBodyWidgetsWidgetInputPayload];if err:=json.Unmarshal(data,&decoded);err!=nil{t.Fatal(err)}
 if _,ok:=decoded.Outline[0].Content.Value.(*g.OutlineCardInput);!ok{t.Fatalf("wrong input: %#v",decoded)}
 var output g.PagesBodyWidgetsWidgetPayload
 if err:=json.Unmarshal([]byte("{\"schema\":\"card\",\"uid\":\"stable\",\"translation\":\"Hi\",\"children\":[{\"blockType\":\"note\",\"_key\":\"child\",\"text\":\"Nested\"}]}"),&output);err!=nil{t.Fatal(err)}
 switch v:=output.Value.(type){case *g.OutlineCard:if v.Key!="stable"||v.Children==nil{t.Fatal(v)};default:t.Fatalf("unexpected variant %T",v)}
 retained,err:=output.Retain();if err!=nil{t.Fatal(err)};retainedBytes,err:=json.Marshal(retained);if err!=nil{t.Fatal(err)};if string(retainedBytes)!="{\"schema\":\"card\",\"uid\":\"stable\"}"{t.Fatalf("retention copied authored fields: %s",retainedBytes)}
 for _,raw:=range []string{"{}","{\"schema\":\"unknown\"}","{\"schema\":\"card\",\"uid\":42}"}{var value g.PagesBodyWidgetsWidgetPayload;err:=json.Unmarshal([]byte(raw),&value);var typed *g.ContractError;if !errors.As(err,&typed){t.Fatalf("missing typed error %s: %v",raw,err)}}
 var localized g.PagesBodyWidgetsWidgetAllLocalesPayload;if err:=json.Unmarshal([]byte("{\"schema\":\"card\",\"uid\":\"stable\",\"translation\":{\"en\":\"Hello\",\"fr\":\"Bonjour\"}}"),&localized);err!=nil{t.Fatal(err)}
 if retained,err:=localized.Retain();err!=nil||retained.Value.BlockKey()!="stable"{t.Fatalf("localized scalar retention: %#v %v",retained,err)}
 for _,empty:=range []g.PagesBodyWidgetsWidgetPayload{{},{Value:(*g.OutlineCard)(nil)}}{_,err:=json.Marshal(empty);var typed *g.ContractError;if !errors.As(err,&typed){t.Fatalf("nil scalar payload encoded: %v",err)}}
 var nullPayload g.PagesBodyWidgetsWidgetPayload;err=json.Unmarshal([]byte("null"),&nullPayload);var typed *g.ContractError;if !errors.As(err,&typed){t.Fatalf("null scalar payload decoded: %v",err)}
}
`
const embeddedTSConsumer = `
import type {PagesCreate,Pages,PagesAllLocales,OutlineCardInput} from "./generated";
const input:PagesCreate={body:{outline:[{kind:"widget",content:{schema:"card",title:"Hello",target:"id",links:[{label:"Read"}]}}]}};
function render(page:Pages){for(const node of page.body?.outline??[]){const block=node.content;if(block?.schema==="card"){block.title?.toUpperCase();block.target;}else if(block?.schema==="notice"){block.message?.toUpperCase();}}}
function localized(page:PagesAllLocales){const node=page.body?.outline[0];if(node?.content?.schema==="card"){node.content.translation?.en;}}
// @ts-expect-error custom discriminator literal
const bad:OutlineCardInput={schema:"notice",title:"bad"};
// @ts-expect-error required child
const absent:OutlineCardInput={schema:"card"};
void input;void render;void localized;void bad;void absent;
`

func TestEmbeddedLiteralPropertyTypeScript(t *testing.T) {
	tree := field.EmbeddedTree{Key: "widgets", Root: []string{"outline"}, Children: "items", Tag: "kind", Cases: []field.EmbeddedTreeCase{{TagValue: "widget", Payload: "content", Discriminator: "schema-kind", Identity: "occurrence-id", Types: []field.Block{field.Block{TypeName: "DashedCard", Slug: "card", Fields: field.Fields{field.Text("title").Required()}}}}}}
	manifest, err := core.Resolve(core.Config{Name: "Literal properties", Plugins: []core.Plugin{outline.Plugin{}}, Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{field.Plugin("body", outline.Key, json.RawMessage(`{}`)).EmbeddedTrees(tree)}}}})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), `"schema-kind": "card"; "occurrence-id"?: string`) {
		t.Fatalf("literal keys were not quoted:\n%s", generated)
	}
	root, _ := filepath.Abs("../..")
	dir, err := os.MkdirTemp(filepath.Join(root, ".ridu"), "embedded-literal-ts-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	files := map[string]string{
		"generated.ts":  string(generated),
		"tsconfig.json": `{"extends":"../../tsconfig.base.json","include":["*.ts"]}`,
		"outline.d.ts":  `declare module "@ridu-test/outline" { export interface Outline<T>{outline: {kind:string;content?:T}[]} export type OutlineInput<T>=Outline<T>; }`,
		"consumer.ts": `import type {DashedCardInput,DashedCard} from "./generated";
const input:DashedCardInput={"schema-kind":"card",title:"Hello"};
const output:DashedCard={"schema-kind":"card","occurrence-id":"stable"};
// @ts-expect-error immutable custom discriminator
const invalid:DashedCardInput={"schema-kind":"unknown",title:"Hello"};
void input;void output;void invalid;`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.CommandContext(t.Context(), "bun", filepath.Join(root, "node_modules/typescript/bin/tsc"), "-p", filepath.Join(dir, "tsconfig.json"))
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("literal property consumer: %v\n%s", err, output)
	}
}

func TestEmbeddedOpenAPIUpdateAcceptsKeylessInsertion(t *testing.T) {
	app := embeddedContractApp(t)
	created, err := app.Local().Create(t.Context(), "pages", store.Values{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := openAPI(app.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	var document openAPIDocument
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	validator := resolvedResourceSchema(t, document, "pagesUpdate")
	payload := `{"body":{"outline":[{"kind":"widget","content":{"schema":"card","title":"Inserted"}}]}}`
	var input map[string]any
	if err := json.Unmarshal([]byte(payload), &input); err != nil {
		t.Fatal(err)
	}
	if err := validator.Validate(input); err != nil {
		t.Fatalf("keyless insertion rejected: %v", err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest("PATCH", "/api/collections/pages/"+created.ID, strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	app.Handler(core.HandlerOptions{}).ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
	}
	var output struct {
		Doc map[string]any `json:"doc"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	content := output.Doc["body"].(map[string]any)["outline"].([]any)[0].(map[string]any)["content"].(map[string]any)
	key, ok := content["uid"].(string)
	if !ok || key == "" {
		t.Fatal("server did not normalize identity")
	}
	keyed := map[string]any{"body": map[string]any{"outline": []any{map[string]any{"kind": "widget", "content": map[string]any{"schema": "card", "uid": key}}}}}
	if err := validator.Validate(keyed); err != nil {
		t.Fatalf("keyed partial update rejected: %v", err)
	}
	missing := map[string]any{"body": map[string]any{"outline": []any{map[string]any{"kind": "widget", "content": map[string]any{"schema": "card"}}}}}
	if validator.Validate(missing) == nil {
		t.Fatal("keyless new payload without required title accepted")
	}
}
