package operation

import (
	"testing"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestUploadMetadataReadCannotUseSubmittedDataToGrantRead(t *testing.T) {
	collection := liveTestCollection(liveTestField("source", schema.FieldTypeJSON))
	collection.Schema.Upload = &schema.UploadSettings{}
	collection.Access = map[operation.Kind]Access{
		operation.Read: func(ctx Context) (Decision, error) {
			if _, supplied := ctx.Data["grantRead"]; supplied {
				return Decision{Kind: Allow}, nil
			}
			return Decision{Kind: Deny}, nil
		},
	}
	engine, id := liveTestEngine(t, collection, store.Values{"source": store.Object(store.Values{"objectKey": store.String("secret")})})
	_, err := engine.ReadUploadMetadata(t.Context(), Request{Operation: operation.Update, Collection: "products", ID: id, Data: store.Values{"grantRead": store.Boolean(true)}})
	liveAssertStatus(t, err, 403)
}

func TestUploadMetadataReadIntersectsReadAndUpdatePredicates(t *testing.T) {
	owner := liveTestField("owner", schema.FieldTypeText)
	collection := liveTestCollection(owner, liveTestField("source", schema.FieldTypeJSON))
	collection.Schema.Upload = &schema.UploadSettings{}
	collection.Access = map[operation.Kind]Access{
		operation.Update: func(Context) (Decision, error) {
			node := query.Equal(owner.Path, query.String("editor-a")).Node()
			return Decision{Kind: Where, Access: &node}, nil
		},
	}
	engine, id := liveTestEngine(t, collection, store.Values{"owner": store.String("editor-b"), "source": store.Object(store.Values{"objectKey": store.String("private")})})
	_, err := engine.ReadUploadMetadata(t.Context(), Request{Operation: operation.Update, Collection: "products", ID: id})
	liveAssertStatus(t, err, 404)
}
