package main

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/tests/contracts/blockreferences"
)

func withBlockReferenceFixture(config ridu.Config) ridu.Config {
	for _, refs := range []bool{false, true} {
		fixture := blockreferences.Config(refs)
		prefix := "inline"
		if refs {
			prefix = "reference"
			config.Blocks = append(config.Blocks, fixture.Blocks...)
		}
		labelBlocks := []field.Block{
			{
				Slug:     "people",
				TypeName: "People",
				Fields: field.Fields{
					field.Text("name"),
				},
			},
			{
				Slug:     "promotion",
				TypeName: "Promotion",
				Labels: field.BlockLabels{
					Singular: "CTA",
					Plural:   "CTAs",
				},
				Fields: field.Fields{
					field.Text("name"),
				},
			},
		}
		layout := field.Blocks("layout", labelBlocks...)
		body := richtext.Config{Blocks: labelBlocks}
		if refs {
			config.Blocks = append(config.Blocks, labelBlocks...)
			layout = field.Blocks("layout").References("people", "promotion")
			body = richtext.Config{BlockReferences: []string{"people", "promotion"}}
		}
		fixture.Collections = append(fixture.Collections, ridu.Collection{
			Slug: "block-labels",
			Fields: field.Fields{
				layout,
				richtext.Field("body", body),
			},
		})
		for _, collection := range fixture.Collections {
			collection.Slug = schema.CollectionSlug(prefix + "-" + string(collection.Slug))
			config.Collections = append(config.Collections, collection)
		}
	}
	return config
}
