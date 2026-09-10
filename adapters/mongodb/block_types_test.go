package mongodb

import (
	"bytes"
	"github.com/riducms/ridu/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
	"testing"
)

func TestBlockGeneratedNameIsMetadataOnly(t *testing.T) {
	before, after := phaseOneBlocks(), phaseOneBlocks()
	after.Blocks.ResolvedTypes()[0].TypeName = "Hero"
	if err := validateMongoDBAdditiveFields("posts", []schema.Field{before}, []schema.Field{after}); err != nil {
		t.Fatal(err)
	}
	a, err := bson.MarshalExtJSON(mongoRepeatedRootJSONSchema(before, nil), false, false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := bson.MarshalExtJSON(mongoRepeatedRootJSONSchema(after, nil), false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("generated name changed stored validator")
	}
}
