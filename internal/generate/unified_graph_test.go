package generate

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/adapters/mongodb"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/typescript"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// All generated contracts consume the one resolved manifest.
func TestUnifiedGraphGenerationExpectedContractAndDeterminism(t *testing.T) {
	build := func() core.Config {
		config := core.Config{Name: "Unified contract control", Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []core.Collection{
			{Slug: "users", Fields: field.Fields{field.Text("name").Required()}},
			{Slug: "media", Upload: true},
			{Slug: "pages"},
		}}
		config.Collections[2].Fields = field.Fields{
			field.Text("title").Required().MaxLength(120),
			field.Text("status").Required().Default("pending"),
			field.Text("optional"),
			field.Text("code").Unique().Index(),
			field.Group("meta", field.Fields{field.Text("description").Localized(), field.Number("order").Min(0).Index()}),
			field.Array("sections", field.Fields{field.Text("label").Required(), field.Text("translation").Localized()}),
			field.Blocks("content", field.Block{Slug: "hero", TypeName: "Hero", Fields: field.Fields{field.Text("heading").Required()}}),
			field.Relationship("author", "users"), field.Relationships("reviewers", "users"), field.Upload("image", "media"), field.Text("localized").Localized(),
			field.Virtual("summary", field.ValueString, func(operation.Context) (operation.Value[store.Value], error) {
				return operation.Present(store.String("summary")), nil
			}),
		}
		return config
	}
	repeated, err := core.Resolve(build())
	if err != nil {
		t.Fatal(err)
	}
	graph, err := core.Resolve(build())
	if err != nil {
		t.Fatal(err)
	}
	for name, generate := range map[string]func(schema.Manifest) ([]byte, error){
		"manifest": func(m schema.Manifest) ([]byte, error) { return m.Bytes() },
		"Go":       goClient, "TypeScript": typescript.Client, "OpenAPI": openAPI,
	} {
		t.Run(name, func(t *testing.T) {
			a, err := generate(repeated)
			if err != nil {
				t.Fatal(err)
			}
			b, err := generate(graph)
			if err != nil {
				t.Fatal(err)
			}
			c, err := generate(graph)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a, b) || !bytes.Equal(b, c) {
				t.Fatal("equivalent graph changed deterministic contract")
			}
		})
	}
	generated, err := goClient(graph)
	if err != nil {
		t.Fatal(err)
	}
	runGeneratedGoConsumer(t, generated, unifiedGeneratedGoConsumer)
	t.Run("PostgreSQL schema plan", func(t *testing.T) {
		a, err := postgres.BuildArtifact(t.Context(), "initial", nil, repeated, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		b, err := postgres.BuildArtifact(t.Context(), "initial", nil, graph, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Fatal("equivalent unified schema changed PostgreSQL plan")
		}
	})
	for _, adapter := range []string{"SQLite", "MongoDB"} {
		t.Run(adapter+" schema plan", func(t *testing.T) {
			create := func(manifest schema.Manifest) []byte {
				var path string
				if adapter == "SQLite" {
					artifact, err := sqlite.CreateArtifact(t.Context(), t.TempDir(), "initial", manifest, time.Unix(1, 0), false)
					if err != nil {
						t.Fatal(err)
					}
					path = artifact.Path
				} else {
					artifact, err := mongodb.CreateArtifact(t.Context(), t.TempDir(), "initial", manifest, time.Unix(1, 0))
					if err != nil {
						t.Fatal(err)
					}
					path = artifact.Path
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				return data
			}
			if !bytes.Equal(create(repeated), create(graph)) {
				t.Fatal("equivalent unified schema changed adapter plan")
			}
		})
	}
}

func TestUnifiedGraphManifestAndQueryProjectionExcludePrivateRuntimeIdentity(t *testing.T) {
	resolve := func(secret string) schema.Manifest {
		read := func(operation.Context) (bool, error) { panic(secret) }
		node := field.Text("sku").Required().Admin(field.Admin{Extensions: map[string]store.Value{"catalog": store.Object(store.Values{"public": store.Boolean(true)})}}).Private("application.secret", store.String(secret)).
			Access(field.Access{Read: read}).Validate(func(operation.Context, operation.Value[string]) ([]operation.Issue, error) { panic(secret) })
		result, err := core.Resolve(core.Config{Name: "Private graph", Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{
			node, field.Group("meta", field.Fields{node}),
			field.Array("sections", field.Fields{node, field.Text("visible")}),
			field.Group("protected", field.Fields{field.Text("child")}).Access(field.Access{Read: read}),
			field.Text("presentation").Admin(field.Admin{Hidden: true}),
		}}}})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	a, b := resolve("first-private-value"), resolve("second-private-value")
	aJSON, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	bJSON, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(aJSON, bJSON) || bytes.Contains(aJSON, []byte("private-value")) || bytes.Contains(aJSON, []byte("application.secret")) {
		t.Fatal("private attachment/callback identity entered portable resolution")
	}
	manifest := a
	if extension := manifest.Snapshot().Collections[0].Fields[0].Admin.Extensions["catalog"]; string(extension) != `{"public":true}` {
		t.Fatalf("public graph attachment omitted from manifest: %s", extension)
	}
	client, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	where := strings.Split(strings.Split(string(client), "export interface PagesWhere {")[1], "\n}")[0]
	for _, absent := range []string{`"sku"`, `"meta.sku"`, `"sections.sku"`, `"protected"`, `"protected.child"`} {
		if strings.Contains(where, absent) {
			t.Errorf("protected query contract contains %s", absent)
		}
	}
	for _, present := range []string{`"meta"`, `"sections.visible"`, `"presentation"`} {
		if !strings.Contains(where, present) {
			t.Errorf("readable query contract lost %s", present)
		}
	}
}

const unifiedGeneratedGoConsumer = `package generated_test
import ("encoding/json"; "testing"; "github.com/riducms/ridu/core"; g "example.com/stable-consumer")
func TestUnifiedShape(t *testing.T) {
 status := "ready"; create := g.PageCreate{Title:"Title", Status:&status, Author:core.Set("author"), Reviewers:core.Set([]string{"author"})}
 encoded,err:=json.Marshal(create); if err!=nil {t.Fatal(err)}
 if string(encoded)=="" {t.Fatal("empty create")}
 for _,raw:=range []string{"{}", "{\"optional\":null}", "{\"optional\":\"value\"}"} {
  var update g.PageUpdate; if err:=json.Unmarshal([]byte(raw), &update);err!=nil {t.Fatal(err)}
  encoded,err:=json.Marshal(update);if err!=nil||string(encoded)!=raw {t.Fatalf("patch presence %s: %s %v",raw,encoded,err)}
 }
 var output g.Page
 if err:=json.Unmarshal([]byte("{\"author\":{\"id\":\"author\",\"name\":\"Ada\"},\"reviewers\":[\"author\"],\"summary\":\"resolved\",\"sections\":[{\"_key\":\"A\"}]}"), &output);err!=nil {t.Fatal(err)}
 if output.Author==nil||output.Author.Document==nil||output.Author.Document.Name==nil||*output.Author.Document.Name!="Ada" {t.Fatal("populated author")}
 if output.Summary==nil||*output.Summary!="resolved" {t.Fatal("computed output")}
 var localized g.PageAllLocales
 if err:=json.Unmarshal([]byte("{\"localized\":{\"en\":\"English\",\"fr\":null},\"meta\":{\"description\":{\"en\":\"Nested\"}}}"), &localized);err!=nil {t.Fatal(err)}
 if (*localized.Localized)["en"]==nil||*(*localized.Localized)["en"]!="English"||(*localized.Localized)["fr"]!=nil {t.Fatal("localized null/values")}
}
`
