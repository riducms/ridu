package field_test

import (
	"reflect"
	"testing"

	"github.com/riducms/ridu/field"
)

func TestConcreteMethodSetsExcludeImpossibleIndexPolicies(t *testing.T) {
	for _, node := range (field.Fields{
		field.MultiSelect("tags", "news"),
		field.Relationships("authors", "users"),
		field.Uploads("files", "media"),
		field.PolymorphicRelationship("owner", "users", "teams"),
		field.PolymorphicRelationships("owners", "users", "teams"),
	}) {
		kind := reflect.TypeOf(node)
		for _, method := range []string{"Unique", "Index"} {
			if _, exposed := kind.MethodByName(method); exposed {
				t.Errorf("%s exposes %s although that policy cannot be enabled", kind, method)
			}
		}
	}
	for _, node := range (field.Fields{field.Text("title"), field.Select("status", "draft"), field.Relationship("author", "users"), field.Upload("file", "media")}) {
		for _, method := range []string{"Unique", "Index"} {
			if _, exposed := reflect.TypeOf(node).MethodByName(method); !exposed {
				t.Errorf("%T lost its supported %s policy", node, method)
			}
		}
	}
}

func TestDateValueFormatSurvivesFactoryRefinement(t *testing.T) {
	base := field.Date("startsAt").Format(field.DateTime).Required().Admin(field.Admin{Description: "Original"})
	refined := base.Rename("endsAt").Admin(field.Admin{Description: "Replacement"})
	if field.Snapshot(base).DateFormat() != field.DateTime || field.Snapshot(refined).DateFormat() != field.DateTime {
		t.Fatal("presentation or rename changed a date factory's value contract")
	}
	if field.Snapshot(base).Description() != "Original" || field.Snapshot(refined).Description() != "Replacement" {
		t.Fatal("admin replacement mutated the factory or failed to apply")
	}
	if _, exposed := reflect.TypeFor[field.TextField]().MethodByName("Format"); exposed {
		t.Fatal("date format is exposed on a text field")
	}
}
