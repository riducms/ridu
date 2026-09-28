package operation

import (
	"context"
	"errors"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// uniqueCandidate records the values a failed write tried to store, so a
// conflict can be explained after the write transaction has rolled back.
type uniqueCandidate struct {
	collection schema.Collection
	documentID string
	values     store.Values
	locales    []schema.LocaleCode
}

// withUniqueCandidate attaches the attempted values to a store conflict.
func withUniqueCandidate(err error, candidate uniqueCandidate) error {
	var failure *Error
	if errors.As(err, &failure) && failure.Code == "conflict" && errors.Is(failure.Cause, store.ErrConflict) {
		failure.unique = &candidate
	}
	return err
}

// explainUniqueConflict turns a store conflict into a field issue when a
// committed document already holds one of the write's unique values. It runs
// only after the write's own transaction has ended, and it never reports a
// field the database has not already refused: when nothing matches, the
// original conflict is returned unchanged.
func (engine *Engine) explainUniqueConflict(ctx context.Context, err error) error {
	failure, direct := err.(*Error)
	if !direct || failure.unique == nil {
		return err
	}
	candidate := failure.unique
	transaction, beginError := engine.store.Begin(ctx)
	if beginError != nil {
		return err
	}
	defer transaction.Rollback(ctx)
	collectionID, globalID := resourceIDs(candidate.collection)
	var issues []schema.Issue
	for _, field := range candidate.collection.Fields {
		if !field.Unique || field.Localized {
			continue
		}
		value, comparable := uniqueQueryValue(candidate.values[field.Name])
		if !comparable {
			continue
		}
		path, pathError := query.NewPath(field.Name)
		if pathError != nil {
			continue
		}
		filter := query.Equal(path, value).Node()
		page, listError := transaction.List(ctx, store.Request{
			Collection: candidate.collection, Filter: &filter, Limit: 2,
			Deletion: store.DeletionAll, Locales: candidate.locales,
		})
		if listError != nil {
			return err
		}
		for _, document := range page.Documents {
			if document.ID == candidate.documentID {
				continue
			}
			issues = append(issues, schema.Issue{
				Code: "unique", Path: field.Name, Message: "Another document already uses this value.",
				FieldID: field.ID, CollectionID: collectionID, GlobalID: globalID,
			})
			break
		}
	}
	if len(issues) == 0 {
		return err
	}
	return &Error{Code: "validation", Status: 422, Message: "document validation failed", Issues: issues, Cause: err}
}

func uniqueQueryValue(value store.Value) (query.Value, bool) {
	if text, ok := value.StringValue(); ok {
		return query.String(text), true
	}
	if number, ok := value.NumberValue(); ok {
		return query.Number(number), true
	}
	if boolean, ok := value.BooleanValue(); ok {
		return query.Boolean(boolean), true
	}
	return query.Value{}, false
}

func resourceIDs(resource schema.Collection) (schema.StableID, schema.StableID) {
	if resource.Capabilities.Global {
		return "", resource.ID
	}
	return resource.ID, ""
}
