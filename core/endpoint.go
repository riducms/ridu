package core

import (
	"context"
	"net/http"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Endpoint is one application-authored, method-specific HTTP endpoint. Root
// endpoints are mounted below /api; collection and global endpoints are
// mounted below their resource route. Path uses Payload-familiar named
// segments such as /:id/tracking.
//
// Custom endpoints are not authorized automatically. Handler must enforce any
// endpoint-specific policy before performing work. Local remains
// access-controlled unless the application explicitly chooses an override on
// an individual local operation.
type Endpoint struct {
	// Method is one supported HTTP method. Matching is case-insensitive during
	// config resolution and the manifest stores its uppercase form.
	Method string
	// Path begins with / and may contain named :parameter segments.
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

// EndpointContext exposes one matched custom endpoint request.
type EndpointContext struct {
	Writer  http.ResponseWriter
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
}

// EndpointHandler handles one trusted compiled custom endpoint.
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
