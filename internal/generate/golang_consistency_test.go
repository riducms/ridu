package generate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
)

func TestGeneratedGoContractsDoNotDependOnUnrelatedBlocks(t *testing.T) {
	var fixtures []generatedGoConsumerFixture
	for _, localized := range []bool{false, true} {
		t.Run(fmt.Sprint(localized), func(t *testing.T) {
			config := core.Config{Name: "Stable contracts", Collections: []core.Collection{
				{Slug: "authors", Fields: field.Fields{field.Text("name").Localized()}},
				{Slug: "posts", Fields: field.Fields{field.Relationship("author", "authors"), field.Relationships("authors", "authors"), field.PolymorphicRelationship("target", "authors", "posts"), field.Group("meta", field.Fields{field.Relationship("author", "authors")}), field.Array("links", field.Fields{field.Relationship("author", "authors")})}},
			}, Globals: []core.Global{{Slug: "site", Fields: field.Fields{field.Relationship("author", "authors")}}}}
			if localized {
				config.Localization = core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}
			}
			// Non-localized projects must not author localized fields.
			if !localized {
				config.Collections[0].Fields = field.Fields{field.Text("name")}
			}
			manifest, err := core.Resolve(config)
			if err != nil {
				t.Fatal(err)
			}
			before, err := goClient(manifest)
			if err != nil {
				t.Fatal(err)
			}
			config.Collections = append(config.Collections, core.Collection{Slug: "pages", Fields: field.Fields{field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading")}})}})
			manifest, err = core.Resolve(config)
			if err != nil {
				t.Fatal(err)
			}
			after, err := goClient(manifest)
			if err != nil {
				t.Fatal(err)
			}
			declarations := goDeclarations(t, after)
			for name, source := range goDeclarations(t, before) {
				if declarations[name] != source {
					t.Errorf("adding an unrelated block changed %s", name)
				}
			}
			if !localized && (strings.Contains(string(before), "AllLocales") || strings.Contains(string(after), "AllLocales")) {
				t.Fatal("nonlocalized schema exposes unused locale families")
			}
			fixtures = append(fixtures,
				generatedGoConsumerFixture{name: fmt.Sprintf("localized_%t_before", localized), generated: before, consumer: generatedStableReadConsumer},
				generatedGoConsumerFixture{name: fmt.Sprintf("localized_%t_after", localized), generated: after, consumer: generatedStableReadConsumer},
			)
		})
	}
	if !t.Failed() {
		runGeneratedGoConsumers(t, fixtures)
	}
}

type generatedGoConsumerFixture struct {
	name      string
	generated []byte
	consumer  string
}

// Independent generated packages share one module/compiler invocation. Go still
// reports every package's contract failures, including the before/after variant.
func runGeneratedGoConsumers(t *testing.T, fixtures []generatedGoConsumerFixture) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	module := fmt.Sprintf("module example.com/stable-consumer\n\ngo 1.25.13\n\nrequire github.com/riducms/ridu v0.0.0\nreplace github.com/riducms/ridu => %s\n", root)
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(module), 0600); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		target := filepath.Join(directory, fixture.name)
		if err := os.Mkdir(target, 0700); err != nil {
			t.Fatal(err)
		}
		consumer := strings.ReplaceAll(fixture.consumer, `"example.com/stable-consumer"`, `"example.com/stable-consumer/`+fixture.name+`"`)
		for name, data := range map[string][]byte{"ridu.generated.go": fixture.generated, "consumer_test.go": []byte(consumer)} {
			if err := os.WriteFile(filepath.Join(target, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	command := exec.CommandContext(t.Context(), "go", "test", "-p=2", "-mod=mod", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated consumer packages: %v\n%s", err, output)
	}
}

func runGeneratedGoConsumer(t *testing.T, generated []byte, consumer string) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	for name, data := range map[string]string{
		"go.mod":            fmt.Sprintf("module example.com/stable-consumer\n\ngo 1.25.13\n\nrequire github.com/riducms/ridu v0.0.0\nreplace github.com/riducms/ridu => %s\n", root),
		"ridu.generated.go": string(generated), "consumer_test.go": consumer,
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.CommandContext(t.Context(), "go", "test", "-mod=mod", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated consumer: %v\n%s", err, output)
	}
}

const generatedStableReadConsumer = `package generated_test
import("encoding/json";"testing";"path/filepath";"github.com/riducms/ridu/core";"github.com/riducms/ridu/field";"github.com/riducms/ridu/query";"github.com/riducms/ridu/adapters/sqlite";g "example.com/stable-consumer")
func TestStableRead(t *testing.T){
 var post g.Post
 for _,raw:=range []string{
  "{\"author\":\"a\",\"authors\":[\"a\"],\"target\":{\"relationTo\":\"authors\",\"id\":\"a\"}}",
  "{\"author\":{\"id\":\"a\",\"name\":\"Ada\"},\"authors\":[{\"id\":\"a\"}],\"target\":{\"relationTo\":\"authors\",\"id\":{\"id\":\"a\"}}}",
 } {
  if err:=json.Unmarshal([]byte(raw),&post);err!=nil{t.Fatal(err)}
  if post.Author==nil||post.Author.ID!="a"||post.Authors==nil||post.Authors[0].ID!="a"{t.Fatal("reference IDs lost")}
  target:=post.Target.Value.(g.PostTargetReferenceAuthors)
  if target.ID.ID!="a"{t.Fatal("polymorphic reference lost")}
 }
 if post.Author.Document==nil||post.Author.Document.Name==nil||*post.Author.Document.Name!="Ada"{t.Fatal("population lost")}
 for _,raw:=range []string{"{\"author\":null}","{\"meta\":{\"author\":null}}","{}"} {
  var update g.PostUpdate
  if err:=json.Unmarshal([]byte(raw),&update);err!=nil{t.Fatal(err)}
  data,err:=json.Marshal(update);if err!=nil||string(data)!=raw{t.Fatalf("mutation presence lost: %s -> %s: %v",raw,data,err)}
 }
}
func TestPopulatedLocalReadsWithoutBlocks(t *testing.T){
 ctx:=t.Context()
 config:=core.Config{Name:"Population",Collections:[]core.Collection{
  {Slug:"authors",Fields:field.Fields{field.Text("name")}},
  {Slug:"posts",Fields:field.Fields{field.Relationship("author", "authors")}},
 },Globals:[]core.Global{{Slug:"site",Fields:field.Fields{field.Relationship("author", "authors")}}}}
 backend,err:=sqlite.Open(ctx,filepath.Join(t.TempDir(),"read.sqlite"));if err!=nil{t.Fatal(err)};defer backend.Close()
 manifest,err:=core.Resolve(config);if err!=nil{t.Fatal(err)}
 if err:=backend.Migrate(ctx,manifest);err!=nil{t.Fatal(err)}
 app,err:=core.New(config,backend);if err!=nil{t.Fatal(err)}
 author,err:=g.AuthorsCollection.With(app.Local()).Create(ctx,g.AuthorCreate{Name:core.Set("Ada")},core.TypedMutationOptions{});if err!=nil{t.Fatal(err)}
 posts:=g.PostsCollection.With(app.Local())
 created,err:=posts.Create(ctx,g.PostCreate{Author:core.Set(author.ID)},core.TypedMutationOptions{});if err!=nil{t.Fatal(err)}
 path,err:=query.ParsePath("author");if err!=nil{t.Fatal(err)}
 populate:=[]query.Population{{Path:path,Depth:1}}
 found,err:=posts.Find(ctx,created.ID,core.TypedReadOptions{Populate:populate});if err!=nil{t.Fatal(err)}
 if found.Author==nil||found.Author.Document==nil||found.Author.Document.ID!=author.ID{t.Fatal("Find did not populate through ordinary collection")}
 listed,err:=posts.List(ctx,core.TypedListOptions{Populate:populate});if err!=nil{t.Fatal(err)}
 if listed.Total!=1||len(listed.Documents)!=1||listed.Documents[0].Author.Document==nil{t.Fatal("List did not populate")}
 site:=g.SiteGlobal.With(app.Local())
 if _,err:=site.Update(ctx,g.SiteUpdate{Author:core.Set(author.ID)},core.TypedMutationOptions{});err!=nil{t.Fatal(err)}
 global,err:=site.Find(ctx,core.TypedReadOptions{Populate:populate});if err!=nil{t.Fatal(err)}
 if global.Author==nil||global.Author.Document==nil{t.Fatal("Global Find did not populate")}
}
`

func TestGoInitialisms(t *testing.T) {
	for input, want := range map[string]string{"url": "URL", "apiURL": "APIURL", "httpServerURL": "HTTPServerURL", "userID": "UserID", "id": "ID", "iD": "ID", "json_value": "JSONValue", "HTMLBody": "HTMLBody", "cta": "Cta", "heading": "Heading"} {
		if got := exportedGoIdentifier(input); got != want {
			t.Errorf("%s => %s, want %s", input, got, want)
		}
	}
	manifest, err := core.Resolve(core.Config{Name: "Initialisms", Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{field.Text("url"), field.Text("userID"), field.Text("jsonData"), field.Blocks("layout", field.Block{TypeName: "Hero", Slug: "hero", Fields: field.Fields{field.Text("apiUrl"), field.Text("httpUrl"), field.Text("httpURL"), field.Text("httpUrlField")}})}}}})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := goClient(manifest)
	if err != nil {
		t.Fatal(err)
	}
	compileGeneratedGo(t, generated)
	for _, fragment := range []string{"URL", "UserID", "JSONData", "APIURL", "HTTPURLField2"} {
		if !strings.Contains(string(generated), fragment) {
			t.Errorf("missing %s", fragment)
		}
	}
}

func TestGoInitialismNestedContractsDoNotOverwrite(t *testing.T) {
	hero := field.Block{TypeName: "Hero", Slug: "hero", Fields: field.Fields{field.Group("httpUrl", field.Fields{field.Text("first")}), field.Group("httpURL", field.Fields{field.Text("second")}), field.Array("apiUrl", field.Fields{field.Text("first")}), field.Array("apiURL", field.Fields{field.Text("second")})}}

	manifest, err := core.Resolve(core.Config{Name: "Nested initialisms", Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("layout", hero)}}}})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := goClient(manifest)
	if err != nil {
		t.Fatal(err)
	}
	runGeneratedGoConsumer(t, generated, `package generated_test
import("encoding/json";"reflect";"testing";g "example.com/stable-consumer")
func TestNestedRoundTrip(t *testing.T){
 raw:="{\"_key\":\"hero\",\"blockType\":\"hero\",\"httpUrl\":{\"first\":\"kept\"},\"httpURL\":{\"second\":\"also kept\"},\"apiUrl\":[{\"_key\":\"one\",\"first\":\"kept\"}],\"apiURL\":[{\"_key\":\"two\",\"second\":\"also kept\"}]}"
 var block g.Hero
 if err:=json.Unmarshal([]byte(raw),&block);err!=nil{t.Fatal(err)}
 first,_:=block.HTTPURL.Get();second,_:=block.HTTPURLField.Get()
 if value,_:=first.First.Get();value!="kept"{t.Fatal("first group shape overwritten")}
 if value,_:=second.Second.Get();value!="also kept"{t.Fatal("second group shape overwritten")}
 rows,_:=block.APIURL.Get();other,_:=block.APIURLField.Get()
 if value,_:=rows[0].First.Get();value!="kept"{t.Fatal("first array shape overwritten")}
 if value,_:=other[0].Second.Get();value!="also kept"{t.Fatal("second array shape overwritten")}
 encoded,err:=json.Marshal(block);if err!=nil{t.Fatal(err)}
 var before,after any;_ = json.Unmarshal([]byte(raw),&before);_ = json.Unmarshal(encoded,&after)
 if !reflect.DeepEqual(before,after){t.Fatalf("round trip lost fields: %s",encoded)}
}
`)
	// Flattened type paths must also fail before overwriting different definitions.
	conflicting := field.Block{TypeName: "Hero", Slug: "hero", Fields: field.Fields{field.Group("api", field.Fields{field.Group("url", field.Fields{field.Text("first")})}), field.Group("apiURL", field.Fields{field.Text("second")})}}
	manifest, err = core.Resolve(core.Config{Name: "Nested path collision", Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("layout", conflicting)}}}})
	if err != nil {
		t.Fatal(err)
	}
	if data, err := goClient(manifest); err == nil || data != nil || !strings.Contains(err.Error(), "generated Go symbol collides: HeroAPIURL") {
		t.Fatalf("nested collision did not fail closed: %v", err)
	}
}
