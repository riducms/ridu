package mongodb

import (
	"testing"

	queryaccess "github.com/riducms/ridu/tests/contracts/query_access"
)

func TestMongoDBQueryAccessConfidentiality(t *testing.T) {
	queryaccess.Run(t, mongoRichTextBlocksFactory, queryaccess.Options{})
}
