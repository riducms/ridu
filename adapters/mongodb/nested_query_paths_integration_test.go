package mongodb

import (
	"testing"

	nestedpaths "github.com/riducms/ridu/tests/contracts/nested_paths"
)

func TestMongoDBNestedQueryPaths(t *testing.T) {
	nestedpaths.Run(t, mongoRichTextBlocksFactory)
}
