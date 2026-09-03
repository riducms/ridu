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
		{Slug: "users", Fields: []field.Definition{field.Text("name")}},
		{Slug: "teams", Fields: []field.Definition{field.Text("name")}},
		{Slug: "media", Upload: true, Fields: []field.Definition{field.Text("alt")}},
		{Slug: "posts", Fields: []field.Definition{
			field.Relationship("optionalOwner", field.To("users")),
			field.Relationship("requiredOwner", field.To("users"), field.Required()),
			field.Relationship("related", field.To("users"), field.HasMany()),
			field.Relationship("requiredRelated", field.To("users"), field.HasMany(), field.Required()),
			field.Relationship("subject", field.ToAny("users", "teams"), field.Required()),
			field.Upload("asset", field.To("media")),
			field.Upload("requiredAsset", field.To("media"), field.Required()),
			field.Upload("requiredAssets", field.To("media"), field.HasMany(), field.Required()),
			field.Relationship("protectedOwner", field.To("users"), field.OnDelete(field.ReferenceDeleteRestrict)),
		}},
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
		{Slug: "users", Fields: []field.Definition{field.Text("name")}},
		{Slug: "media", Upload: true, Fields: []field.Definition{field.Text("alt")}},
		{Slug: "posts", Fields: []field.Definition{
			field.Relationship("owner", field.To("users"), field.Required(), field.OnDelete(field.ReferenceDeleteNullify)),
			field.Upload("asset", field.To("media"), field.Required(), field.OnDelete(field.ReferenceDeleteNullify)),
			field.Relationship("related", field.ToMany("users"), field.Required(), field.OnDelete(field.ReferenceDeleteNullify)),
			field.Relationship("subject", field.ToAny("users", "media"), field.Required(), field.OnDelete(field.ReferenceDeleteNullify)),
			field.Upload("assets", field.ToMany("media"), field.Required(), field.OnDelete(field.ReferenceDeleteNullify)),
			field.Relationship("invalid", field.To("users"), field.OnDelete(field.ReferenceDeleteAction("cascade"))),
			field.Relationship("duplicate", field.To("users"), field.OnDelete(field.ReferenceDeleteRestrict), field.OnDelete(field.ReferenceDeleteRestrict)),
		}},
	}})
	var validation *schema.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("Resolve error = %T, want validation error", err)
	}
	want := map[string]bool{
		"collections[2].fields[0].options.onDelete": false,
		"collections[2].fields[1].options.onDelete": false,
		"collections[2].fields[2].options.onDelete": false,
		"collections[2].fields[3].options.onDelete": false,
		"collections[2].fields[4].options.onDelete": false,
		"collections[2].fields[5].options.onDelete": false,
		"collections[2].fields[6].options.onDelete": false,
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
