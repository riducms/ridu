package schema_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestCurrentManifestRequiresSafeResolvedReferenceDeleteActions(t *testing.T) {
	ownerPath, _ := query.NewPath("owner")
	base := schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Reference deletes"},
		Collections: []schema.Collection{
			{ID: "users", Slug: "users", Fields: []schema.Field{}},
			{ID: "posts", Slug: "posts", Fields: []schema.Field{{
				ID: "posts-owner", Name: "owner", Path: ownerPath, Type: schema.FieldTypeRelationship,
				Category:     schema.FieldCategoryRelationship,
				Relationship: &schema.RelationshipField{CollectionID: "users", CollectionSlug: "users"},
			}}},
		},
		Plugins: []schema.Plugin{},
	}
	encoded, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schema.Parse(encoded); err == nil || !strings.Contains(err.Error(), "onDelete") {
		t.Fatalf("missing action error = %v", err)
	}

	base.Collections[1].Fields[0].Required = true
	base.Collections[1].Fields[0].Relationship.OnDelete = schema.ReferenceDeleteNullify
	encoded, _ = json.Marshal(base)
	if _, err := schema.Parse(encoded); err == nil || !strings.Contains(err.Error(), "cannot be nullified") {
		t.Fatalf("unsafe nullify error = %v", err)
	}
	base.Collections[1].Fields[0].Relationship.HasMany = true
	encoded, _ = json.Marshal(base)
	if _, err := schema.Parse(encoded); err == nil || !strings.Contains(err.Error(), "cannot be nullified") {
		t.Fatalf("unsafe required has-many nullify error = %v", err)
	}

	polymorphic := schema.NewManifest(base).Snapshot()
	polymorphic.Collections[1].Fields[0].Relationship.CollectionID = ""
	polymorphic.Collections[1].Fields[0].Relationship.CollectionSlug = ""
	polymorphic.Collections[1].Fields[0].Relationship.HasMany = false
	polymorphic.Collections[1].Fields[0].Relationship.Polymorphic = true
	polymorphic.Collections[1].Fields[0].Relationship.Targets = []schema.RelationshipTarget{
		{CollectionID: "users", CollectionSlug: "users"},
		{CollectionID: "posts", CollectionSlug: "posts"},
	}
	encoded, _ = json.Marshal(polymorphic)
	if _, err := schema.Parse(encoded); err == nil || !strings.Contains(err.Error(), "cannot be nullified") {
		t.Fatalf("unsafe required polymorphic nullify error = %v", err)
	}

	upload := schema.NewManifest(base).Snapshot()
	upload.Collections[1].Fields[0].Type = schema.FieldTypeUpload
	upload.Collections[1].Fields[0].Category = schema.FieldCategoryUpload
	upload.Collections[1].Fields[0].Relationship = nil
	upload.Collections[1].Fields[0].Upload = &schema.UploadField{
		CollectionID: "users", CollectionSlug: "users", HasMany: true, OnDelete: schema.ReferenceDeleteNullify,
	}
	encoded, _ = json.Marshal(upload)
	if _, err := schema.Parse(encoded); err == nil || !strings.Contains(err.Error(), "cannot be nullified") {
		t.Fatalf("unsafe required upload-list nullify error = %v", err)
	}

	base.Collections[1].Fields[0].Relationship.HasMany = false
	base.Collections[1].Fields[0].Relationship.OnDelete = schema.ReferenceDeleteRestrict
	encoded, _ = json.Marshal(base)
	manifest, err := schema.Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got := manifest.Snapshot().Collections[1].Fields[0].Relationship.OnDelete; got != schema.ReferenceDeleteRestrict {
		t.Fatalf("onDelete = %q", got)
	}
}
