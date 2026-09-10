package core_test

import (
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"strings"
	"testing"
)

func TestBlocksBoundsAndSummaryMetadata(t *testing.T) {
	resolve := func(label string) (schema.Manifest, error) {
		return ridu.Resolve(ridu.Config{Name: "Blocks", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("layout", field.Block{Admin: field.BlockAdmin{RowLabelPath: label}, Slug: "hero", Fields: field.Fields{field.Text("heading"), field.Number("count"), field.Checkbox("enabled"), field.JSON("data"), field.Group("group", field.Fields{field.Text("child")})}}).MinRows(1).MaxRows(3)}}}})
	}
	for _, label := range []string{"heading", "count", "enabled", ""} {
		manifest, err := resolve(label)
		if err != nil {
			t.Fatal(err)
		}
		blocks := manifest.Snapshot().Collections[0].Fields[0].Blocks
		if blocks.MinRows != 1 || blocks.MaxRows != 3 {
			t.Fatal("bounds lost")
		}
		if label != "" && blocks.ResolvedTypes()[0].Admin.RowLabel != label {
			t.Fatal("summary lost")
		}
		if label != "" {
			blocks.ResolvedTypes()[0].Admin.RowLabel = "mutated"
			if manifest.Snapshot().Collections[0].Fields[0].Blocks.ResolvedTypes()[0].Admin.RowLabel != label {
				t.Fatal("mutable manifest")
			}
		}
		encoded, err := manifest.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = schema.Parse(encoded); err != nil {
			t.Fatal(err)
		}
	}
	for _, label := range []string{"missing", "data", "group", "group.child", "blockType", "_key"} {
		if _, err := resolve(label); err == nil || !strings.Contains(err.Error(), ".admin.rowLabelPath") {
			t.Fatalf("%q: %v", label, err)
		}
	}
}

func TestParseRejectsInvalidBlocksMetadata(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{Name: "Blocks", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading")}})}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*schema.BlocksField){
		func(blocks *schema.BlocksField) { blocks.MinRows = -1 },
		func(blocks *schema.BlocksField) { blocks.MaxRows = -1 },
		func(blocks *schema.BlocksField) { blocks.MinRows = 3; blocks.MaxRows = 2 },
		func(blocks *schema.BlocksField) {
			blocks.ResolvedTypes()[0].Admin = &schema.BlockAdmin{RowLabel: "missing"}
		},
	} {
		snapshot := manifest.Snapshot()
		change(snapshot.Collections[0].Fields[0].Blocks)
		encoded, err := schema.NewManifest(snapshot).MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = schema.Parse(encoded); err == nil {
			t.Fatal("tampered blocks metadata accepted")
		}
	}
}
