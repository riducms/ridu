package core

import (
	"fmt"
	"strings"
	"testing"

	"github.com/riducms/ridu/field"
)

type graphValidationPlugin struct {
	key      string
	validate func(FieldGraphContext, field.Fields) error
}

func (p graphValidationPlugin) Key() string { return p.key }
func (p graphValidationPlugin) ValidateFields(context FieldGraphContext, graph field.Fields) error {
	return p.validate(context, graph)
}

func TestFieldGraphValidationSeesFinalDetachedGraph(t *testing.T) {
	calls := 0
	validator := graphValidationPlugin{key: "validation", validate: func(context FieldGraphContext, graph field.Fields) error {
		calls++
		if len(graph) != 2 || graph[1].Name() != "added" {
			return fmt.Errorf("%s did not receive the final graph", context.Slug)
		}
		graph[0] = field.Text("discarded")
		return nil
	}}
	config := Config{Name: "Final graph", Plugins: []Plugin{validator, graphEditPlugin{key: "later", transform: func(_ FieldGraphContext, graph field.Fields) (field.Fields, error) {
		return graph.Edit(func(draft *field.ChildrenDraft) error { return draft.Insert(len(graph), field.Text("added")) })
	}}}, Collections: []Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}, Globals: []Global{{Slug: "site", Fields: field.Fields{field.Text("name")}}}}
	manifest, err := Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	if calls != 2 || snapshot.Collections[0].Fields[0].Name != "title" || snapshot.Globals[0].Fields[0].Name != "name" {
		t.Fatal("configuration validator mutated the final graph or ran more than once per resource")
	}
	config.Plugins = []Plugin{validator, validator}
	_, err = Resolve(config)
	if err == nil || !strings.Contains(err.Error(), "duplicate_plugin_key") || calls != 2 {
		t.Fatalf("duplicate validator executed before rejection: calls=%d error=%v", calls, err)
	}
}
