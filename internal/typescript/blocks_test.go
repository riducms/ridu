package typescript_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/typescript"
)

func TestNamedBlocksExternalTypeScriptConsumer(t *testing.T) {
	hero := field.Block{TypeName: "Hero", Slug: "hero", Fields: field.Fields{field.Text("heading").Required().Localized(), field.Text("subtitle"), field.Relationship("author", "authors"), field.Upload("image", "assets"), field.Group("settings", field.Fields{field.Checkbox("wide").Required()}).Required(), field.Array("links", field.Fields{field.Text("label").Required()}), field.Blocks("children", field.Block{Slug: "note", Fields: field.Fields{field.Text("body").Required()}})}}

	manifest, err := ridu.Resolve(ridu.Config{Name: "Named blocks", Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []ridu.Collection{
		{Slug: "authors", Fields: field.Fields{field.Text("name").Localized()}},
		{Slug: "assets", Upload: true, Fields: field.Fields{field.Text("alt")}},
		{Slug: "pages", Fields: field.Fields{field.Blocks("layout", hero, field.Block{TypeName: "CTA", Slug: "cta", Labels: field.BlockLabels{Singular: "CTA"}, Fields: field.Fields{field.Text("url").Required()}}).Required().MinRows(1).MaxRows(5), field.Blocks("secondary", hero), field.Blocks("translated", hero).Localized()}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	again, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, again) {
		t.Fatal("generation is not stable")
	}
	if strings.Count(string(generated), "export type Hero =") != 1 {
		t.Fatal("reusable definition emitted more than once")
	}
	// An inline block without a TypeName is named after its slug wherever it is declared.
	if strings.Count(string(generated), "export type Note =") != 1 || strings.Contains(string(generated), "HeroChildrenNote") {
		t.Fatal("nested unnamed variant lacks its slug-derived definition name")
	}
	root := moduleRoot(t)
	if _, err := os.Stat(filepath.Join(root, "node_modules", "typescript", "bin", "tsc")); err != nil {
		t.Skip("install workspace dependencies to compile external consumer")
	}
	if err := os.MkdirAll(filepath.Join(root, ".ridu"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(filepath.Join(root, ".ridu"), "blocks-ts-consumer-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	files := map[string][]byte{"generated.ts": generated, "tsconfig.json": []byte(`{"extends":"../../tsconfig.base.json","include":["*.ts"]}`), "consumer.ts": []byte(`
import { createClient } from "./generated";
import type { Hero, HeroInput, HeroUpdate, HeroAllLocales, PagesLayout, PagesLayoutInput, PagesLayoutUpdate, PagesAllLocales, PagesPopulate, PagesValidationPath, PagesWhere, CTA } from "./generated";
const input: HeroInput = { blockType: "hero", heading: "Hello", settings: { wide: true }, author: "author-id", image: "asset-id", links: [{label:"Home"}], children: [{blockType:"note",body:"Nested"}] };
const rows: PagesLayoutInput = [input, {blockType:"cta",url:"/start"}];
const patch: HeroUpdate = {blockType:"hero",_key:"stable",subtitle:"Edited",settings:{},links:[{_key:"existing-link"}],children:[{blockType:"note",_key:"existing-note"}]};
const mixed: PagesLayoutUpdate = [patch,input,{blockType:"cta",url:"/new"}];
// @ts-expect-error a retained row patch must provide identity
const keylessPatch: HeroUpdate = {blockType:"hero",subtitle:"Edited"};
// @ts-expect-error required child cannot be explicitly cleared
const nullPatch: HeroUpdate = {blockType:"hero",_key:"stable",heading:null};
// @ts-expect-error new keyless rows still require authoring fields
const newInvalid: PagesLayoutUpdate = [{blockType:"hero",subtitle:"Missing heading"}];
// @ts-expect-error new array rows still require their authored fields
const newLinkInvalid: HeroUpdate = {blockType:"hero",_key:"stable",links:[{}]};
const redacted: Hero = {blockType:"hero",_key:"stable"};
const populated: Hero = {blockType:"hero",_key:"stable",author:{id:"author-id",createdAt:"now",updatedAt:"now",name:"Author"},image:{id:"asset-id",createdAt:"now",updatedAt:"now",_revision:1},subtitle:null};
const all: HeroAllLocales = {blockType:"hero",_key:"stable",heading:{en:"Hello",fr:"Bonjour"}};
const localizedAncestor: PagesAllLocales = {id:"page",createdAt:"now",updatedAt:"now",translated:{en:[{blockType:"hero",_key:"stable",heading:"Hello",author:{id:"author",createdAt:"now",updatedAt:"now",name:{en:"Author"}}}]}};
// @ts-expect-error output identity is required
const keyless: Hero = {blockType:"hero"};
// @ts-expect-error literal discriminator belongs to its concrete variant
const mismatch: HeroInput = {blockType:"cta",heading:"Hello",settings:{wide:true}};
// @ts-expect-error unknown discriminator
const unknown: PagesLayoutInput = [{blockType:"retired"}];
// @ts-expect-error required input child is not optional
const absentHeading: HeroInput = {blockType:"hero",settings:{wide:true}};
// @ts-expect-error required input child is not nullable
const nullHeading: HeroInput = {blockType:"hero",heading:null,settings:{wide:true}};
// @ts-expect-error inputs use canonical relationship IDs
const populatedInput: HeroInput = {...input,author:populated.author};
// @ts-expect-error all-locales localized child uses a map
const singleInAll: HeroAllLocales = {blockType:"hero",_key:"stable",heading:"Hello"};
// @ts-expect-error a localized container already owns descendant locale values
const doubleLocale: PagesAllLocales = {id:"page",createdAt:"now",updatedAt:"now",translated:{en:[{blockType:"hero",_key:"stable",heading:{en:"Hello"}}]}};
const nestedWhere: PagesWhere = {"layout.hero.children.note.body":{like:"Nested"},"layout.hero.settings.wide":{equals:true},"secondary.hero.author":{in:["author-id"]},"translated.hero.links.label":{exists:true},or:[{"layout.cta.url":{equals:"/start"}},{"layout.hero.children":{exists:false}}]};
// @ts-expect-error nested block filters keep their field operators
const wrongNested: PagesWhere = {"layout.hero.children.note.body":{equals:1}};
// @ts-expect-error unknown nested block fields are not queryable
const unknownNested: PagesWhere = {"layout.hero.children.note.missing":{exists:true}};
// @ts-expect-error block filter paths name the block slug
const slugless: PagesWhere = {"layout.heading":{exists:true}};
const nestedPopulate: PagesPopulate = {"layout.hero.author":true,"translated.hero.image":{select:{id:true}}};
// @ts-expect-error population paths name relationship fields
const notRelationship: PagesPopulate = {"layout.hero.subtitle":true};
const issuePaths: PagesValidationPath[] = ["layout.0.heading","layout.0.heading.fr","translated.0.heading","translated.en.0.heading","layout.1.children.2.body","layout.0.blockType","layout.0.url"];
// @ts-expect-error a localized ancestor already selects the locale of its descendants
const doubleLocalePath: PagesValidationPath = "translated.en.0.heading.fr";
function render(blocks: PagesLayout): string { return blocks.map(block=> { switch(block.blockType) { case "hero": return block.heading ?? ""; case "cta": return block.url ?? ""; default: {const exhaustive:never=block;return exhaustive;} } }).join(""); }
const client = createClient({baseURL:"http://localhost:3000"});
void client.create("pages",{layout:rows});
void client.update("pages","page-id",{layout:mixed});
void render([redacted,populated]);void all;void localizedAncestor;void nestedWhere;void nestedPopulate;void issuePaths;
`)}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("bun", filepath.Join(root, "node_modules", "typescript", "bin", "tsc"), "-p", filepath.Join(dir, "tsconfig.json"))
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("external TypeScript consumer: %v\n%s", err, output)
	}
}
