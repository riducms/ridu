package postgres_test

import (
	"testing"

	"github.com/riducms/ridu/tests/contracts/cascade"
)

func TestPostgresCascadeDeletes(t *testing.T) {
	cascade.Run(t, cascade.Factory(postgresRichTextBlocksFactory))
}
