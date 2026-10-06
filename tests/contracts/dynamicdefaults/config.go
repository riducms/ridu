// Package dynamicdefaults exercises server initialization through the real admin.
package dynamicdefaults

import (
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
)

// Block slugs of this contract. A slug names one definition per application,
// so the Blocks card and the differently shaped rich-text card have their own
// slugs, distinct from the plain "card" blocks of other contracts mounted in
// the admin fixture, while keeping the "Card" editor label.
const (
	CardSlug         = "default-card"
	EmbeddedCardSlug = "default-embedded-card"
)

// Collection uses the same callback inside ordinary, repeated, and embedded fields.
func Collection() core.Collection {
	title := field.Text("title").Required().DefaultFrom(initialTitle)
	allow := func(core.AccessContext) (core.AccessDecision, error) { return core.Allow(), nil }
	return core.Collection{
		Slug:   "dynamic-defaults",
		Admin:  core.CollectionAdmin{UseAsTitle: "title"},
		Labels: core.CollectionLabels{Singular: "Default example", Plural: "Default examples"},
		Access: core.CollectionAccess{Create: allow, Read: allow, Update: allow, Delete: allow},
		Fields: field.Fields{
			title.Admin(field.Admin{Editor: field.Component("app:text")}),
			field.Relationship("related", "dynamic-defaults"),
			field.Text("note").DefaultFrom(initialNote),
			title.Rename("localizedTitle").Localized(),
			field.Array("sections", field.Fields{title}),
			field.Blocks("content", field.Block{Slug: CardSlug, Labels: field.BlockLabels{Singular: "Card", Plural: "Cards"}, Fields: field.Fields{title}}),
			richtext.Field("body", richtext.Config{Blocks: []field.Block{{Slug: EmbeddedCardSlug, Labels: field.BlockLabels{Singular: "Card", Plural: "Cards"}, Fields: field.Fields{
				title, field.Text("note").DefaultFrom(initialNote),
			}}}}),
		},
	}
}

func initialTitle(ctx operation.Context) (operation.Value[string], error) {
	if ctx.Locale == "fr" {
		return operation.Present("Sans titre"), nil
	}
	return operation.Present("Untitled"), nil
}

func initialNote(operation.Context) (operation.Value[string], error) {
	return operation.Present("Server note"), nil
}
