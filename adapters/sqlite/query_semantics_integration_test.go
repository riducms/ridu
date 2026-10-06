package sqlite_test

import (
	"testing"

	querysemantics "github.com/riducms/ridu/tests/contracts/query_semantics"
)

func TestSQLiteQuerySemantics(t *testing.T) {
	querysemantics.Run(t, sqliteRichTextBlocksFactory, querysemantics.Options{})
}
