package generate

import (
	"bytes"
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
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/graphql"
	"github.com/riducms/ridu/schema"
)

func primitiveListManifest(t *testing.T) schema.Manifest {
	t.Helper()
	manifest, err := core.Resolve(core.Config{Name: "Primitive contracts", Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []core.Collection{{Slug: "products", Fields: field.Fields{
		field.Text("title"), field.Number("price"),
		field.TextList("sellingPoints").MinRows(1).MaxRows(3).MinLength(1).MaxLength(12),
		field.NumberList("availableSizes").Min(0).Max(30).MaxRows(3),
		field.TextList("translated").Localized(),
		field.NumberList("defaulted").Required().Default(0, 10),
		field.TextList("computedDefault").Required().DefaultFrom(func(operation.Context) (operation.Value[[]string], error) {
			t.Fatal("generation executed a dynamic default")
			return operation.Empty[[]string](), nil
		}),
		field.Group("meta", field.Fields{field.NumberList("sizes").MinRows(1)}),
		field.Array("variants", field.Fields{field.TextList("tags")}),
		field.Blocks("content", field.Block{TypeName: "Card", Slug: "card", Fields: field.Fields{field.TextList("tags").Required()}}),
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestPrimitiveListGeneratedContracts(t *testing.T) {
	manifest := primitiveListManifest(t)
	for name, generate := range map[string]func(schema.Manifest) ([]byte, error){"Go": goClient, "TS": typescript.Client, "OpenAPI": openAPI, "GraphQL": func(m schema.Manifest) ([]byte, error) { text, err := graphql.GenerateSDL(m); return []byte(text), err }} {
		t.Run(name, func(t *testing.T) {
			encoded, err := generate(manifest)
			if err != nil {
				t.Fatal(err)
			}
			repeated, err := generate(manifest)
			if err != nil || !bytes.Equal(encoded, repeated) {
				t.Fatalf("nondeterministic %s: %v", name, err)
			}
			switch name {
			case "Go":
				for _, fragment := range []string{"core.NonNullInput[[]string]", "*core.Input[[]float64]", "*core.NonNullInput[[]float64]", "map[string][]string", "null list items are not allowed"} {
					if !strings.Contains(string(encoded), fragment) {
						t.Fatalf("missing %q", fragment)
					}
				}
			case "TS":
				for _, fragment := range []string{`"sellingPoints": string[]`, `"availableSizes"?: number[] | null`, `"defaulted"?: number[]`, `"computedDefault"?: string[]`, `"sellingPoints"?: PrimitiveListWhere<string>`, `"availableSizes"?: PrimitiveListWhere<number>`, `RiduLocalizedValues<string[] | null>`} {
					if !strings.Contains(string(encoded), fragment) {
						t.Fatalf("missing %q", fragment)
					}
				}
			case "GraphQL":
				for _, fragment := range []string{"sellingPoints: [String!]!", "availableSizes: [Float!]", "defaulted: [Float!]", "in: [String!]", "not_in: [Float!]"} {
					if !strings.Contains(string(encoded), fragment) {
						t.Fatalf("missing %q", fragment)
					}
				}
			}
		})
	}
}

func TestPrimitiveListOpenAPIInputAndReadShapes(t *testing.T) {
	encoded, err := openAPI(primitiveListManifest(t))
	if err != nil {
		t.Fatal(err)
	}
	var document openAPIDocument
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	create := resolvedResourceSchema(t, document, "productsCreate")
	update := resolvedResourceSchema(t, document, "productsUpdate")
	for _, test := range []struct {
		raw   string
		valid bool
	}{
		{`{"sellingPoints":["Oak","Oak"],"availableSizes":[0,10,10]}`, true},
		{`{"sellingPoints":["Oak"],"availableSizes":[]}`, true},
		{`{"sellingPoints":["Oak"],"availableSizes":null}`, true},
		{`{}`, false}, {`{"sellingPoints":null}`, false}, {`{"sellingPoints":[]}`, false},
		{`{"sellingPoints":[""]}`, false}, {`{"sellingPoints":["much too long value"]}`, false},
		{`{"sellingPoints":["a","b","c","d"]}`, false}, {`{"sellingPoints":[null]}`, false},
		{`{"sellingPoints":[2]}`, false}, {`{"sellingPoints":"Oak"}`, false},
		{`{"sellingPoints":["Oak"],"availableSizes":["10"]}`, false},
		{`{"sellingPoints":["Oak"],"availableSizes":[31]}`, false},
		{`{"sellingPoints":["Oak"],"availableSizes":[-1]}`, false},
		{`{"sellingPoints":["Oak"],"availableSizes":[null]}`, false},
		{`{"sellingPoints":["Oak"],"meta":{}}`, false},
	} {
		var value any
		if err := json.Unmarshal([]byte(test.raw), &value); err != nil {
			t.Fatal(err)
		}
		if err := create.Validate(value); (err == nil) != test.valid {
			t.Errorf("create %s valid=%v: %v", test.raw, test.valid, err)
		}
	}
	if err := update.Validate(map[string]any{}); err != nil {
		t.Fatalf("update omission: %v", err)
	}
	if err := update.Validate(map[string]any{"sellingPoints": nil}); err == nil {
		t.Fatal("MinRows update admits null")
	}
	// Read hooks may change lengths and bounds while preserving primitive types.
	output := resolvedResourceSchema(t, document, "products")
	if err := output.Validate(map[string]any{"id": "one", "createdAt": "2026-09-08T00:00:00Z", "updatedAt": "2026-09-08T00:00:00Z", "sellingPoints": []any{}, "availableSizes": []any{100.0}}); err != nil {
		t.Fatalf("read constraints: %v", err)
	}
}

func TestPrimitiveListGeneratedGoExternalCodec(t *testing.T) {
	generated, err := goClient(primitiveListManifest(t))
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	for name, source := range map[string]string{"go.mod": fmt.Sprintf("module example.com/list-consumer\n\ngo 1.25.13\nrequire github.com/riducms/ridu v0.0.0\nreplace github.com/riducms/ridu => %s\n", root), "ridu.generated.go": string(generated), "lists_test.go": primitiveListGoConsumer} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.CommandContext(t.Context(), "go", "test", "-mod=mod", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("external primitive list consumer: %v\n%s", err, output)
	}
}

const primitiveListGoConsumer = `package generated_test
import("testing";"encoding/json";"strings";"reflect";"github.com/riducms/ridu/core";g "example.com/list-consumer")
func TestListOutputRoundtrip(t *testing.T) {
 for _,raw:=range []string{
  "{\"sellingPoints\":[],\"availableSizes\":[0,10,10]}",
  "{\"meta\":{\"sizes\":[]},\"variants\":[{\"_key\":\"a\",\"tags\":[]}]}",
  "{\"content\":[{\"blockType\":\"card\",\"_key\":\"a\",\"tags\":[]}]}",
 } {
  var document g.Product;if err:=json.Unmarshal([]byte(raw),&document);err!=nil{t.Fatal(err)}
  encoded,err:=json.Marshal(document);if err!=nil{t.Fatal(err)}
  var actual,expected map[string]any;json.Unmarshal(encoded,&actual);json.Unmarshal([]byte(raw),&expected)
  for name,value:=range expected {if !reflect.DeepEqual(actual[name],value){t.Fatalf("%s lost %s: %s",raw,name,encoded)}}
 }
 encoded,err:=json.Marshal(g.Product{});if err!=nil||strings.Contains(string(encoded),"sellingPoints")||strings.Contains(string(encoded),"availableSizes"){t.Fatalf("absent list emitted: %s %v",encoded,err)}
}
func TestListCodecs(t *testing.T) {
 input:=g.ProductCreate{SellingPoints:core.NonNull([]string{"Oak","Oak"}),AvailableSizes:core.Set([]float64{0,10,10})}
 raw,err:=json.Marshal(input);if err!=nil{t.Fatal(err)}
 var decoded g.ProductCreate;if err:=json.Unmarshal(raw,&decoded);err!=nil{t.Fatal(err)}
 values,_:=decoded.AvailableSizes.Get();if len(values)!=3||values[0]!=0||values[1]!=values[2]{t.Fatal(values)}
 for _,raw:=range []string{
  "{\"sellingPoints\":[null]}","{\"availableSizes\":[null]}","{\"availableSizes\":[\"0\"]}","{\"sellingPoints\":\"Oak\"}",
  "{\"variants\":[{\"tags\":[null]}]}","{\"meta\":{\"sizes\":[null]}}",
  "{\"content\":[{\"blockType\":\"card\",\"tags\":[null]}]}"} {
  var value g.ProductCreate;if err:=json.Unmarshal([]byte(raw),&value);err==nil{t.Fatalf("accepted %s",raw)}
 }
 var update g.ProductUpdate;if err:=json.Unmarshal([]byte("{\"availableSizes\":[]}"),&update);err!=nil{t.Fatal(err)}
 empty,ok:=update.AvailableSizes.Get();if !ok||empty==nil||len(empty)!=0{t.Fatal("empty list lost")}
 encoded,err:=json.Marshal(update);if err!=nil||!strings.Contains(string(encoded),"\"availableSizes\":[]"){t.Fatalf("%s %v",encoded,err)}
 if err:=json.Unmarshal([]byte("{\"availableSizes\":null}"),&update);err!=nil{t.Fatal(err)}
 if _,ok:=update.AvailableSizes.Get();ok{t.Fatal("null list lost")}
 var all g.ProductAllLocales;if err:=json.Unmarshal([]byte("{\"translated\":{\"fr\":[\"Bois\",\"Bois\"]}}"),&all);err!=nil{t.Fatal(err)}
 if len((*all.Translated)["fr"])!=2{t.Fatal("localized list lost")}
 if err:=json.Unmarshal([]byte("{\"translated\":{\"fr\":[null]}}"),&all);err==nil{t.Fatal("null localized item accepted")}
}
`
