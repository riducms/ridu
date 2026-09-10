package core_test

import (
	"errors"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

func TestReferenceDeleteActionsResolveSafeDefaults(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{Name: "Reference deletes", Collections: []ridu.Collection{
		{Slug: "users", Fields: field.Fields{field.Text("name")}},
		{Slug: "teams", Fields: field.Fields{field.Text("name")}},
		{Slug: "media", Upload: true, Fields: field.Fields{field.Text("alt")}},
		{Slug: "posts", Fields: field.Fields{field.Relationship("optionalOwner", "users"), field.Relationship("requiredOwner", "users").Required(), field.Relationships("related", "users"), field.Relationships("requiredRelated", "users").Required(), field.PolymorphicRelationship("subject", "users", "teams").Required(), field.Upload("asset", "media"), field.Upload("requiredAsset", "media").Required(), field.Uploads("requiredAssets", "media").Required(), field.Relationship("protectedOwner", "users").OnDelete(field.ReferenceDeleteRestrict)}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	posts := manifest.Snapshot().Collections[3]
	actions := make(map[string]schema.ReferenceDeleteAction, len(posts.Fields))
	for _, candidate := range posts.Fields {
		if candidate.Relationship != nil {
			actions[candidate.Name] = candidate.Relationship.OnDelete
		}
		if candidate.Upload != nil {
			actions[candidate.Name] = candidate.Upload.OnDelete
		}
	}
	want := map[string]schema.ReferenceDeleteAction{
		"optionalOwner":   schema.ReferenceDeleteNullify,
		"requiredOwner":   schema.ReferenceDeleteRestrict,
		"related":         schema.ReferenceDeleteNullify,
		"requiredRelated": schema.ReferenceDeleteRestrict,
		"subject":         schema.ReferenceDeleteRestrict,
		"asset":           schema.ReferenceDeleteNullify,
		"requiredAsset":   schema.ReferenceDeleteRestrict,
		"requiredAssets":  schema.ReferenceDeleteRestrict,
		"protectedOwner":  schema.ReferenceDeleteRestrict,
	}
	for name, action := range want {
		if actions[name] != action {
			t.Errorf("%s onDelete = %q, want %q", name, actions[name], action)
		}
	}
}

func TestReferenceDeleteActionsRejectUnsafeRequiredNullifyAndInvalidValues(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{Name: "Reference deletes", Collections: []ridu.Collection{
		{Slug: "users", Fields: field.Fields{field.Text("name")}},
		{Slug: "media", Upload: true, Fields: field.Fields{field.Text("alt")}},
		{Slug: "posts", Fields: field.Fields{field.Relationship("owner", "users").Required().OnDelete(field.ReferenceDeleteNullify), field.Upload("asset", "media").Required().OnDelete(field.ReferenceDeleteNullify), field.Relationships("related", "users").Required().OnDelete(field.ReferenceDeleteNullify), field.PolymorphicRelationship("subject", "users", "media").Required().OnDelete(field.ReferenceDeleteNullify), field.Uploads("assets", "media").Required().OnDelete(field.ReferenceDeleteNullify), field.Relationship("invalid", "users").OnDelete(field.ReferenceDeleteAction("cascade"))}},
	}})
	var validation *schema.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("Resolve error = %T, want validation error", err)
	}
	want := map[string]bool{
		"collections[2].fields[0].onDelete": false,
		"collections[2].fields[1].onDelete": false,
		"collections[2].fields[2].onDelete": false,
		"collections[2].fields[3].onDelete": false,
		"collections[2].fields[4].onDelete": false,
		"collections[2].fields[5].onDelete": false,
	}
	for _, issue := range validation.Issues {
		if _, exists := want[issue.Path]; exists {
			want[issue.Path] = true
		}
	}
	for path, found := range want {
		if !found {
			t.Errorf("missing issue at %s in %#v", path, validation.Issues)
		}
	}
}
