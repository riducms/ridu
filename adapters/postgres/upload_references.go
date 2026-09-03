package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// ReferencedUploadObjects resolves one bounded candidate set in a single SQL
// round trip. The statement unions current/trash rows and immutable versions,
// so callers never have to paginate or decode the repository to protect an
// object-store deletion.
func (transaction *documentTransaction) ReferencedUploadObjects(ctx context.Context, request store.UploadReferenceRequest) ([]string, error) {
	statement, arguments, err := postgresUploadReferenceStatement(request)
	if err != nil {
		return nil, err
	}
	rows, err := transaction.transaction.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	result := make([]string, 0, len(request.ObjectKeys))
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		result = append(result, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func postgresUploadReferenceStatement(request store.UploadReferenceRequest) (string, []any, error) {
	if len(request.ObjectKeys) == 0 || len(request.ObjectKeys) > store.MaxUploadReferenceCandidates {
		return "", nil, fmt.Errorf("upload reference lookup requires between 1 and %d object keys", store.MaxUploadReferenceCandidates)
	}
	seenKeys := make(map[string]struct{}, len(request.ObjectKeys))
	for _, key := range request.ObjectKeys {
		if key == "" {
			return "", nil, fmt.Errorf("upload reference lookup contains an empty object key")
		}
		if _, duplicate := seenKeys[key]; duplicate {
			return "", nil, fmt.Errorf("upload reference lookup contains duplicate object key %q", key)
		}
		seenKeys[key] = struct{}{}
	}
	collections := append([]schema.Collection(nil), request.Collections...)
	sort.Slice(collections, func(left, right int) bool { return collections[left].ID < collections[right].ID })
	parts := make([]string, 0, len(collections)*2+2)
	versionCollections := make([]string, 0, len(collections))
	seenCollections := make(map[schema.StableID]struct{}, len(collections))
	for _, collection := range collections {
		if collection.Upload == nil {
			return "", nil, fmt.Errorf("upload reference lookup collection %q is not upload-enabled", collection.ID)
		}
		if _, duplicate := seenCollections[collection.ID]; duplicate {
			return "", nil, fmt.Errorf("upload reference lookup contains duplicate collection %q", collection.ID)
		}
		seenCollections[collection.ID] = struct{}{}
		objectKey, objectKeyFound := uploadMetadataFieldColumn(collection, "objectKey")
		sizes, sizesFound := uploadMetadataFieldColumn(collection, "sizes")
		if !objectKeyFound || !sizesFound {
			return "", nil, fmt.Errorf("upload collection %q is missing framework object metadata", collection.ID)
		}
		table := quote(collectionTable(collection.ID))
		objectColumn := quote(objectKey)
		sizesColumn := quote(sizes)
		parts = append(parts,
			fmt.Sprintf("SELECT %s AS object_key FROM %s WHERE %s = ANY($1::text[])", objectColumn, table, objectColumn),
			fmt.Sprintf("SELECT candidate.object_key FROM unnest($1::text[]) AS candidate(object_key) WHERE EXISTS (SELECT 1 FROM %s WHERE jsonb_path_query_array(%s, '$.*.\"objectKey\"'::jsonpath) ? candidate.object_key)", table, sizesColumn),
		)
		if collection.Versions != nil {
			versionCollections = append(versionCollections, string(collection.ID))
		}
	}
	arguments := []any{append([]string(nil), request.ObjectKeys...)}
	if len(versionCollections) != 0 {
		arguments = append(arguments, versionCollections)
		parts = append(parts,
			"SELECT snapshot #>> '{Values,objectKey}'::text[] AS object_key FROM ridu_versions WHERE collection_id = ANY($2::text[]) AND snapshot #>> '{Values,objectKey}'::text[] = ANY($1::text[])",
			"SELECT candidate.object_key FROM unnest($1::text[]) AS candidate(object_key) WHERE EXISTS (SELECT 1 FROM ridu_versions WHERE collection_id = ANY($2::text[]) AND jsonb_path_query_array(snapshot #> '{Values,sizes}'::text[], '$.*.\"objectKey\"'::jsonpath) ? candidate.object_key)",
		)
	}
	if len(parts) == 0 {
		return "", nil, fmt.Errorf("upload reference lookup requires at least one upload collection")
	}
	return "SELECT DISTINCT object_key FROM (" + strings.Join(parts, " UNION ALL ") + ") AS upload_references WHERE object_key IS NOT NULL ORDER BY object_key", arguments, nil
}

func uploadMetadataFieldColumn(collection schema.Collection, name string) (string, bool) {
	for _, field := range collection.Fields {
		if field.Name == name && field.Category == schema.FieldCategoryUpload {
			return fieldColumn(field.ID), true
		}
	}
	return "", false
}

var _ store.UploadReferenceTransaction = (*documentTransaction)(nil)
