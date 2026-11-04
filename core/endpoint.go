package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/riducms/ridu/internal/httpapi"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Endpoint is one compiled, method-specific HTTP endpoint. Registration owns placement. Root
// endpoints are mounted below /api; collection and global endpoints are
// mounted below their resource route. Path uses Payload-familiar named
// segments such as /:id/tracking. Plugin providers use exact relative paths below
// /api/plugins/<key>/; transport providers use their restricted absolute protocol paths.
//
// Custom endpoints are not authorized automatically. Handler must enforce any
// endpoint-specific policy before performing work. Local remains
// access-controlled unless the application explicitly chooses an override on
// an individual local operation.
type Endpoint struct {
	// Method is one supported HTTP method. Matching is case-insensitive during
	// config resolution and the manifest stores its uppercase form.
	Method string
	// Path follows the registering provider: application/resource paths begin with /
	// and support :parameters, plugin paths are exact and relative, and transport
	// paths are exact allowed absolute protocol routes.
	Path string
	// Summary appears in the generated OpenAPI operation. When empty, Ridu
	// supplies a deterministic generic summary.
	Summary string
	// MaxBodyBytes bounds the raw request body before Handler receives it. Zero
	// inherits HandlerOptions.MaxBodyBytes; a negative value opts trusted
	// streaming code out of that bound.
	MaxBodyBytes int64
	// Handler is trusted compiled application code and is never serialized.
	Handler EndpointHandler
}

// EndpointContext gives an EndpointHandler the matched request, response writer,
// route metadata, authenticated actor, and access-controlled Local API.
type EndpointContext struct {
	// Writer receives the status, headers, and body chosen by the handler.
	Writer http.ResponseWriter
	// Request is the matched HTTP request. Use its Context for cancellation,
	// deadlines, and dependent work.
	Request *http.Request
	// RequestID is the framework request ID also returned in X-Request-ID.
	RequestID string
	// ClientIP is resolved through the configured trusted-proxy policy.
	ClientIP string
	// RouteParams contains decoded named path parameters.
	RouteParams map[string]string
	// Collection identifies the owning collection endpoint, when applicable.
	Collection schema.CollectionSlug
	// Global identifies the owning global endpoint, when applicable.
	Global schema.CollectionSlug
	// Actor is the authenticated user, or nil for an anonymous request.
	Actor *store.Document
	// ActorCollection identifies the auth collection that owns Actor.
	ActorCollection schema.CollectionSlug
	// Local enters the same access-controlled operation engine as REST and the
	// generated SDK.
	Local *LocalAPI
	// AdmitAuthAttempt applies the distributed authentication-attempt policy for
	// endpoints implementing an authentication flow.
	AdmitAuthAttempt func(context.Context, string, string) error
	// ReportError records a stable code and sends trusted detail to
	// HandlerOptions.RequestError without exposing it to the client.
	ReportError func(error, string)

	writeError func(error)
}

// JSON answers with status and value encoded as JSON, with an
// application/json Content-Type. A value that can't be encoded is answered as
// Error answers an internal failure.
func (ctx EndpointContext) JSON(status int, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		ctx.Error(fmt.Errorf("encode endpoint response: %w", err))
		return
	}
	ctx.Writer.Header().Set("Content-Type", "application/json")
	ctx.Writer.WriteHeader(status)
	_, _ = ctx.Writer.Write(append(encoded, '\n'))
}

// Error answers with Ridu's error envelope, exactly as REST does, so the SDK
// reads it as a RiduError. An *OperationError, such as one returned by Local
// or built by the handler, keeps its code, status, message and validation
// issues. Any other error, and any 5xx, is answered as a generic internal
// error, and the error itself goes to HandlerOptions.RequestError.
func (ctx EndpointContext) Error(err error) {
	ctx.Writer.Header().Set("Content-Type", "application/json")
	if ctx.writeError != nil {
		ctx.writeError(err)
		return
	}
	// A context built outside Ridu's handler, as in a test, has no request
	// observation to record; it still reports internal failures.
	envelope := httpapi.ErrorEnvelope(ctx.RequestID, err)
	if envelope.Error.Status >= http.StatusInternalServerError && ctx.ReportError != nil {
		ctx.ReportError(err, string(envelope.Error.Code))
	}
	ctx.Writer.WriteHeader(envelope.Error.Status)
	_ = json.NewEncoder(ctx.Writer).Encode(envelope)
}

// EndpointHandler serves one trusted compiled custom route. It writes the
// endpoint-owned response through EndpointContext.Writer and must enforce any
// endpoint-specific authorization before performing protected work.
type EndpointHandler func(EndpointContext)

type endpointScope uint8

const (
	endpointScopeRoot endpointScope = iota
	endpointScopeCollection
	endpointScopeGlobal
)

type runtimeEndpoint struct {
	scope      endpointScope
	resource   schema.CollectionSlug
	definition Endpoint
}

func runtimeEndpoints(config Config) []runtimeEndpoint {
	result := make([]runtimeEndpoint, 0, len(config.Endpoints))
	for _, endpoint := range config.Endpoints {
		result = append(result, runtimeEndpoint{scope: endpointScopeRoot, definition: endpoint})
	}
	for _, collection := range config.Collections {
		for _, endpoint := range collection.Endpoints {
			result = append(result, runtimeEndpoint{scope: endpointScopeCollection, resource: collection.Slug, definition: endpoint})
		}
	}
	for _, global := range config.Globals {
		for _, endpoint := range global.Endpoints {
			result = append(result, runtimeEndpoint{scope: endpointScopeGlobal, resource: global.Slug, definition: endpoint})
		}
	}
	return result
}
