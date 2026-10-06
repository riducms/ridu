// Package schematest builds bound schemas for tests that construct them in Go
// rather than resolving configuration.
package schematest

import (
	"testing"

	"github.com/riducms/ridu/schema"
)

// Bind returns fields bound as the fields of a collection with ID resource,
// whose containers select definitions from blocks, as schema.Parse binds a
// manifest's resources. Definitions use definition-relative paths and IDs;
// placement views derive the placement's own. The graph is validated first.
func Bind(t testing.TB, resource schema.StableID, blocks []schema.BlockType, fields ...schema.Field) []schema.Field {
	t.Helper()
	snapshot := schema.Snapshot{Blocks: blocks, Collections: []schema.Collection{{ID: resource, Slug: schema.CollectionSlug(resource), Fields: fields}}}
	if err := schema.BindBlockReferences(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot.Collections[0].Fields
}
