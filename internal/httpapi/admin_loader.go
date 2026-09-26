package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/protocol"
)

type AdminLoaderRequest struct {
	Key         string
	Pathname    string
	RouteParams map[string]string
	Query       url.Values
	Identity    *AuthIdentity
	Locale      string
	AuditRead   func(string, string)
}

// Browser fallback and explicit refresh invoke the same compiled function as
// route preparation. Metadata selects speculative reads, not authorization.
func (api *API) adminLoader(writer http.ResponseWriter, request *http.Request, requestID string) {
	setAdminPreparedHeaders(writer.Header())
	if request.Method == http.MethodHead {
		writer.WriteHeader(http.StatusOK)
		return
	}
	if request.Method != http.MethodGet {
		api.methodNotAllowed(writer, requestID, http.MethodGet, http.MethodHead)
		return
	}
	key := strings.TrimPrefix(request.URL.Path, "/api/admin/loaders/")
	route, err := url.Parse(request.URL.Query().Get("route"))
	if err != nil || route == nil || !strings.HasPrefix(route.Path, "/") || strings.HasPrefix(route.Path, "//") || route.Host != "" || route.IsAbs() || route.Fragment != "" || strings.Contains(route.Path, `\`) {
		api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: http.StatusBadRequest, Message: "Expected a router-root pathname and search in route."})
		return
	}
	// Resolve locale and typed input from the source route, not the endpoint's envelope query.
	request = request.Clone(request.Context())
	route.Path = "/admin" + route.Path
	if route.RawPath != "" {
		route.RawPath = "/admin" + route.RawPath
	}
	request.URL = route
	_, search := normalizeAdminRouteIdentity(request)
	request.URL.RawQuery = strings.TrimPrefix(search, "?")
	runtime, identity, _, err := api.prepareAdminRuntime(request.Context(), request, "")
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	data, err := api.readAdminLoader(request, runtime, identity, key)
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	writeJSON(writer, http.StatusOK, data)
}

func (api *API) readAdminLoader(request *http.Request, runtime *protocol.AdminPreparedRuntimeV1, identity adminPreparedIdentity, key string) (json.RawMessage, error) {
	if runtime.Manifest.Application.Admin != nil && (runtime.Session == nil || !runtime.CollectionOperations[runtime.Session.Collection].Admin) {
		return nil, &operationengine.Error{Code: "access_denied", Status: http.StatusForbidden, Message: "An admin session is required."}
	}
	// The request-scoped manifest is the loader allowlist. Runtime definitions
	// that were hidden from this identity must not remain callable by key.
	for _, loader := range runtime.Manifest.Application.AdminLoaders {
		if loader.Key == key && api.config.AdminLoad != nil {
			pathname, _ := normalizeAdminRouteIdentity(request)
			pathname = strings.TrimPrefix(pathname, "/admin")
			if pathname == "" {
				pathname = "/"
			}
			return api.config.AdminLoad(request.Context(), AdminLoaderRequest{Key: key, Pathname: pathname, RouteParams: adminLoaderRouteParams(pathname), Query: request.URL.Query(), Identity: identity.identity, Locale: adminRouteLocale(runtime, request.URL.Query()), AuditRead: func(collection, id string) {
				requestID, _ := request.Context().Value(adminPreparedRequestIDContextKey{}).(string)
				api.audit(request, requestID, identityActor(identity.identity), "read", collection, id, identityCollection(identity.identity))
			}})
		}
	}
	return nil, &operationengine.Error{Code: "not_found", Status: http.StatusNotFound, Message: "Admin loader not found."}
}

func adminLoaderRouteParams(pathname string) map[string]string {
	segments := strings.Split(strings.Trim(pathname, "/"), "/")
	for index, segment := range segments {
		segments[index], _ = url.PathUnescape(segment)
	}
	route := classifyAuthenticatedAdminRoute(segments)
	params := map[string]string{}
	switch route.surface {
	case "collectionList", "collectionCreate", "collectionEdit":
		params["collection"] = segments[1]
		if route.surface == "collectionEdit" {
			params["document"] = segments[2]
		}
	case "global":
		params["global"] = segments[1]
	}
	return params
}
