package generate

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/typescript"
	"github.com/riducms/ridu/plugins/richtext"
	outline "github.com/riducms/ridu/tests/contracts/embedded_plugin"
)

func TestEmptyEmbeddedAllowlistGeneratesImpossiblePayload(t *testing.T) {
	manifest, err := core.Resolve(core.Config{Name: "Empty envelope", Plugins: []core.Plugin{outline.Plugin{}}, Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{outline.Field("body")}}}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(manifest.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"types":`) || strings.Contains(string(encoded), `"blockReferences":`) {
		t.Fatalf("empty allowlist must omit both definition alternatives: %s", encoded)
	}
	ts, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ts), "export type PagesBodyWidgetsWidgetBlock = never;") {
		t.Fatal(string(ts))
	}
	generated, err := goClient(manifest)
	if err != nil {
		t.Fatal(err)
	}
	api, err := openAPI(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var contract openAPIDocument
	if err := json.Unmarshal(api, &contract); err != nil {
		t.Fatal(err)
	}
	validate := resolvedResourceSchema(t, contract, "pages")
	ordinary := map[string]any{"id": "one", "createdAt": "2026-09-05T00:00:00Z", "updatedAt": "2026-09-05T00:00:00Z", "body": map[string]any{"outline": []any{map[string]any{"kind": "section", "items": []any{}}}}}
	if err := validate.Validate(ordinary); err != nil {
		t.Fatal(err)
	}
	ordinary["body"].(map[string]any)["outline"] = []any{map[string]any{"kind": "widget", "content": map[string]any{"schema": "arbitrary", "uid": "one"}}}
	if validate.Validate(ordinary) == nil {
		t.Fatal("empty allowlist admitted a payload")
	}
	compileRichTextGoConsumer(t, generated, `package generated_test
import("encoding/json";"testing";g "example.com/richtext-consumer";outline "github.com/riducms/ridu/tests/contracts/embedded_plugin")
func TestEmpty(t *testing.T){var value outline.Document[g.PagesBodyWidgetsWidgetPayload];if err:=json.Unmarshal([]byte("{\"outline\":[{\"kind\":\"section\"}]}"),&value);err!=nil{t.Fatal(err)};if err:=json.Unmarshal([]byte("{\"outline\":[{\"kind\":\"widget\",\"content\":{\"schema\":\"unknown\",\"uid\":\"one\"}}]}"),&value);err==nil{t.Fatal("empty typed allowlist decoded payload")}}
`)
}

func TestRichTextConfiguredGeneratedContracts(t *testing.T) {
	config := core.Config{Name: "Rich text contracts", Plugins: []core.Plugin{richtext.New()}, Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{
		richtext.Field("body", richtext.Config{Blocks: []field.Block{field.Block{TypeName: "ArticleCallout", Slug: "callout", Fields: field.Fields{field.Text("title").Required(), field.Relationship("target", "pages")}}, field.Block{TypeName: "ArticleCTA", Slug: "cta", Labels: field.BlockLabels{Singular: "CTA"}, Fields: field.Fields{field.Text("label").Required()}}}}), richtext.Field("plain"),
	}}}}
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := goClient(manifest)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{".RichTextDocument<PagesBodyBlocksBlock[number]>", ".RichTextDocumentInput<PagesBodyBlocksBlockInput[number]>", "export type PagesPlainBlocksBlockBlock = never;", "blockType: \"callout\""} {
		if !strings.Contains(string(ts), expected) {
			t.Fatalf("missing %s:\n%s", expected, ts)
		}
	}
	api, err := openAPI(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var contract openAPIDocument
	json.Unmarshal(api, &contract)
	validator := resolvedResourceSchema(t, contract, "pages")
	value := map[string]any{"id": "one", "createdAt": "2026-09-05T00:00:00Z", "updatedAt": "2026-09-05T00:00:00Z", "body": map[string]any{"version": 1, "root": map[string]any{"type": "root", "children": []any{map[string]any{"type": "block", "version": 1, "fields": map[string]any{"blockType": "callout", "_key": "one", "title": "Hello"}}}}}}
	if err := validator.Validate(value); err != nil {
		t.Fatal(err)
	}
	node := value["body"].(map[string]any)["root"].(map[string]any)["children"].([]any)[0].(map[string]any)
	node["children"] = []any{}
	if validator.Validate(value) == nil {
		t.Fatal("OpenAPI accepted editor children on atomic block")
	}
	delete(node, "children")
	node["version"] = 2
	if validator.Validate(value) == nil {
		t.Fatal("OpenAPI accepted unknown block version")
	}
	compileRichTextGoConsumer(t, generated, richTextGeneratedGoConsumer)
	compileRichTextTSConsumer(t, ts)
}

func compileRichTextGoConsumer(t *testing.T, generated []byte, consumer string) {
	t.Helper()
	root, _ := filepath.Abs("../..")
	dir := t.TempDir()
	for name, content := range map[string]string{"go.mod": fmt.Sprintf("module example.com/richtext-consumer\n\ngo 1.25.13\nrequire github.com/riducms/ridu v0.0.0\nreplace github.com/riducms/ridu => %s\n", root), "generated.go": string(generated), "consumer_test.go": consumer} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.CommandContext(t.Context(), "go", "test", "-mod=mod", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("typed rich-text Go consumer: %v\n%s", err, output)
	}
}
func compileRichTextTSConsumer(t *testing.T, generated []byte) {
	compileRichTextTSConsumerSource(t, generated, richTextGeneratedTSConsumer)
}

func compileRichTextTSConsumerSource(t *testing.T, generated []byte, consumer string) {
	t.Helper()
	root, _ := filepath.Abs("../..")
	if err := os.MkdirAll(filepath.Join(root, ".ridu"), 0755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(filepath.Join(root, ".ridu"), "richtext-types-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	for name, content := range map[string]string{"generated.ts": string(generated), "tsconfig.json": `{"extends":"../../tsconfig.base.json","include":["*.ts"]}`, "consumer.ts": consumer} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.CommandContext(t.Context(), "bun", filepath.Join(root, "node_modules/typescript/bin/tsc"), "-p", filepath.Join(dir, "tsconfig.json"))
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("typed rich-text TS consumer: %v\n%s", err, output)
	}
}

const richTextGeneratedGoConsumer = `package generated_test
import("encoding/json";"testing";g "example.com/richtext-consumer";richtext "github.com/riducms/ridu/plugins/richtext")
func TestTyped(t *testing.T){payload:=g.PagesBodyBlocksBlockInputPayload{Value:&g.ArticleCalloutInput{Title:"Hello"}};value:=richtext.Document[g.PagesBodyBlocksBlockInputPayload]{Version:1,Root:richtext.Node[g.PagesBodyBlocksBlockInputPayload]{Type:"root",Children:[]richtext.Node[g.PagesBodyBlocksBlockInputPayload]{{Type:"block",Version:1,Fields:&payload}}}};data,err:=json.Marshal(value);if err!=nil{t.Fatal(err)};var decoded richtext.Document[g.PagesBodyBlocksBlockInputPayload];if err:=json.Unmarshal(data,&decoded);err!=nil{t.Fatal(err)};if _,ok:=decoded.Root.Children[0].Fields.Value.(*g.ArticleCalloutInput);!ok{t.Fatalf("untyped payload %T",decoded.Root.Children[0].Fields.Value)};var output richtext.Document[g.PagesBodyBlocksBlockPayload];if err:=json.Unmarshal([]byte("{\"version\":1,\"root\":{\"type\":\"root\",\"children\":[{\"type\":\"block\",\"version\":1,\"fields\":{\"blockType\":\"callout\",\"_key\":\"one\"}}]}}"),&output);err!=nil{t.Fatal(err)};if _,err:=richtext.RenderDocument(output,func(payload g.PagesBodyBlocksBlockPayload)(string,error){switch payload.Value.(type){case *g.ArticleCallout:return "callout",nil;case *g.ArticleCTA:return "CTA",nil;default:t.Fatal("unknown variant");return "",nil}},nil);err!=nil{t.Fatal(err)}}
`
const richTextGeneratedTSConsumer = `
import type { PagesCreate, Pages, ArticleCalloutInput } from "./generated";
import type { RichTextBlockRenderers } from "@riducms/plugin-richtext/document";
import {renderRichTextHTML} from "@riducms/plugin-richtext/render";
const input: PagesCreate={body:{version:1,root:{type:"root",children:[{type:"block",version:1,fields:{blockType:"callout",title:"Hello",target:"id"}}]}}};
function output(page: Pages){for(const node of page.body?.root.children??[]){if(node.type==="block"&&node.fields.blockType==="callout"){node.fields.title?.toUpperCase();node.fields.target;}}}
const renderers:RichTextBlockRenderers<NonNullable<Pages["body"]>["root"]["children"][number] extends infer Node ? Node extends {fields:infer Payload} ? Payload extends {blockType:string} ? Payload : never : never : never,string>={callout:(block)=>block.title??"",cta:(block)=>block.label??""};
function render(page:Pages){if(page.body)renderRichTextHTML(page.body,{blocks:renderers});}
// @ts-expect-error required authoring title
const missing:ArticleCalloutInput={blockType:"callout"};
// @ts-expect-error declared variant only
const unknown:ArticleCalloutInput={blockType:"unknown",title:"Hello"};
// @ts-expect-error blocks are not permitted in plain rich text
const arbitrary:PagesCreate={plain:{version:1,root:{type:"root",children:[{type:"block",version:1,fields:{blockType:"anything"}}]}}};
`
