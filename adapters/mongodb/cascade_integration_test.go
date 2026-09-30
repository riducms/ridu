package mongodb

import (
	"testing"

	"github.com/riducms/ridu/tests/contracts/cascade"
)

func TestMongoDBCascadeDeletes(t *testing.T) {
	cascade.Run(t, cascade.Factory(mongoRichTextBlocksFactory))
}
