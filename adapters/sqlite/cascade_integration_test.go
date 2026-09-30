package sqlite_test

import (
	"testing"

	"github.com/riducms/ridu/tests/contracts/cascade"
)

func TestSQLiteCascadeDeletes(t *testing.T) {
	cascade.Run(t, cascade.Factory(sqliteRichTextBlocksFactory))
}
