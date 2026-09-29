package main

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/richtext"
)

// richTextEditorOptionsCollection exercises rich-text presentation settings: a
// pinned toolbar without a gutter, and an editor without block handles.
func richTextEditorOptionsCollection() ridu.Collection {
	return ridu.Collection{
		Slug:   "editor-options",
		Labels: ridu.CollectionLabels{Singular: "Editor option", Plural: "Editor options"},
		Admin:  ridu.CollectionAdmin{Group: "Editorial", UseAsTitle: "title"},
		Fields: field.Fields{
			field.Text("title").Required(),
			richtext.Field("pinned", richtext.Config{Admin: richtext.Admin{
				FixedToolbar: true,
				HideGutter:   true,
			}}).Label("Pinned toolbar"),
			richtext.Field("minimal", richtext.Config{Admin: richtext.Admin{
				HideDraggableBlockElement: true,
				HideAddBlockButton:        true,
				HideInsertParagraphAtEnd:  true,
			}}).Label("Minimal"),
		},
		Access: ridu.CollectionAccess{
			Create: allowRoles(roleAdministrator, roleEditor),
			Read:   allowEveryone,
			Update: allowRoles(roleAdministrator, roleEditor),
			Delete: allowRoles(roleAdministrator),
		},
	}
}
