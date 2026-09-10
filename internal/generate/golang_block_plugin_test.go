package generate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

func TestGeneratedGoBlockPluginPointerCodecs(t *testing.T) {
	manifest, err := core.Resolve(core.Config{Name: "Plugin codecs", Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("layout", field.Block{TypeName: "Hero", Slug: "hero", Fields: field.Fields{field.JSON("value")}})}}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	child := &snapshot.Collections[0].Fields[0].Blocks.ResolvedTypes()[0].ResolvedFields()[0]
	child.Type = schema.FieldTypePlugin
	child.Plugin = &schema.PluginField{Key: "codec"}
	snapshot.Plugins = []schema.Plugin{{Key: "codec", FieldTypes: []schema.PluginFieldType{{Key: "codec", GoPackage: "example.com/plugin-consumer/pluginvalue", GoType: "Value"}}}}
	generated, err := goClient(schema.NewManifest(snapshot))
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "pluginvalue"), 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod":            fmt.Sprintf("module example.com/plugin-consumer\n\ngo 1.25.13\nrequire github.com/riducms/ridu v0.0.0\nreplace github.com/riducms/ridu => %s\n", root),
		"ridu.generated.go": string(generated),
		"pluginvalue/value.go": `package pluginvalue
import "errors"
var ErrValue=errors.New("plugin codec failure")
type Value struct {Fail bool}
func(value *Value) MarshalText()([]byte,error){if value.Fail{return nil,ErrValue};return []byte("wire"),nil}
func(value *Value) UnmarshalText(text []byte)error{if string(text)=="fail"{return ErrValue};return nil}
`,
		"plugin_test.go": `package generated_test
import("encoding/json";"errors";"strings";"testing";g "example.com/plugin-consumer";"example.com/plugin-consumer/pluginvalue")
func TestOpaquePointerCodec(t *testing.T){
 value:=pluginvalue.Value{}
 rows:=g.PagesLayout{&g.Hero{Key:"one",Value:&g.BlockOptional[pluginvalue.Value]{Value:&value}}}
 data,err:=json.Marshal(rows);if err!=nil||!strings.Contains(string(data),"\"value\":\"wire\""){t.Fatalf("pointer text codec bypassed: %s %v",data,err)}
 var decoded g.PagesLayout;if err:=json.Unmarshal(data,&decoded);err!=nil{t.Fatal(err)}
 value.Fail=true
 _,err=json.Marshal(rows);var detail *g.ContractError
 if !errors.Is(err,pluginvalue.ErrValue)||!errors.As(err,&detail)||detail.Path!="[0].value"||detail.Operation!="encode"{t.Fatalf("encode cause: %#v %v",detail,err)}
 before:=decoded[0]
 err=json.Unmarshal([]byte("[{\"blockType\":\"hero\",\"_key\":\"one\",\"value\":\"fail\"}]"),&decoded)
 if !errors.Is(err,pluginvalue.ErrValue)||!errors.As(err,&detail)||detail.Path!="[0].value"||detail.Operation!="decode"||decoded[0]!=before{t.Fatalf("decode cause/atomicity: %#v %v",detail,err)}
}
`,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.CommandContext(t.Context(), "go", "test", "-mod=mod", "./...")
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("plugin consumer: %v\n%s", err, output)
	}
}
