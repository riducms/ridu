package main

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
)

func blockNamesCollection() ridu.Collection {
	name := field.Text("blockName").
		Label("Block name").
		MaxLength(120).
		Access(field.Access{
			Update: func(ctx operation.Context) (bool, error) {
				locked, _ := ctx.Siblings.Get("nameLocked").BooleanValue()
				return !locked, nil
			},
		}).
		Validate(func(_ operation.Context, value operation.Value[string]) ([]operation.Issue, error) {
			if text, _ := value.Get(); text == "invalid" {
				return []operation.Issue{
					{Code: "block_name", Message: "Choose a descriptive block name"},
				}, nil
			}
			return nil, nil
		})

	// The rich-text fixture owns "cta" and "callout"; these blocks keep their
	// editor labels under their own slugs.
	cta := field.Block{
		Slug:   "named-cta",
		Labels: field.BlockLabels{Singular: "CTA", Plural: "Ctas"},
		Admin:  field.BlockAdmin{RowLabelPath: "heading"},
		Fields: field.Fields{
			name,
			field.Text("heading").Required(),
			field.Checkbox("nameLocked"),
		},
	}

	callout := field.Block{
		Slug:   "named-callout",
		Labels: field.BlockLabels{Singular: "Callout", Plural: "Callouts"},
		Admin:  field.BlockAdmin{RowLabelPath: "heading"},
		Fields: field.Fields{
			name,
			field.Text("heading").Required(),
			field.Checkbox("nameLocked"),
			richtext.Field("detail", richtext.Config{Blocks: []field.Block{cta}}),
			field.Blocks("nested", cta),
		},
	}

	hidden := field.Block{
		Slug:  "hidden-name",
		Admin: field.BlockAdmin{RowLabelPath: "blockName"},
		Fields: field.Fields{
			field.Text("blockName").Admin(field.Admin{Hidden: true}),
			field.Text("heading"),
		},
	}

	return ridu.Collection{
		Slug: "block-names",
		Labels: ridu.CollectionLabels{
			Singular: "Block names",
			Plural:   "Block names",
		},
		Versions:      true,
		VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{
			field.Text("title").Required(),
			field.Blocks("layout", callout, cta, hidden),
			richtext.Field("body", richtext.Config{
				Blocks: []field.Block{callout, cta, hidden},
			}),
		},
		Access: ridu.CollectionAccess{
			Create: allowRoles(roleAdministrator, roleEditor),
			Read:   allowEveryone,
			Update: allowRoles(roleAdministrator, roleEditor),
			Delete: allowRoles(roleAdministrator),
		},
	}
}
