package content

import (
	"strings"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

var Tags = field.TextList("tags").
	MinLength(1).MaxLength(30). // Limit each tag's length.
	MaxRows(8)                  // Allow up to eight tags.

var Catalog = ridu.Collection{
	Slug: "products",
	Fields: field.Fields{
		field.Text("name").Required(),
		Tags,
		field.NumberList("availableSizes").
			Min(0).Max(50). // Limit each size's value.
			MaxRows(20),    // Allow up to twenty sizes.
	},
}

var UniqueTags = Tags.Validate(noRepeatedTags)

func noRepeatedTags(
	_ operation.Context,
	value operation.Value[[]string],
) ([]operation.Issue, error) {
	tags, present := value.Get()
	if !present {
		return nil, nil // This optional field can be left empty.
	}
	seen := make(map[string]bool)
	for _, tag := range tags {
		// Compare without case; keep the author's original spelling.
		key := strings.ToLower(tag)
		if seen[key] {
			return []operation.Issue{{
				Code:    "duplicate_tag",
				Message: "Each tag must be unique, regardless of case.",
			}}, nil
		}
		seen[key] = true
	}
	return nil, nil
}
