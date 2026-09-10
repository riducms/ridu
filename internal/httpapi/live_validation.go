package httpapi

import (
	"net/http"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/protocol"
)

func (api *API) liveValidation(writer http.ResponseWriter, request *http.Request, requestID, collection, id string) {
	if request.Method != http.MethodPost {
		api.methodNotAllowed(writer, requestID, http.MethodPost)
		return
	}
	// Preserve the normal JSON complexity, duplicate-key and unknown-field checks
	// while imposing the smaller advisory request ceiling.
	request.Body = http.MaxBytesReader(writer, request.Body, 1<<20)
	var input protocol.LiveValidationRequest
	if err := api.decodeJSON(writer, request, &input); err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	if id != "" {
		if input.ID != "" {
			api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "global validation does not accept a document ID"})
			return
		}
		input.ID = id
	}
	locale, err := decodeLocaleQuery(request)
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	if locale.allLocales || locale.fallbackLocales != nil || request.URL.Query().Has("fallbackLocale") || request.URL.Query().Has("fallbackLocales") {
		api.writeError(writer, requestID, &operationengine.Error{Code: "bad_locale", Status: 400, Message: "live validation accepts one exact locale without fallback"})
		return
	}
	identity := api.optionalIdentity(request)
	embedded := make([]operationengine.LiveValidationEmbeddedScope, len(input.Embedded))
	for i, scope := range input.Embedded {
		embedded[i] = operationengine.LiveValidationEmbeddedScope{Field: scope.Field, TreeKey: scope.TreeKey, CaseTag: scope.CaseTag, VariantSlug: scope.VariantSlug, Identity: scope.Identity, Data: scope.Data}
	}
	result, err := api.config.Engine.LiveValidate(request.Context(), operationengine.LiveValidationRequest{Collection: collection, ID: input.ID, Data: input.Data, Fields: input.Fields, Embedded: embedded, Actor: identityActor(identity), ActorCollection: identityCollection(identity), Locale: locale.locale})
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	output := protocol.LiveValidationEnvelope{Evaluations: make([]protocol.LiveValidationEvaluation, len(result.Evaluations))}
	for i, evaluation := range result.Evaluations {
		issues := make([]protocol.ValidationIssue, len(evaluation.Issues))
		for j, issue := range evaluation.Issues {
			issues[j] = protocol.ValidationIssue{Code: issue.Code, Path: issue.Path, Message: issue.Message, Target: issue.Target, FieldID: issue.FieldID, CollectionID: issue.CollectionID, GlobalID: issue.GlobalID, Locale: issue.Locale}
		}
		output.Evaluations[i] = protocol.LiveValidationEvaluation{Path: evaluation.Path, Target: evaluation.Target, Status: evaluation.Status, Issues: issues}
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusOK, output)
}
