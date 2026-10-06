package postgres_test

import (
	"testing"

	querysemantics "github.com/riducms/ridu/tests/contracts/query_semantics"
)

func TestPostgresQuerySemantics(t *testing.T) {
	querysemantics.Run(t, postgresRichTextBlocksFactory, querysemantics.Options{})
}
