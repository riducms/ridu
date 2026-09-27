package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Articles = ridu.Collection{
	Slug: "articles",
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Text("slug"),
		field.Textarea("body"),
		field.Text("excerpt").Required(),
		field.Number("wordCount"),
		field.Text("metaTitle"),
		field.Checkbox("featured"),
		field.Text("lastEditedBy"),
	},
	// Each list runs its functions in the order you write them.
	Hooks: ridu.CollectionHooks{
		BeforeValidate:  []ridu.Hook{fillExcerpt},
		BeforeChange:    []ridu.Hook{recordLastEditor},
		BeforeOperation: []ridu.Hook{countWords},
		AfterChange:     []ridu.Hook{writeAuditEntry},
		AfterRead:       []ridu.Hook{fillMetaTitle},
		BeforeDuplicate: []ridu.Hook{markCopy},
		BeforeDelete:    []ridu.Hook{keepFeatured},
		AfterError:      []ridu.Hook{logFailure},
		AfterCommit:     []ridu.Hook{purgeArticleCache},
	},
}
