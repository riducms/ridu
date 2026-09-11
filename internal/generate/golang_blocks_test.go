package generate

import (
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
)

func TestGeneratedGoBlocksExternalCodec(t *testing.T) {
	hero := field.Block{TypeName: "Hero", Slug: "hero", Fields: field.Fields{field.Text("heading").Required().Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }}).Localized(), field.Number("amount").Localized(), field.Text("subtitle").Localized(), field.Text("key"), field.Text("blockKey"), field.Select("tone", "light", "dark"), field.Radio("alignment", "start", "end"), field.MultiSelect("tags", "news", "events"), field.Group("style", field.Fields{field.Text("tone")}), field.Array("links", field.Fields{field.Text("label").Required(), field.Number("amount")}), field.Relationship("target", "pages"), field.Upload("image", "media"), field.PolymorphicRelationship("link", "pages", "campaigns"), field.Blocks("children", field.Block{TypeName: "Note", Slug: "note", Fields: field.Fields{field.Text("text")}})}}
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app, err := core.New(core.Config{Storage: backend, StorageNamespace: "typed-blocks", Name: "Typed Blocks", Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []core.Collection{
		{Slug: "pages", Fields: field.Fields{field.Blocks("layout", hero, field.Block{TypeName: "CTA", Slug: "cta", Labels: field.BlockLabels{Singular: "CTA"}, Fields: field.Fields{field.Text("label").Required()}}), field.Blocks("translatedLayout", hero).Localized()}},
		{Slug: "media", Upload: true},
		{Slug: "campaigns", Fields: field.Fields{field.Text("title"), field.Group("content", field.Fields{field.Blocks("layout", field.Block{TypeName: "CTA", Slug: "cta", Labels: field.BlockLabels{Singular: "CTA"}, Fields: field.Fields{field.Text("label").Required()}})})}},
	}, Globals: []core.Global{{Slug: "pages", Label: "Site pages", Fields: field.Fields{field.Text("globalOnly")}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	generated, err := goClient(app.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := goClient(app.Manifest())
	if err != nil || string(generated) != string(repeat) {
		t.Fatalf("unstable generation: %v", err)
	}
	if strings.Contains(string(generated), "[]map[string]any") {
		t.Fatal("map block fallback remains")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	files := map[string]string{"go.mod": fmt.Sprintf("module example.com/block-consumer\n\ngo 1.25.13\nrequire github.com/riducms/ridu v0.0.0\nreplace github.com/riducms/ridu => %s\n", root), "ridu.generated.go": string(generated), "blocks_test.go": generatedBlocksConsumer, "ergonomics_test.go": generatedBlocksErgonomicsConsumer, "array_retention_test.go": generatedArrayRetentionConsumer}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.CommandContext(t.Context(), "go", "test", "-mod=mod", "./...")
	cmd.Dir = dir
	server := httptest.NewServer(app.Handler(core.HandlerOptions{}))
	defer server.Close()
	cmd.Env = append(os.Environ(), "GOWORK=off", "RIDU_TEST_URL="+server.URL)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("external generated block consumer: %v\n%s\n%s", err, output, generated)
	}

	if err := os.WriteFile(filepath.Join(dir, "invalid_test.go"), []byte(`package generated_test
 import g "example.com/block-consumer"
 import "github.com/riducms/ridu/core"
 var _ = &g.HeroInput{BlockType:"cta"}
 var _ = g.PagesLayoutInput{struct{}{}}
 var _ = &g.HeroInput{Heading:42}
 var _ = g.PagesLayout{g.Hero{}}
 var _ = g.PagesLayoutInput{g.HeroInput{}}
 var _ = g.PagesLayoutUpdate{g.HeroUpdate{}}
 var _ = &g.HeroInput{Tone:core.Set(g.HeroAlignmentStart)}
 `), 0600); err != nil {
		t.Fatal(err)
	}
	invalid := exec.CommandContext(t.Context(), "go", "test", "-mod=mod", "./...")
	invalid.Dir = dir
	invalid.Env = append(os.Environ(), "GOWORK=off")
	diagnostic, err := invalid.CombinedOutput()
	if err == nil {
		t.Fatal("invalid discriminator/variant/child types compiled")
	}
	for _, fragment := range []string{"unknown field BlockType", "does not implement", "cannot use 42", "g.Hero{}", "g.HeroInput{}", "g.HeroUpdate{}", "*core.Input[generated.HeroAlignment]"} {
		if !strings.Contains(string(diagnostic), fragment) {
			t.Fatalf("missing compile rejection %q: %s", fragment, diagnostic)
		}
	}
}

const generatedBlocksConsumer = `package generated_test
import("encoding/json";"bytes";"net/http";"os";"errors";"strings";"testing"; g "example.com/block-consumer"; "github.com/riducms/ridu/core")
func TestCodecs(t *testing.T){
 if g.HeroBlockType != "hero" { t.Fatal("discriminator constant") }
 rows:=g.PagesLayoutInput{&g.HeroInput{Heading:"Hello",Image:core.Set("asset"),Subtitle:core.Null[string](),Link:core.Set(g.HeroLinkReferenceInput{Value:g.HeroLinkReferencePagesInput{ID:"page"}}),Style:core.Set(g.HeroStyleInput{Tone:core.Set("dark")}),Links:core.Set([]g.HeroLinksRowInput{{Key:"link",Label:"Read"}}),Children:core.Set(g.HeroChildrenInput{&g.NoteInput{Text:core.Set("nested")}})},&g.CTAInput{Label:"Read more"}}
 encoded,err:=json.Marshal(rows);if err!=nil{t.Fatal(err)}
 var input g.PagesLayoutInput;if err:=json.Unmarshal(encoded,&input);err!=nil{t.Fatal(err)}
 round,err:=json.Marshal(input);if err!=nil||string(round)!=string(encoded){t.Fatalf("input roundtrip %s -> %s: %v",encoded,round,err)}
 output:=` + "`" + `[{"blockType":"hero","_key":"one","heading":"Hello","image":{"id":"asset"},"target":{"id":"page","layout":[{"blockType":"cta","_key":"nested","label":"Nested"}]},"children":[{"blockType":"note","_key":"child","text":"Nested"}]},{"blockType":"cta","_key":"two"}]` + "`" + `
 var blocks g.PagesLayout;if err:=json.Unmarshal([]byte(output),&blocks);err!=nil{t.Fatal(err)}
 hero,ok:=blocks[0].(*g.Hero);if !ok||hero.Key!="one"||hero.Heading==nil||*hero.Heading!="Hello"||hero.Target.Value.Document==nil||hero.Image.Value.Document==nil{t.Fatalf("bad typed hero: %#v",blocks[0])}
 if hero.Target.Value.Document.Layout==nil{t.Fatal("populated target blocks omitted")}
 if _,err:=json.Marshal(blocks);err!=nil{t.Fatal(err)}
 var nullable g.PagesLayout;if err:=json.Unmarshal([]byte(` + "`" + `[{"blockType":"hero","_key":"nullable","subtitle":null,"style":{"tone":null},"links":null}]` + "`" + `),&nullable);err!=nil{t.Fatal(err)}
 nullJSON,err:=json.Marshal(nullable);if err!=nil||!strings.Contains(string(nullJSON),` + "`" + `"subtitle":null` + "`" + `)||!strings.Contains(string(nullJSON),` + "`" + `"tone":null` + "`" + `)||!strings.Contains(string(nullJSON),` + "`" + `"links":null` + "`" + `){t.Fatalf("nullable output roundtrip: %s %v",nullJSON,err)}

 var all g.PagesLayoutAllLocales;if err:=json.Unmarshal([]byte(` + "`" + `[{"blockType":"hero","_key":"one","heading":{"en":"Hello","fr":"Bonjour"},"subtitle":{"en":null,"fr":""}}]` + "`" + `),&all);err!=nil{t.Fatal(err)}
 if got:=all[0].(*g.HeroAllLocales);got.Heading==nil||(*got.Heading)["fr"]!="Bonjour"{t.Fatal("locale map lost")}
 if got:=all[0].(*g.HeroAllLocales);got.Subtitle==nil||(*got.Subtitle.Value)["en"]!=nil||(*got.Subtitle.Value)["fr"]==nil||*(*got.Subtitle.Value)["fr"]!=""{t.Fatal("null or empty locale lost")}
 for _,raw:=range []string{` + "`" + `[{"blockType":"unknown","_key":"key"}]` + "`" + `,` + "`" + `[{"_key":"key"}]` + "`" + `,` + "`" + `[{"blockType":"hero","_key":"key","heading":42}]` + "`" + `,` + "`" + `[{"blockType":"hero"}]` + "`" + `,` + "`" + `[{"blockType":"hero","_key":" "}]` + "`" + `}{var rows g.PagesLayout;err:=json.Unmarshal([]byte(raw),&rows);var typed *g.ContractError;if !errors.As(err,&typed){t.Fatalf("wanted typed error for %s: %v",raw,err)}}
 var contextual g.PageAllLocales;if err:=json.Unmarshal([]byte(` + "`" + `{"translatedLayout":{"en":[{"blockType":"hero","_key":"key","heading":"Hello","target":{"id":"target","translatedLayout":{"fr":[]}}}]}}` + "`" + `),&contextual);err!=nil{t.Fatal(err)}
 h:=(*(*contextual.TranslatedLayout)["en"])[0].(*g.HeroAllLocalesValue);if h.Target.Value.Document.TranslatedLayout==nil{t.Fatal("localized container lost all-locales populated target")}
 var absent g.PagesLayout;if err:=json.Unmarshal([]byte("null"),&absent);err!=nil||absent!=nil{t.Fatal("null list")}
 var reference g.Reference[g.Page];if err:=json.Unmarshal([]byte("null"),&reference);err==nil{t.Fatal("reference accepted null")}
 var poly g.HeroLinkReferenceInput;for _,raw:=range []string{` + "`" + `{"relationTo":"retired","id":"x"}` + "`" + `,` + "`" + `{"relationTo":"pages","id":null}` + "`" + `}{if err:=json.Unmarshal([]byte(raw),&poly);err==nil{t.Fatal("invalid polymorphic reference")}}
 var nilHero *g.HeroInput;for _,rows:=range []g.PagesLayoutInput{{nil},{nilHero}}{if _,err:=json.Marshal(rows);err==nil{t.Fatal("nil variant encoded")}}
 var patch g.PagesLayoutUpdate;if err:=json.Unmarshal([]byte(` + "`" + `[{"blockType":"hero","_key":"existing","links":[{"_key":"link"}]}]` + "`" + `),&patch);err!=nil{t.Fatal(err)}
 if _,ok:=patch[0].(*g.HeroUpdate);!ok{t.Fatal("keyed patch not typed")};if _,err:=json.Marshal(g.PagesLayoutUpdate{&g.HeroUpdate{}});err==nil{t.Fatal("keyless partial patch encoded")}
 for _,raw:=range []string{` + "`" + `[{"blockType":"hero","_key":"existing","unknown":"bad"}]` + "`" + `,` + "`" + `[{"blockType":"hero","heading":"valid","style":{"unknown":true}}]` + "`" + `,` + "`" + `[{"blockType":"hero","_key":"existing","links":[{}]}]` + "`" + `}{if err:=json.Unmarshal([]byte(raw),&patch);err==nil{t.Fatalf("accepted malformed patch %s",raw)}}
 var invalid g.PagesLayoutInput;for _,raw:=range []string{` + "`" + `[{"blockType":"hero"}]` + "`" + `,` + "`" + `[{"blockType":"hero","heading":null}]` + "`" + `}{if err:=json.Unmarshal([]byte(raw),&invalid);err==nil{t.Fatalf("accepted required omission: %s",raw)}}
 encoded,_=json.Marshal(&g.HeroInput{Heading:"Hello"});if !strings.Contains(string(encoded),` + "`" + `"blockType":"hero"` + "`" + `){t.Fatal("constant discriminator missing")}
}
func TestAPIRedactedKeyedUpdate(t *testing.T){
 base:=os.Getenv("RIDU_TEST_URL");if base==""{t.Fatal("missing API fixture")}
 body,err:=json.Marshal(g.PageCreate{Layout:core.Set(g.PagesLayoutInput{&g.HeroInput{Heading:"Hidden required"},&g.CTAInput{Label:"Untouched"}})});if err!=nil{t.Fatal(err)}
 response,err:=http.Post(base+"/api/collections/pages","application/json",bytes.NewReader(body));if err!=nil{t.Fatal(err)};defer response.Body.Close()
 var result struct{Data g.Page ` + "`" + `json:"doc"` + "`" + `};if err:=json.NewDecoder(response.Body).Decode(&result);err!=nil{t.Fatal(err)};if response.StatusCode!=201{t.Fatalf("create status %d",response.StatusCode)}
 hero:=(*result.Data.Layout)[0].(*g.Hero);if hero.Key==""||hero.Heading!=nil{t.Fatal("identity/redaction contract")}
 untouchedKey:=(*result.Data.Layout)[1].BlockKey();retained,err:=result.Data.Layout.Retain();if err!=nil{t.Fatal(err)};retained[0].(*g.HeroUpdate).Subtitle=core.Set("Updated");body,err=json.Marshal(g.PageUpdate{Layout:core.Set(retained)});if err!=nil{t.Fatal(err)}
 request,err:=http.NewRequest(http.MethodPatch,base+"/api/collections/pages/"+result.Data.ID,bytes.NewReader(body));if err!=nil{t.Fatal(err)};request.Header.Set("Content-Type","application/json");response,err=http.DefaultClient.Do(request);if err!=nil{t.Fatal(err)};defer response.Body.Close();if response.StatusCode!=200{t.Fatalf("keyed update status %d",response.StatusCode)}
 if err:=json.NewDecoder(response.Body).Decode(&result);err!=nil{t.Fatal(err)};if len(*result.Data.Layout)!=2||(*result.Data.Layout)[1].BlockKey()!=untouchedKey{t.Fatal("retention lost untouched variant")};updated:=(*result.Data.Layout)[0].(*g.Hero);subtitle,ok:=updated.Subtitle.Get();if updated.Key!=hero.Key||updated.Heading!=nil||!ok||subtitle!="Updated"{t.Fatal("keyed redacted edit failed")}
}

`
