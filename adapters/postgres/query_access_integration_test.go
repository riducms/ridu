package postgres_test

import (
	"testing"

	queryaccess "github.com/riducms/ridu/tests/contracts/query_access"
)

func TestPostgresQueryAccessConfidentiality(t *testing.T) {
	queryaccess.Run(t, postgresRichTextBlocksFactory, queryaccess.Options{})
}
