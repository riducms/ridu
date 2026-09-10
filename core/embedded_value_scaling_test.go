package core_test

import (
	"encoding/json"
	"fmt"
	"runtime"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
	outline "github.com/riducms/ridu/tests/contracts/embedded_plugin"
)

// Keep and Replace use the same operation and payload as BenchmarkEmbeddedHookBatch.
// Views isolates callback-requested work from engine work; Serialize includes final
// materialization so a deferred representation cannot hide that cost from timing.
func BenchmarkEmbeddedValueScaling(b *testing.B) {
	benchmarkValueScaling(b, false)
}

// Native blocks provide the same logical fields without the embedded plugin
// envelope. They match the native-block operation used in the Payload comparison.
func BenchmarkNativeValueScaling(b *testing.B) {
	benchmarkValueScaling(b, true)
}

func benchmarkValueScaling(b *testing.B, native bool) {
	for _, view := range []string{"None", "Retain", "ReadRoot", "ReadAll", "Materialize", "Serialize"} {
		b.Run(view, func(b *testing.B) {
			for _, size := range []int{1, 10, 100, 1000, 5000} {
				b.Run(fmt.Sprint(size), func(b *testing.B) {
					for _, replace := range []bool{false, true} {
						name := "Keep"
						if replace {
							name = "Replace"
						}
						b.Run(name, func(b *testing.B) {
							calls, observed := 0, 0
							var retained []operation.View
							title := field.Text("title").Required().Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(ctx operation.WriteContext, _ operation.Value[string]) (operation.Change[string], error) {
								calls++
								switch view {
								case "Retain":
									retained = append(retained, ctx.Root)
								case "ReadRoot":
									if ctx.Root.Get("body").Kind() == store.ValueObject {
										observed++
									}
								case "ReadAll":
									for item := range ctx.Root.Get("body").Get("outline").Elements() {
										text, _ := item.Get("content").Get("title").StringValue()
										observed += len(text)
									}
								case "Materialize":
									body, _ := ctx.Root.Get("body").CopyObject()
									items, _ := body["outline"].CopyList()
									for _, item := range items {
										node, _ := item.CopyObject()
										content, _ := node["content"].CopyObject()
										text, _ := content["title"].StringValue()
										observed += len(text)
									}
								}
								if replace {
									return operation.Replace(operation.Present("edited")), nil
								}
								return operation.Keep[string](), nil
							}}})
							config := embeddedConfig(title)
							if native {
								config.Plugins = nil
								config.Collections[1].Fields = field.Fields{
									field.Group("body", field.Fields{field.Blocks("outline", field.Block{Slug: "card", Fields: field.Fields{field.Group("content", embeddedCard(title).Fields)}})}),
									field.JSON("ordinary"),
								}
							}
							nodes := make([]store.Value, size)
							for i := range nodes {
								if native {
									nodes[i] = store.Object(store.Values{"blockType": store.String("card"), "_key": store.String(fmt.Sprint(i)), "content": store.Object(store.Values{"title": store.String("node")})})
								} else {
									nodes[i] = outline.Widget("card", fmt.Sprint(i), store.Values{"title": store.String("node")})
								}
							}
							var values store.Values
							if native {
								values = store.Values{"body": store.Object(store.Values{"outline": store.List(nodes...)})}
							} else {
								values = store.Values{"body": outline.Value(nodes...)}
							}
							b.ReportAllocs()
							b.ResetTimer()
							for range b.N {
								b.StopTimer()
								app, err := ridu.New(config, teststore.New())
								if err != nil {
									b.Fatal(err)
								}
								calls, observed, retained = 0, 0, nil
								b.StartTimer()
								created, err := app.Local().Create(b.Context(), "pages", values, nil)
								if err != nil || calls != size {
									b.Fatalf("nodes=%d hooks=%d error=%v", size, calls, err)
								}
								if view == "Serialize" {
									encoded, err := json.Marshal(created.Values)
									if err != nil || len(encoded) == 0 {
										b.Fatal("serialize result:", err)
									}
								}
								if view == "ReadRoot" && observed != size || (view == "ReadAll" || view == "Materialize") && observed == 0 || view == "Retain" && len(retained) != size {
									b.Fatal("callback workload did not run")
								}
								runtime.KeepAlive(retained)
							}
						})
					}
				})
			}
		})
	}
}
