package content

import (
	"strings"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
)

var Workshops = ridu.Collection{
	Slug: "workshops",
	Admin: ridu.CollectionAdmin{
		UseAsTitle: "title",
	},
	Access: ridu.CollectionAccess{
		Admin:  authenticatedOnly,
		Create: authenticatedOnly,
		Read:   authenticatedOnly,
		Update: authenticatedOnly,
		Delete: authenticatedOnly,
	},
	Fields: append(workshopFields(),
		field.Array("sessions", workshopFields()).
			Label("Extra sessions").
			Admin(field.Admin{
				Description: "The same rule works inside each row. Try editing a title and moving that row.",
			}),
		richtext.Field("description", richtext.Config{
			Blocks: []field.Block{
				{
					Slug:   "workshop",
					Labels: field.BlockLabels{Singular: "Workshop card", Plural: "Workshop cards"},
					Fields: workshopFields(),
				},
			},
		}).
			Label("Description and embedded workshop card").
			Admin(field.Admin{
				Description: "Choose Edit on the seeded Workshop card. Its title is checked before you choose Apply.",
			}),
	),
}

// The same small factory works in a document, repeated rows and rich-text cards.
func workshopFields() field.Fields {
	return field.Fields{
		field.Select("city", "London", "Bristol").
			Required().
			Admin(field.Admin{
				Description: "After editing the title, change this city to refresh its feedback.",
			}),
		field.Text("title").
			Required().
			Label("Workshop title").
			Admin(field.Admin{
				Description: "Include the selected city, for example: London pottery evening. Try removing London, then wait a moment without saving.",
			}).
			Validate(func(
				ctx operation.ValidationContext,
				value operation.Value[string],
			) ([]operation.Issue, error) {
				return checkTitle(ctx.Siblings, value)
			}).
			LiveValidate(func(
				ctx operation.LiveValidationContext,
				value operation.Value[string],
			) ([]operation.Issue, error) {
				return checkTitle(ctx.Siblings, value)
			}),
	}
}

// One Go business rule, used both for advisory feedback and authoritative Save.
func checkTitle(siblings operation.View, value operation.Value[string]) ([]operation.Issue, error) {
	title, present := value.Get()
	city, selected := siblings.String("city")
	if !present || strings.TrimSpace(title) == "" || !selected || city == "" {
		return nil, nil // Required handles empty values when saving.
	}
	if !strings.Contains(strings.ToLower(title), strings.ToLower(city)) {
		return []operation.Issue{
			{
				Code:    "city_in_title",
				Message: "Include " + city + " in the workshop title so guests know where to go.",
			},
		}, nil
	}
	return nil, nil
}
