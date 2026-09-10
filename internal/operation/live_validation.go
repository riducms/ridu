package operation

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const MaxLiveValidationFields = 64
const MaxLiveValidationEmbeddedScopes = 8
const LiveValidationTimeout = 5 * time.Second

type LiveValidationRequest struct {
	Collection      string
	ID              string
	Data            store.Values
	Fields          []string
	Embedded        []LiveValidationEmbeddedScope
	Actor           *store.Document
	ActorCollection schema.CollectionSlug
	Locale          string
}

type LiveValidationEmbeddedScope struct {
	Field       string
	TreeKey     string
	CaseTag     string
	VariantSlug string
	Identity    string
	Data        store.Values
}

type LiveValidationEvaluation struct {
	Path   string
	Target string
	Status string
	Issues []schema.Issue
}

type LiveValidationResult struct{ Evaluations []LiveValidationEvaluation }

// LiveValidate evaluates explicitly attached read-only checks. It deliberately
// does not enter Execute: a browser snapshot is not a prepared write candidate.
func (engine *Engine) LiveValidate(ctx context.Context, request LiveValidationRequest) (result LiveValidationResult, err error) {
	if request.Data == nil || len(request.Fields) == 0 || len(request.Fields) > MaxLiveValidationFields || len(request.Embedded) > MaxLiveValidationEmbeddedScopes || len(request.ID) > 512 {
		return result, liveBadRequest("live validation requires data and between 1 and 64 field paths, with at most 8 embedded scopes")
	}
	seen := map[string]bool{}
	for _, path := range request.Fields {
		if !liveValidPath(path) || seen[path] {
			return result, liveBadRequest("live validation field paths must be distinct bounded display paths")
		}
		seen[path] = true
	}
	ctx, cancel := context.WithTimeout(ctx, LiveValidationTimeout)
	defer cancel()
	collection, exists := engine.collections[request.Collection]
	if !exists {
		return result, &Error{Code: "unknown_collection", Status: 404, Message: "resource was not found"}
	}
	selection, localeError := localization.Resolve(engine.localization, request.Locale, nil, engine.localization != nil, false)
	if localeError != nil {
		return result, &Error{Code: "bad_locale", Status: 400, Message: localeError.Error(), Cause: localeError}
	}
	if selection.All {
		return result, liveBadRequest("live validation requires one exact locale")
	}
	selection = exactUpdateSelection(selection)
	ctx = context.WithValue(ctx, liveContextKey{}, &liveEvaluationContext{locale: selection.Locale})
	state, owns, beginError := engine.transaction(ctx, transactionReadOnly)
	if beginError != nil {
		return result, transactionAdmissionError("begin live validation transaction", beginError)
	}
	ctx = context.WithValue(ctx, transactionKey{}, state)
	if owns {
		defer func() {
			if rollback := engine.rollbackTransactionState(ctx, state); rollback != nil {
				err = rollbackOperationError("rollback live validation transaction", err, rollback)
			}
		}()
	}
	kind := operation.Create
	if request.ID != "" || collection.Schema.Capabilities.Global {
		kind = operation.Update
	}
	base := Context{LiveValidation: true, Context: ctx, Operation: kind, Collection: collection.Schema, ID: request.ID, Actor: cloneDocumentPointer(request.Actor), ActorCollection: request.ActorCollection, Data: store.CloneValues(request.Data), Locale: selection.Locale, Locales: selection.Configured}
	if err := liveAuthorizeResource(collection, base, operation.Admin); err != nil {
		return result, err
	}
	var canonical *store.Document
	read := base
	read.Operation = operation.Read
	read.Data = store.Values{}
	decision, accessError := authorize(collection, read)
	if accessError != nil {
		return result, capabilityAccessError("read access rule failed", accessError)
	}
	if decision.Kind == Deny || request.ID == "" && decision.Kind != Allow {
		return result, liveDenied()
	}
	if request.ID != "" {
		document, findError := state.transaction.Find(ctx, store.Request{Collection: collection.Schema, Collections: engine.schemas, ID: request.ID, Access: decision.Access, Deletion: store.DeletionActive, PublishedOnly: request.Actor == nil, Locales: selection.Configured, LocaleChain: selection.Chain})
		if findError != nil {
			if !(collection.Schema.Capabilities.Global && decision.Kind == Allow && errors.Is(findError, store.ErrNotFound)) {
				return result, translateStoreError(findError)
			}
		} else {
			canonical = cloneDocumentPointer(&document)
		}
	}
	prior := store.Values{}
	if canonical != nil {
		projected, projectError := localization.ProjectDocumentChecked(*canonical, collection.Schema.Fields, selection)
		if projectError != nil {
			return result, liveBadRequest("stored embedded structure is unavailable for live validation")
		}
		prior = projected.Values
		base.Original = cloneDocumentPointer(&projected)
	}
	base.originalCanonical = canonical
	candidate := store.CloneValues(request.Data)
	if canonical != nil {
		candidate, err = completeUpdateForValidation(collection.Schema.Fields, canonical.Values, request.Data, selection, nil)
		if err != nil {
			return result, liveBadRequest("snapshot has an invalid embedded or localized structure")
		}
		completed := candidate
		candidate = store.CloneValues(prior)
		for name, value := range completed {
			candidate[name] = value
		}
	}
	if err := liveValidateStructure(collection.Schema.Fields, candidate, "", embedded.NewBudget()); err != nil {
		return result, err
	}
	base.Data = candidate
	decision, accessError = authorize(collection, base)
	if accessError != nil {
		return result, capabilityAccessError("write access rule failed", accessError)
	}
	if decision.Kind == Deny || kind == operation.Create && decision.Kind != Allow {
		return result, liveDenied()
	}
	if decision.Kind == Where {
		if request.ID == "" {
			return result, liveDenied()
		}
		if _, findError := state.transaction.Find(ctx, store.Request{Collection: collection.Schema, Collections: engine.schemas, ID: request.ID, Access: decision.Access, Deletion: store.DeletionActive, PublishedOnly: request.Actor == nil, Locales: selection.Configured, LocaleChain: selection.Chain}); findError != nil {
			return result, translateStoreError(findError)
		}
	}
	base.submittedData, base.submittedLocale = store.CloneValues(request.Data), selection.Locale
	if err := authorizeBoundFields(collection, base, kind, prior, candidate, false); err != nil {
		return result, err
	}
	scope, err := liveScopeViews(collection, base, candidate, prior, request.Data)
	if err != nil {
		return result, err
	}
	root := store.CloneValues(scope.data)
	base.RootData = root
	for _, selector := range request.Embedded {
		scope, err = liveDescendScope(scope, base, selector)
		if err != nil {
			return result, err
		}
	}
	base.RootData = root
	base.Data, base.Document, base.originalCanonical = scope.data, nil, nil
	base.Collection.Fields = scope.collection.Schema.Fields
	base.Original = &store.Document{Values: scope.prior}
	tokens := fieldIssueTargets(scope.collection.Schema.Fields, scope.data, false)
	entries := liveEntries(scope.collection, scope.data)
	inputs := liveEntries(scope.collection, scope.input)
	priors := liveEntries(scope.collection, scope.prior)
	priorTokens := fieldIssueTargets(scope.collection.Schema.Fields, scope.prior, false)
	result.Evaluations = make([]LiveValidationEvaluation, 0, len(request.Fields))
	issueCount, issueBytes := 0, 0
	for _, path := range request.Fields {
		if err := ctx.Err(); err != nil {
			return LiveValidationResult{}, liveOperational(err)
		}
		entry, found := entries[path]
		if !found {
			field, valid := liveUnavailableField(scope.collection.Schema.Fields, scope.data, strings.Split(path, "."))
			if !valid || !liveHasValidator(scope.collection, field.ID) {
				return LiveValidationResult{}, liveBadRequest("requested field is not an available live validation field")
			}
			if livePathDenied(scope.denied, path) {
				return LiveValidationResult{}, liveDenied()
			}
			result.Evaluations = append(result.Evaluations, LiveValidationEvaluation{Path: path, Status: "skipped", Issues: []schema.Issue{}})
			continue
		}
		if len(entry.binding.LiveValidators) == 0 {
			return LiveValidationResult{}, liveBadRequest("requested field has no live validation callback")
		}
		if livePathDenied(scope.denied, path) {
			return LiveValidationResult{}, liveDenied()
		}
		previous := livePriorLocations(entry.binding, priors, priorTokens, tokens)
		scoped := scopedBindingContext(base, entry.binding, entry.location, previous)
		// Advisory snapshots preserve unavailable/read-redacted keys as absent.
		// The save callback scalar-normalization helper must not recreate them.
		scoped.SiblingData, _ = entry.location.siblings.CopyObject()
		scoped.OriginalSiblingData = nil
		if old, exists := previous[entry.location.identity]; exists {
			scoped.OriginalSiblingData, _ = old.siblings.CopyObject()
		}
		scoped.RootData = root
		scoped.InputSiblingData = nil
		if input, ok := inputs[path]; ok {
			scoped.InputSiblingData, _ = input.location.siblings.CopyObject()
		}
		evaluation := LiveValidationEvaluation{Path: path, Target: tokens[path], Status: "checked", Issues: []schema.Issue{}}
		if !liveStructuredValueAvailable(entry.binding.Field, entry.location.value) {
			evaluation.Status = "skipped"
			result.Evaluations = append(result.Evaluations, evaluation)
			continue
		}
		for _, validator := range entry.binding.LiveValidators {
			if err := ctx.Err(); err != nil {
				return LiveValidationResult{}, liveOperational(err)
			}
			issues, available, validateError := validator(scoped)
			if validateError != nil {
				return LiveValidationResult{}, liveOperational(validateError)
			}
			if !available {
				evaluation.Status = "skipped"
				evaluation.Issues = []schema.Issue{}
				break
			}
			if len(issues) > MaxValidationIssues {
				return LiveValidationResult{}, liveOperational(fmt.Errorf("live validation returned too many issues"))
			}
			for _, issue := range issues {
				if livePathDenied(scope.denied, issue.Path) {
					continue
				}
				issue.Target = tokens[issue.Path]
				if issue.Target == "" {
					return LiveValidationResult{}, liveOperational(fmt.Errorf("live validation issue targets an unavailable field"))
				}
				issueCount++
				issueBytes += len(issue.Code) + len(issue.Message) + len(issue.Path) + len(issue.Target)
				evaluation.Issues = append(evaluation.Issues, issue)
				if issueCount > MaxValidationIssues || issueBytes > 1<<20 {
					return LiveValidationResult{}, liveOperational(fmt.Errorf("live validation returned too many issues"))
				}
			}
		}
		result.Evaluations = append(result.Evaluations, evaluation)
	}
	if err := ctx.Err(); err != nil {
		return LiveValidationResult{}, liveOperational(err)
	}
	return result, nil
}

func liveAuthorizeResource(collection Collection, base Context, kind operation.Kind) error {
	base.Operation = kind
	if kind == operation.Admin {
		base.Data = store.Values{}
	}
	decision, err := authorize(collection, base)
	if err != nil {
		return capabilityAccessError("resource access rule failed", err)
	}
	if decision.Kind != Allow {
		return liveDenied()
	}
	return nil
}
func liveBadRequest(message string) error {
	return &Error{Code: "bad_request", Status: 400, Message: message}
}
func liveDenied() error {
	return &Error{Code: "access_denied", Status: 403, Message: "live validation is not permitted"}
}
func liveOperational(cause error) error {
	return &Error{Code: "live_validation_failed", Status: 500, Message: "live validation could not be completed", Cause: cause}
}
func liveValidPath(path string) bool {
	if path == "" || len(path) > 4096 {
		return false
	}
	segments := strings.Split(path, ".")
	if len(segments) > 64 {
		return false
	}
	for _, part := range segments {
		if !schema.IsValidFieldName(part) {
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || strconv.Itoa(i) != part {
				return false
			}
		}
	}
	return true
}
