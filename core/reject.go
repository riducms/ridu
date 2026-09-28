package core

import (
	"errors"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Reject stops an operation from a collection, global, or field hook with a
// message for the person who made the request. The admin shows the message,
// and REST, the SDK, and the Local API return it as a "rejected" error
// (HTTP 422). Any other error from a hook is reported as an internal failure
// whose message stays on the server.
//
// Issues mark the fields to fix. In a field hook, an issue's Target starts at
// that field; in a collection or global hook, it starts at the document root,
// as in operation.At("featured"). Return Reject before the transaction commits:
// an AfterCommit hook cannot undo a saved change.
func Reject(message string, issues ...operation.Issue) error {
	return &rejection{message: message, issues: append([]operation.Issue(nil), issues...)}
}

type rejection struct {
	message string
	issues  []operation.Issue
}

func (rejection *rejection) Error() string { return rejection.message }

type issueTargetResolver func(operation.IssueTarget) (schema.Field, string, schema.LocaleCode, error)

// lowerRejection turns a hook's Reject into the engine's caller-facing
// rejection, resolving each issue target against the hook's own scope. Other
// errors pass through unchanged.
func lowerRejection(err error, ctx operationengine.Context, resolve issueTargetResolver) error {
	var rejected *rejection
	if !errors.As(err, &rejected) {
		return err
	}
	collectionID, globalID := resourceIDs(ctx.Collection)
	issues := make([]schema.Issue, len(rejected.issues))
	for index, issue := range rejected.issues {
		field, path, locale, resolveError := resolve(issue.Target)
		if resolveError != nil {
			return resolveError
		}
		code := issue.Code
		if code == "" {
			code = "rejected"
		}
		issues[index] = schema.Issue{Code: code, Path: path, Message: issue.Message, FieldID: field.ID, CollectionID: collectionID, GlobalID: globalID, Locale: locale}
	}
	message := rejected.message
	if message == "" {
		message = "the operation was rejected"
	}
	return &operationengine.Rejection{Message: message, Issues: issues}
}

// resourceHookValues returns the document a resource hook can see: the values
// being saved, or the saved document during reads and deletes.
func resourceHookValues(ctx operationengine.Context) store.Values {
	switch {
	case len(ctx.Data) != 0:
		return ctx.Data
	case ctx.Document != nil:
		return ctx.Document.Values
	case ctx.Original != nil:
		return ctx.Original.Values
	default:
		return store.Values{}
	}
}
