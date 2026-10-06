package postgres_test

import (
	"testing"

	nestedpaths "github.com/riducms/ridu/tests/contracts/nested_paths"
)

func TestPostgresNestedQueryPaths(t *testing.T) {
	nestedpaths.Run(t, postgresRichTextBlocksFactory)
}
