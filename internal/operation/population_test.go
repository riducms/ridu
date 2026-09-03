package operation

import (
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestPreparePopulationSelectionSeparatesMetadataAcrossPolymorphicTargets(t *testing.T) {
	plainName := mustPopulationPath(t, "name")
	versionedTitle := mustPopulationPath(t, "title")
	trashCaption := mustPopulationPath(t, "caption")
	collections := map[schema.StableID]schema.Collection{
		"plain": {
			ID:     "plain",
			Fields: []schema.Field{{Name: "name", Path: plainName, Type: schema.FieldTypeText}},
		},
		"versioned": {
			ID:           "versioned",
			Capabilities: schema.Capabilities{Versions: true},
			Versions:     &schema.VersionSettings{},
			Fields:       []schema.Field{{Name: "title", Path: versionedTitle, Type: schema.FieldTypeText}},
		},
		"trash": {
			ID:           "trash",
			Capabilities: schema.Capabilities{Trash: true},
			Fields:       []schema.Field{{Name: "caption", Path: trashCaption, Type: schema.FieldTypeText}},
		},
	}
	relationship := &schema.RelationshipField{Polymorphic: true, Targets: []schema.RelationshipTarget{
		{CollectionID: "plain", CollectionSlug: "plain"},
		{CollectionID: "versioned", CollectionSlug: "versioned"},
		{CollectionID: "trash", CollectionSlug: "trash"},
	}}

	selected := []query.Path{
		mustPopulationPath(t, "id"),
		mustPopulationPath(t, "createdAt"),
		mustPopulationPath(t, "updatedAt"),
		mustPopulationPath(t, "deletedAt"),
		mustPopulationPath(t, "_status"),
		mustPopulationPath(t, "_revision"),
		plainName,
		versionedTitle,
		trashCaption,
	}
	prepared, selectionError := preparePopulationSelection(collections, relationship, selected)
	if selectionError != nil {
		t.Fatal(selectionError)
	}
	if got, want := populationPathStrings(prepared), []string{"name", "title", "caption"}; !equalPopulationStrings(got, want) {
		t.Fatalf("prepared selection = %v, want %v", got, want)
	}

	metadataOnly, selectionError := preparePopulationSelection(collections, relationship, selected[:6])
	if selectionError != nil {
		t.Fatal(selectionError)
	}
	if metadataOnly == nil || len(metadataOnly) != 0 {
		t.Fatalf("metadata-only selection = %#v, want non-nil empty selection", metadataOnly)
	}
	complete, selectionError := preparePopulationSelection(collections, relationship, nil)
	if selectionError != nil || complete != nil {
		t.Fatalf("omitted selection = %#v, %v; want nil", complete, selectionError)
	}
}

func TestPreparePopulationSelectionRejectsUnavailableTargetMetadata(t *testing.T) {
	collections := map[schema.StableID]schema.Collection{
		"plain":     {ID: "plain"},
		"versioned": {ID: "versioned", Capabilities: schema.Capabilities{Versions: true}, Versions: &schema.VersionSettings{}},
		"trash":     {ID: "trash", Capabilities: schema.Capabilities{Trash: true}},
	}
	tests := []struct {
		name         string
		relationship *schema.RelationshipField
		field        string
	}{
		{name: "plain status", relationship: &schema.RelationshipField{CollectionID: "plain"}, field: "_status"},
		{name: "plain revision", relationship: &schema.RelationshipField{CollectionID: "plain"}, field: "_revision"},
		{name: "plain deletion", relationship: &schema.RelationshipField{CollectionID: "plain"}, field: "deletedAt"},
		{name: "versioned deletion", relationship: &schema.RelationshipField{CollectionID: "versioned"}, field: "deletedAt"},
		{name: "trash status", relationship: &schema.RelationshipField{CollectionID: "trash"}, field: "_status"},
		{name: "trash revision", relationship: &schema.RelationshipField{CollectionID: "trash"}, field: "_revision"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, selectionError := preparePopulationSelection(collections, test.relationship, []query.Path{mustPopulationPath(t, test.field)}); selectionError == nil {
				t.Fatalf("selection of %q unexpectedly succeeded", test.field)
			}
		})
	}
}

func mustPopulationPath(t *testing.T, segments ...string) query.Path {
	t.Helper()
	path, err := query.NewPath(segments...)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func populationPathStrings(paths []query.Path) []string {
	result := make([]string, len(paths))
	for index, path := range paths {
		result[index] = path.String()
	}
	return result
}

func equalPopulationStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
