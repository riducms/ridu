package mongodb

import (
	"bytes"
	"github.com/riducms/ridu/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
	"testing"
)

func TestBlockGeneratedNameIsMetadataOnly(t *testing.T) {
	before := phaseOneBlocks(t, nil)
	after := phaseOneBlocks(t, func(hero *schema.BlockType) { hero.TypeName = "Banner" })
	if err := validateMongoDBAdditiveFields("posts", []schema.Field{before}, []schema.Field{after}); err != nil {
		t.Fatal(err)
	}
	a, err := bson.MarshalExtJSON(mongoRepeatedRootJSONSchema(before, nil, mongoRowFields), false, false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := bson.MarshalExtJSON(mongoRepeatedRootJSONSchema(after, nil, mongoRowFields), false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("generated name changed stored validator")
	}
}
