package generate

import (
	"fmt"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/schema"
)

func TestRichTextRepeatedDefinitionGrowth(t *testing.T) {
	callout := field.Block{TypeName: "SharedCallout", Slug: "callout", Fields: field.Fields{field.Text("title").Required(), field.Textarea("message"), richtext.Field("body")}}
	checkRepeatedDefinitionGrowth(t, "SharedCallout", func(count int) schema.Manifest {
		fields := make(field.Fields, count)
		for index := range fields {
			fields[index] = richtext.Field(fmt.Sprintf("body%d", index), richtext.Config{Blocks: []field.Block{callout}})
		}
		manifest, err := core.Resolve(core.Config{Name: "Rich text growth", Plugins: []core.Plugin{richtext.New()}, Collections: []core.Collection{{Slug: "pages", Fields: fields}}})
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	})
}
