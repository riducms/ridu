package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

// registration is the part of an RFC 7591 registration request the plugin
// uses. Other metadata, such as a logo, is accepted and ignored.
type registration struct {
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
}

// unusedClientLifetime is how long a registered client that never connected
// is kept. Clients such as Claude register afresh for each connection.
const unusedClientLifetime = 24 * time.Hour

// register answers POST /api/plugins/oauth/register. Anyone may register a
// public client: registering grants nothing, and a person still approves
// every connection.
func (plugin *Plugin) register(endpoint ridu.EndpointContext) {
	ctx := endpoint.Request.Context()
	if endpoint.AdmitAuthAttempt != nil {
		if err := endpoint.AdmitAuthAttempt(ctx, "oauth-register", ""); err != nil {
			oauthError(endpoint, http.StatusTooManyRequests, "slow_down", "Too many registrations; try again later.")
			return
		}
	}
	var body registration
	if err := json.NewDecoder(endpoint.Request.Body).Decode(&body); err != nil {
		oauthError(endpoint, http.StatusBadRequest, "invalid_client_metadata", "Send the client's metadata as JSON.")
		return
	}
	if len(body.RedirectURIs) == 0 || len(body.RedirectURIs) > 10 {
		oauthError(endpoint, http.StatusBadRequest, "invalid_redirect_uri", "Register between 1 and 10 redirect_uris.")
		return
	}
	for _, uri := range body.RedirectURIs {
		if !validRedirectURI(uri) {
			oauthError(endpoint, http.StatusBadRequest, "invalid_redirect_uri", "Each redirect URI must be an https URL, or an http URL on a loopback address, without a fragment: "+uri)
			return
		}
	}
	if method := body.TokenEndpointAuthMethod; method != "" && method != "none" {
		oauthError(endpoint, http.StatusBadRequest, "invalid_client_metadata", "Only public clients can register: token_endpoint_auth_method must be none, with PKCE.")
		return
	}
	for _, grant := range body.GrantTypes {
		if grant != "authorization_code" && grant != "refresh_token" {
			oauthError(endpoint, http.StatusBadRequest, "invalid_client_metadata", "The supported grant_types are authorization_code and refresh_token.")
			return
		}
	}
	if len(body.ResponseTypes) > 0 && !slices.Equal(body.ResponseTypes, []string{"code"}) {
		oauthError(endpoint, http.StatusBadRequest, "invalid_client_metadata", "The only supported response_type is code.")
		return
	}
	name := strings.TrimSpace(body.ClientName)
	if name == "" {
		name = "MCP client"
	}
	if utf8.RuneCountInString(name) > 100 {
		oauthError(endpoint, http.StatusBadRequest, "invalid_client_metadata", "client_name has at most 100 characters.")
		return
	}
	plugin.pruneClients(ctx, endpoint.Local)
	uris := make([]store.Value, len(body.RedirectURIs))
	for index, uri := range body.RedirectURIs {
		uris[index] = store.String(uri)
	}
	client, err := endpoint.Local.Create(ctx, ClientsCollection, store.Values{
		"name": store.String(name), "redirectUris": store.List(uris...),
	}, ridu.MutationOptions{System: true})
	if err != nil {
		endpoint.ReportError(err, "oauth_register_failed")
		oauthError(endpoint, http.StatusInternalServerError, "server_error", "The client couldn't be registered.")
		return
	}
	endpoint.JSON(http.StatusCreated, map[string]any{
		"client_id":                  client.ID,
		"client_id_issued_at":        client.CreatedAt.Unix(),
		"client_name":                name,
		"redirect_uris":              body.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

// pruneClients deletes up to a page of clients registered over a day ago that
// never connected, so registrations that went nowhere don't accumulate.
// Failures only leave them for next time.
func (plugin *Plugin) pruneClients(ctx context.Context, local *ridu.LocalAPI) {
	stale, err := local.List(ctx, ClientsCollection, ridu.ListOptions{
		Where: query.LessThan("createdAt", plugin.now().Add(-unusedClientLifetime).UTC().Format("2006-01-02T15:04:05Z")), Limit: 100, SkipTotal: true, System: true,
	})
	if err != nil {
		return
	}
	for _, client := range stale.Documents {
		used, err := local.List(ctx, ConnectionsCollection, ridu.ListOptions{Where: query.Equal("client", client.ID), Limit: 1, SkipTotal: true, System: true})
		if err == nil && len(used.Documents) == 0 {
			_, _ = local.Delete(ctx, ClientsCollection, client.ID, ridu.MutationOptions{System: true})
		}
	}
}
