package operation

import (
	"context"
	"testing"

	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

type referenceCountingTransaction struct {
	store.Transaction
	findCalls int
}

func (transaction *referenceCountingTransaction) Find(context.Context, store.Request) (store.Document, error) {
	transaction.findCalls++
	return store.Document{}, store.ErrNotFound
}

func TestReferenceAdmissionRejectsOverLimitBeforeStoreLookups(t *testing.T) {
	field := schema.Field{
		Name: "related", Type: schema.FieldTypeRelationship,
		Relationship: &schema.RelationshipField{CollectionID: "targets", CollectionSlug: "targets", HasMany: true},
	}
	items := make([]store.Value, MaxDocumentReferences+1)
	for index := range items {
		items[index] = store.String("target")
	}
	transaction := &referenceCountingTransaction{}
	issues, err := (&Engine{}).validateDocumentReferences(
		Context{Context: context.Background(), Collection: schema.Collection{Fields: []schema.Field{field}}},
		transaction,
		store.Values{"related": store.List(items...)},
		localization.Selection{},
	)
	if err != nil {
		t.Fatalf("validate references: %v", err)
	}
	if transaction.findCalls != 0 {
		t.Fatalf("expected no store lookups above the reference limit, got %d", transaction.findCalls)
	}
	if len(issues) != 1 || issues[0].Code != "max_references" || issues[0].Path != "related.512" {
		t.Fatalf("unexpected issues: %#v", issues)
	}
}

func TestHasManyReferenceFieldRejectsCardinalityAboveEngineLimit(t *testing.T) {
	field := schema.Field{
		Name: "related", Type: schema.FieldTypeRelationship,
		Relationship: &schema.RelationshipField{CollectionID: "targets", CollectionSlug: "targets", HasMany: true},
	}
	items := make([]store.Value, MaxDocumentReferences+1)
	for index := range items {
		items[index] = store.String("target")
	}
	_, issues := validate([]schema.Field{field}, store.Values{"related": store.List(items...)}, true, nil)
	if len(issues) != 1 || issues[0].Code != "max_references" || issues[0].Path != "related" {
		t.Fatalf("unexpected issues: %#v", issues)
	}
}
