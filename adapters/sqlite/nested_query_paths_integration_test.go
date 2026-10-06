package sqlite_test

import (
	"testing"

	nestedpaths "github.com/riducms/ridu/tests/contracts/nested_paths"
)

func TestSQLiteNestedQueryPaths(t *testing.T) {
	nestedpaths.Run(t, sqliteRichTextBlocksFactory)
}
