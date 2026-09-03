package postgres

import (
	"fmt"
	"strings"
	"testing"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPostgresUploadReferenceStatementCoalescesCollectionsAndVersions(t *testing.T) {
	versioned := &schema.VersionSettings{MaxPerDocument: 10}
	request := store.UploadReferenceRequest{
		Collections: []schema.Collection{
			uploadReferenceCollection("media-b", versioned),
			uploadReferenceCollection("media-a", nil),
		},
		ObjectKeys: []string{"object-b", "object-a"},
	}
	statement, arguments, err := postgresUploadReferenceStatement(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(arguments) != 2 {
		t.Fatalf("arguments = %#v", arguments)
	}
	if got := strings.Count(statement, "SELECT DISTINCT object_key"); got != 1 {
		t.Fatalf("statement performs %d outer reference queries: %s", got, statement)
	}
	if got := strings.Count(statement, "ridu_versions"); got != 2 {
		t.Fatalf("version branches = %d in %s", got, statement)
	}
	if got := strings.Count(statement, " = ANY($1::text[])"); got != 3 {
		t.Fatalf("candidate predicates = %d in %s", got, statement)
	}
	if got := strings.Count(statement, "jsonb_path_query_array("); got != 3 || strings.Contains(statement, "jsonb_each") {
		t.Fatalf("indexed variant predicates = %d in %s", got, statement)
	}
	mediaATable := quote(collectionTable("media-a"))
	mediaBTable := quote(collectionTable("media-b"))
	if strings.Index(statement, mediaATable) > strings.Index(statement, mediaBTable) {
		t.Fatalf("collection branches are not deterministic: %s", statement)
	}
}

func TestPostgresUploadReferenceStatementRejectsUnboundedCandidates(t *testing.T) {
	keys := make([]string, store.MaxUploadReferenceCandidates+1)
	for index := range keys {
		keys[index] = fmt.Sprintf("object-%d", index)
	}
	_, _, err := postgresUploadReferenceStatement(store.UploadReferenceRequest{
		Collections: []schema.Collection{uploadReferenceCollection("media", nil)},
		ObjectKeys:  keys,
	})
	if err == nil || !strings.Contains(err.Error(), fmt.Sprint(store.MaxUploadReferenceCandidates)) {
		t.Fatalf("unbounded candidate error = %v", err)
	}
}

func uploadReferenceCollection(id schema.StableID, versions *schema.VersionSettings) schema.Collection {
	return schema.Collection{
		ID: id, Slug: schema.CollectionSlug(id), Upload: &schema.UploadSettings{}, Versions: versions,
		Fields: []schema.Field{
			{ID: "upload-object-key", Name: "objectKey", Category: schema.FieldCategoryUpload},
			{ID: "upload-sizes", Name: "sizes", Category: schema.FieldCategoryUpload},
		},
	}
}
