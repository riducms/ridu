package sqlite_test

import (
	"testing"

	queryaccess "github.com/riducms/ridu/tests/contracts/query_access"
)

func TestSQLiteQueryAccessConfidentiality(t *testing.T) {
	queryaccess.Run(t, sqliteRichTextBlocksFactory, queryaccess.Options{OpaqueJSONDescendants: true})
}
