package mongodb

import (
	"testing"

	querysemantics "github.com/riducms/ridu/tests/contracts/query_semantics"
)

func TestMongoDBQuerySemantics(t *testing.T) {
	querysemantics.Run(t, mongoRichTextBlocksFactory, querysemantics.Options{JSONSubPathsUnsupported: true})
}
