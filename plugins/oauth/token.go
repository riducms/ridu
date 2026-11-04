package oauth

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/store"
)

// token answers POST /api/plugins/oauth/token (RFC 6749 section 3.2): a
// form-encoded authorization code or refresh token for a new access token
// and a new refresh token that replaces the old one.
func (plugin *Plugin) token(endpoint ridu.EndpointContext) {
	ctx := endpoint.Request.Context()
	if endpoint.AdmitAuthAttempt != nil {
		if err := endpoint.AdmitAuthAttempt(ctx, "oauth-token", ""); err != nil {
			oauthError(endpoint, http.StatusTooManyRequests, "slow_down", "Too many token requests; try again later.")
			return
		}
	}
	if err := endpoint.Request.ParseForm(); err != nil {
		oauthError(endpoint, http.StatusBadRequest, "invalid_request", "Send the token request as application/x-www-form-urlencoded.")
		return
	}
	form := endpoint.Request.PostForm
	for _, resource := range form["resource"] {
		if !sameResource(resource, plugin.resource(endpoint.Request)) {
			oauthError(endpoint, http.StatusBadRequest, "invalid_target", "Tokens from this server are for "+plugin.resource(endpoint.Request)+" only.")
			return
		}
	}
	switch form.Get("grant_type") {
	case "authorization_code":
		plugin.exchangeCode(endpoint, form)
	case "refresh_token":
		plugin.refresh(endpoint, form)
	default:
		oauthError(endpoint, http.StatusBadRequest, "unsupported_grant_type", "Use grant_type authorization_code or refresh_token.")
	}
}

// exchangeCode turns an approved authorization code into tokens, once.
func (plugin *Plugin) exchangeCode(endpoint ridu.EndpointContext, form url.Values) {
	ctx := endpoint.Request.Context()
	code := form.Get("code")
	connection, found, err := findConnection(ctx, endpoint.Local, "codeHash", digest(code))
	switch {
	case err != nil:
		plugin.tokenFailure(endpoint, err)
		return
	case code == "" || !found:
		oauthError(endpoint, http.StatusBadRequest, "invalid_grant", "The authorization code is unknown or was already used.")
		return
	case !plugin.before(connection.Values, "codeExpiresAt"):
		_, _ = endpoint.Local.Delete(ctx, ConnectionsCollection, connection.ID, ridu.MutationOptions{System: true})
		oauthError(endpoint, http.StatusBadRequest, "invalid_grant", "The authorization code expired; connect again.")
		return
	case form.Get("client_id") != reference(connection.Values, "client"):
		oauthError(endpoint, http.StatusBadRequest, "invalid_grant", "The authorization code was issued to another client.")
		return
	case form.Get("redirect_uri") != text(connection.Values, "redirectUri"):
		oauthError(endpoint, http.StatusBadRequest, "invalid_grant", "redirect_uri must match the one in the authorization request.")
		return
	case !verifierMatches(form.Get("code_verifier"), text(connection.Values, "codeChallenge")):
		oauthError(endpoint, http.StatusBadRequest, "invalid_grant", "code_verifier doesn't match the code_challenge.")
		return
	}
	plugin.issueTokens(endpoint, connection, store.Values{"codeHash": store.Null(), "codeChallenge": store.Null(), "codeExpiresAt": store.Null()})
}

// refresh replaces a connection's tokens. The old refresh token stops
// working, as OAuth 2.1 asks of public clients.
func (plugin *Plugin) refresh(endpoint ridu.EndpointContext, form url.Values) {
	token := form.Get("refresh_token")
	connection, found, err := findConnection(endpoint.Request.Context(), endpoint.Local, "refreshTokenHash", digest(token))
	switch {
	case err != nil:
		plugin.tokenFailure(endpoint, err)
		return
	case token == "" || !found || !plugin.before(connection.Values, "refreshExpiresAt"):
		oauthError(endpoint, http.StatusBadRequest, "invalid_grant", "The refresh token is unknown, expired or was replaced; connect again.")
		return
	case form.Has("client_id") && form.Get("client_id") != reference(connection.Values, "client"):
		oauthError(endpoint, http.StatusBadRequest, "invalid_grant", "The refresh token was issued to another client.")
		return
	}
	plugin.issueTokens(endpoint, connection, store.Values{})
}

// issueTokens claims the connection's next grant, stores new tokens with
// changes, and sends them. Claiming first makes a code or refresh token
// single-use: of two concurrent requests with the same one, the second
// claims a later generation than the connection it read and is refused.
func (plugin *Plugin) issueTokens(endpoint ridu.EndpointContext, connection store.Document, changes store.Values) {
	ctx := endpoint.Request.Context()
	system := ridu.MutationOptions{System: true}
	read, _ := connection.Values["generation"].NumberValue()
	claimed, err := endpoint.Local.Increment(ctx, ConnectionsCollection, connection.ID, "generation", 1, system)
	if err != nil {
		var operation *ridu.OperationError
		if errors.As(err, &operation) && (operation.Code == "not_found" || operation.Code == "conflict") {
			oauthError(endpoint, http.StatusBadRequest, "invalid_grant", "The grant was already used or revoked; connect again.")
			return
		}
		plugin.tokenFailure(endpoint, err)
		return
	}
	if generation, _ := claimed.Values["generation"].NumberValue(); generation != read+1 {
		oauthError(endpoint, http.StatusBadRequest, "invalid_grant", "The grant was already used; connect again.")
		return
	}
	access, err := newSecret(accessPrefix)
	if err != nil {
		plugin.tokenFailure(endpoint, err)
		return
	}
	refresh, err := newSecret(refreshPrefix)
	if err != nil {
		plugin.tokenFailure(endpoint, err)
		return
	}
	changes["accessTokenHash"] = store.String(digest(access))
	changes["accessExpiresAt"] = plugin.stamp(plugin.config.AccessTokenLifetime)
	changes["refreshTokenHash"] = store.String(digest(refresh))
	changes["refreshExpiresAt"] = plugin.stamp(plugin.config.RefreshTokenLifetime)
	changes["lastUsedAt"] = plugin.stamp(0)
	if _, err := endpoint.Local.Update(ctx, ConnectionsCollection, connection.ID, changes, system); err != nil {
		plugin.tokenFailure(endpoint, err)
		return
	}
	endpoint.Writer.Header().Set("Pragma", "no-cache")
	endpoint.JSON(http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(plugin.config.AccessTokenLifetime.Seconds()),
		"refresh_token": refresh,
	})
}

// revoke answers POST /api/plugins/oauth/revoke (RFC 7009). Revoking either
// of a connection's tokens ends the connection. The answer is the same
// whether or not the token was known.
func (plugin *Plugin) revoke(endpoint ridu.EndpointContext) {
	ctx := endpoint.Request.Context()
	if err := endpoint.Request.ParseForm(); err != nil {
		oauthError(endpoint, http.StatusBadRequest, "invalid_request", "Send the revocation request as application/x-www-form-urlencoded.")
		return
	}
	token := endpoint.Request.PostForm.Get("token")
	for _, field := range []string{"accessTokenHash", "refreshTokenHash"} {
		connection, found, err := findConnection(ctx, endpoint.Local, field, digest(token))
		if err != nil {
			plugin.tokenFailure(endpoint, err)
			return
		}
		if !found || token == "" {
			continue
		}
		if client := endpoint.Request.PostForm.Get("client_id"); client != "" && client != reference(connection.Values, "client") {
			break
		}
		if _, err := endpoint.Local.Delete(ctx, ConnectionsCollection, connection.ID, ridu.MutationOptions{System: true}); err != nil {
			plugin.tokenFailure(endpoint, err)
			return
		}
		break
	}
	endpoint.Writer.WriteHeader(http.StatusOK)
}

func (plugin *Plugin) tokenFailure(endpoint ridu.EndpointContext, err error) {
	endpoint.ReportError(err, "oauth_token_failed")
	oauthError(endpoint, http.StatusInternalServerError, "server_error", "The token couldn't be issued; try again.")
}
