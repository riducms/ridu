package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/store"
)

func adminReadResult[Value any](api *API, request *http.Request, value Value, err error) protocol.AdminReadResultV1[Value] {
	if err != nil {
		return protocol.AdminReadResultV1[Value]{Error: api.adminReadError(request, err)}
	}
	return protocol.AdminReadResultV1[Value]{Value: &value}
}

func (api *API) prepareAdminRouteData(request *http.Request, runtime *protocol.AdminPreparedRuntimeV1, identity adminPreparedIdentity, route adminRouteClassification) *protocol.AdminPreparedRouteDataV1 {
	data := &protocol.AdminPreparedRouteDataV1{Kind: route.kind}
	locale := adminRouteLocale(runtime, request.URL.Query())
	segments := route.segments
	switch route.kind {
	case protocol.AdminPreparedRouteCollectionCreate:
		collection := adminCollection(runtime.Manifest, segments[1])
		values, deterministic := adminInitialFormValues(collection.Fields)
		if !deterministic {
			return nil
		}
		// Round-trip defaults through JSON so the capability check sees the same
		// store.Value representation that a browser create request would submit.
		encoded, err := json.Marshal(values)
		var input store.Values
		if err == nil {
			err = json.Unmarshal(encoded, &input)
		}
		if err != nil {
			return nil
		}
		access, err := api.readDocumentAccess(request.Context(), segments[1], "", input, false, identity.identity, localeQuery{locale: locale})
		data.Create = &protocol.AdminCreateDataV1{Values: values, Access: adminReadResult(api, request, access, err)}
	case protocol.AdminPreparedRouteCollectionDocument, protocol.AdminPreparedRouteCollectionAPI, protocol.AdminPreparedRouteGlobalDocument, protocol.AdminPreparedRouteGlobalAPI, protocol.AdminPreparedRouteAccount:
		key, id := "", ""
		if route.kind == protocol.AdminPreparedRouteAccount {
			if runtime.Session == nil || identity.resolvedSession == nil {
				return nil
			}
			key, id = runtime.Session.Collection, identity.resolvedSession.User.ID
		} else if strings.HasPrefix(string(route.kind), "global-") {
			key, id = "global:"+segments[1], segments[1]
		} else {
			key, id = segments[1], segments[2]
		}
		apiView := route.kind == protocol.AdminPreparedRouteCollectionAPI || route.kind == protocol.AdminPreparedRouteGlobalAPI
		if route.kind == protocol.AdminPreparedRouteAccount {
			data.Document = &protocol.AdminDocumentDataV1{
				Document: api.prepareAdminDocumentRead(request, identity.identity, key, id, locale, false),
				Access:   api.prepareAdminAccess(request, identity.identity, key, id, locale),
			}
		} else {
			data.Document = &protocol.AdminDocumentDataV1{
				Access:   api.prepareAdminAccess(request, identity.identity, key, id, locale),
				Document: api.prepareAdminDocumentRead(request, identity.identity, key, id, locale, apiView),
			}
		}
	case protocol.AdminPreparedRouteUpload:
		access, err := api.readDocumentAccess(request.Context(), segments[1], "", nil, false, identity.identity, localeQuery{locale: locale})
		result := adminReadResult(api, request, access, err)
		data.Access = &result
	case protocol.AdminPreparedRouteCollectionVersions, protocol.AdminPreparedRouteGlobalVersions:
		key, id, revision := segments[1], "", ""
		global := route.kind == protocol.AdminPreparedRouteGlobalVersions
		if global {
			key, id = "global:"+segments[1], segments[1]
			if len(segments) == 4 {
				revision = segments[3]
			}
		} else {
			id = segments[2]
			if len(segments) == 5 {
				revision = segments[4]
			}
		}
		historyOptions := operationengine.LocalizationOptions{
			Locale:          locale,
			ActorCollection: identityCollection(identity.identity),
		}
		detailOptions := historyOptions
		// Comparison renders exact values for each selected locale, without fallback substitution.
		if runtime.Manifest.Application.Localization != nil {
			detailOptions.Locale = "all"
		}
		history, err := api.config.Engine.Versions(request.Context(), key, id, identityActor(identity.identity), historyOptions)
		versions := &protocol.AdminVersionsDataV1{History: adminReadResult(api, request, versionsJSON(history), err)}
		versions.Document = protocol.AdminDocumentDataV1{
			Document: api.prepareAdminDocumentRead(request, identity.identity, key, id, locale, false),
			Access:   api.prepareAdminAccess(request, identity.identity, key, id, locale),
		}
		if number, valid := adminIntegerQuery(revision); valid && number > 0 {
			version, err := api.config.Engine.Version(request.Context(), key, id, number, identityActor(identity.identity), detailOptions)
			detail := adminReadResult(api, request, versionJSON(version), err)
			versions.Detail = &detail
		}
		data.Versions = versions
	case protocol.AdminPreparedRouteSecurity:
		data.Security = api.prepareAdminSecurity(request, runtime, identity)
	}
	return data
}

func (api *API) prepareAdminAccess(request *http.Request, identity *AuthIdentity, key, id, locale string) protocol.AdminReadResultV1[protocol.AccessCapabilitiesEnvelope] {
	access, err := api.readDocumentAccess(request.Context(), key, id, nil, false, identity, localeQuery{locale: locale})
	return adminReadResult(api, request, access, err)
}

func (api *API) prepareAdminDocumentRead(request *http.Request, identity *AuthIdentity, key, id, locale string, apiView bool) protocol.AdminReadResultV1[json.RawMessage] {
	query := url.Values{}
	if locale != "" {
		query.Set("locale", locale)
	}
	if apiView {
		query.Set("depth", "2")
	}
	collection := api.collections[key]
	if slug, global := strings.CutPrefix(key, "global:"); global {
		collection = api.globals[slug]
	}
	options, err := decodeListQuery(query, collection, false)
	var document json.RawMessage
	if err == nil {
		document, err = api.readDocument(request.Context(), key, id, identity, options)
	}
	if err == nil && !strings.HasPrefix(key, "global:") {
		requestID, _ := request.Context().Value(adminPreparedRequestIDContextKey{}).(string)
		api.audit(request, requestID, identityActor(identity), "read", key, id, identityCollection(identity))
	}
	return adminReadResult(api, request, document, err)
}

func (api *API) prepareAdminSecurity(request *http.Request, runtime *protocol.AdminPreparedRuntimeV1, identity adminPreparedIdentity) *protocol.AdminSecurityDataV1 {
	sessions := []protocol.AuthSessionInfo{}
	var err error
	if identity.resolvedSession == nil || identity.resolvedSession.PreparedSessions == nil {
		err = errors.New("prepared session read is unavailable")
	} else {
		var resolved []AuthSessionInfo
		resolved, err = identity.resolvedSession.PreparedSessions(request.Context())
		for _, session := range resolved {
			sessions = append(sessions, protocol.AuthSessionInfo{
				ID: session.ID, CreatedAt: session.CreatedAt.Format(time.RFC3339Nano),
				LastSeenAt: session.LastSeenAt.Format(time.RFC3339Nano), ExpiresAt: session.ExpiresAt.Format(time.RFC3339Nano),
				IPAddress: session.IPAddress, UserAgent: session.UserAgent, Current: session.Current,
			})
		}
	}
	data := &protocol.AdminSecurityDataV1{Sessions: adminReadResult(api, request, sessions, err)}
	keys := []protocol.APIKeyInfo{}
	err = nil
	if auth := adminAuthCollection(runtime.Manifest); auth != nil && auth.Auth != nil && auth.Auth.APIKeys {
		if identity.resolvedSession == nil || identity.resolvedSession.PreparedAPIKeys == nil {
			err = errors.New("prepared API-key read is unavailable")
		} else {
			var resolved []APIKeyInfo
			resolved, err = identity.resolvedSession.PreparedAPIKeys(request.Context())
			for _, key := range resolved {
				keys = append(keys, apiKeyInfoEnvelope(key))
			}
		}
	}
	data.APIKeys = adminReadResult(api, request, keys, err)
	return data
}

func adminRouteDataFailed(data *protocol.AdminPreparedRouteDataV1) bool {
	if data == nil {
		return true
	}
	errors := []*protocol.ErrorPayload{}
	if data.Document != nil {
		errors = append(errors, data.Document.Document.Error, data.Document.Access.Error)
	}
	if data.Create != nil {
		errors = append(errors, data.Create.Access.Error)
	}
	if data.Access != nil {
		errors = append(errors, data.Access.Error)
	}
	if data.Versions != nil {
		versions := data.Versions
		errors = append(errors, versions.History.Error, versions.Document.Document.Error, versions.Document.Access.Error)
		if versions.Detail != nil {
			errors = append(errors, versions.Detail.Error)
		}
	}
	if data.Security != nil {
		errors = append(errors, data.Security.Sessions.Error, data.Security.APIKeys.Error)
	}
	// Expected access, validation and not-found failures stay independently
	// renderable. Only an internal failure abandons the whole prepared route.
	for _, err := range errors {
		if err != nil && err.Code == protocol.ErrorInternal {
			return true
		}
	}
	return false
}
