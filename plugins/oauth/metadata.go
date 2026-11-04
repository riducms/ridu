package oauth

import (
	"net/http"

	"github.com/riducms/ridu"
)

// authorizationServer answers GET /.well-known/oauth-authorization-server
// with the issuer's metadata (RFC 8414).
func (plugin *Plugin) authorizationServer(endpoint ridu.EndpointContext) {
	origin := plugin.origin(endpoint.Request)
	publicMetadata(endpoint)
	endpoint.JSON(http.StatusOK, map[string]any{
		"issuer":                                     origin,
		"authorization_endpoint":                     origin + "/admin/" + consentRoute,
		"token_endpoint":                             origin + "/api/plugins/oauth/token",
		"registration_endpoint":                      origin + "/api/plugins/oauth/register",
		"revocation_endpoint":                        origin + "/api/plugins/oauth/revoke",
		"response_types_supported":                   []string{"code"},
		"response_modes_supported":                   []string{"query"},
		"grant_types_supported":                      []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported":      []string{"none"},
		"revocation_endpoint_auth_methods_supported": []string{"none"},
		"code_challenge_methods_supported":           []string{"S256"},
	})
}

// protectedResource answers GET /.well-known/oauth-protected-resource with
// the metadata MCP clients read, from the protected resource's challenge, to
// find this server (RFC 9728).
func (plugin *Plugin) protectedResource(endpoint ridu.EndpointContext) {
	publicMetadata(endpoint)
	endpoint.JSON(http.StatusOK, map[string]any{
		"resource":                 plugin.resource(endpoint.Request),
		"authorization_servers":    []string{plugin.origin(endpoint.Request)},
		"bearer_methods_supported": []string{"header"},
	})
}

// publicMetadata lets a browser-based client read metadata from another
// origin; it is public and carries no credentials.
func publicMetadata(endpoint ridu.EndpointContext) {
	endpoint.Writer.Header().Set("Access-Control-Allow-Origin", "*")
}
