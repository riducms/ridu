package httpapi

import (
	"context"
	"encoding/json"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/store"
)

// readDocument is shared by ordinary REST reads and prepared admin documents.
// Each invocation retains its own operation-engine lifecycle.
func (api *API) readDocument(ctx context.Context, collection, id string, identity *AuthIdentity, options listQuery) (json.RawMessage, error) {
	result, err := api.config.Engine.Execute(ctx, operationengine.Request{
		Operation: operation.Read, Collection: collection, ID: id,
		Actor: identityActor(identity), ActorCollection: identityCollection(identity),
		Draft:  options.draft,
		Select: options.selectFields, OutputFields: options.outputFields, Populate: options.populate,
		Locale: options.locale, FallbackLocales: options.fallbackLocales,
		DisableFallback: options.disableFallback, AllLocales: options.allLocales,
	})
	if err != nil {
		return nil, err
	}
	return documentJSON(*result.Document), nil
}

func (api *API) readDocumentAccess(ctx context.Context, collection, id string, data store.Values, trash bool, identity *AuthIdentity, options localeQuery) (protocol.AccessCapabilitiesEnvelope, error) {
	capabilities, err := api.config.Engine.Capabilities(ctx, operationengine.CapabilitiesRequest{
		Collection: collection, ID: id, Data: data, TrashOnly: trash,
		Actor: identityActor(identity), ActorCollection: identityCollection(identity),
		Locale: options.locale, FallbackLocales: options.fallbackLocales,
		DisableFallback: options.disableFallback, AllLocales: options.allLocales,
	})
	return accessCapabilitiesJSON(capabilities), err
}

func (api *API) readScheduledPublications(ctx context.Context, collection, id string, identity *AuthIdentity) ([]protocol.ScheduledPublication, error) {
	if api.config.SchedulePublish == nil || api.config.ScheduledPublications == nil || api.config.CancelScheduledPublication == nil {
		return nil, &operationengine.Error{Code: "not_found", Status: 404, Message: "scheduled publishing is unavailable"}
	}
	jobs, err := api.config.ScheduledPublications(ctx, collection, id, identity)
	if err != nil {
		return nil, err
	}
	result := make([]protocol.ScheduledPublication, len(jobs))
	for index, job := range jobs {
		result[index] = scheduledPublicationJSON(job)
	}
	return result, nil
}
