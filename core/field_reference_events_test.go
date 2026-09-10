package core_test

import (
	"reflect"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func TestReferenceObserversRetainWriteCarriersAfterPopulation(t *testing.T) {
	var singular []operation.ID
	var repeated [][]operation.ID
	fields := field.Fields{
		field.Relationship("owner", "people").Hooks(field.Hooks[operation.ID]{AfterOperation: []field.Observer[operation.ID]{func(_ operation.EventContext, input operation.Value[operation.ID]) error {
			value, _ := input.Get()
			singular = append(singular, value)
			return nil
		}}}),
		field.Relationships("reviewers", "people").Hooks(field.Hooks[[]operation.ID]{AfterOperation: []field.Observer[[]operation.ID]{func(_ operation.EventContext, input operation.Value[[]operation.ID]) error {
			value, _ := input.Get()
			repeated = append(repeated, value)
			return nil
		}}}),
	}
	app, err := ridu.New(ridu.Config{Name: "Reference event carriers", Collections: []ridu.Collection{{Slug: "people", Fields: field.Fields{field.Text("name")}}, {Slug: "posts", Fields: fields}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	person, err := app.Local().Create(t.Context(), "people", store.Values{"name": store.String("Editor")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	post, err := app.Local().Create(t.Context(), "posts", store.Values{"owner": store.String(person.ID), "reviewers": store.List(store.String(person.ID))}, nil)
	if err != nil {
		t.Fatal(err)
	}
	singular, repeated = nil, nil
	owner, _ := query.NewPath("owner")
	reviewers, _ := query.NewPath("reviewers")
	found, err := app.Local().FindWithOptions(t.Context(), "posts", post.ID, ridu.FindOptions{Populate: []query.Population{{Path: owner}, {Path: reviewers}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := found.Values["owner"].CopyDocument(); !ok {
		t.Fatal("requested relationship was not populated")
	}
	if !reflect.DeepEqual(singular, []operation.ID{operation.ID(person.ID)}) || !reflect.DeepEqual(repeated, [][]operation.ID{{operation.ID(person.ID)}}) {
		t.Fatalf("observer carriers: singular=%v repeated=%v", singular, repeated)
	}
}
