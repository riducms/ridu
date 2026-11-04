package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

// authorizationRequest is a request a person may approve.
type authorizationRequest struct {
	client      store.Document
	redirectURI string
	state       string
	challenge   string
}

// refusal is a request that can't be approved. Once the client and its
// redirect URI check out, the client hears why through its redirect (RFC 6749
// section 4.1.2.1); before that, only the person does, so a forged request
// can't send their browser anywhere.
type refusal struct {
	code, description string
	redirect          string
}

// checkRequest validates the query string the client sent the person's
// browser to the consent page with.
func (plugin *Plugin) checkRequest(ctx context.Context, local *ridu.LocalAPI, request *http.Request, raw string) (authorizationRequest, *refusal) {
	params, err := url.ParseQuery(strings.TrimPrefix(raw, "?"))
	if err != nil {
		return authorizationRequest{}, &refusal{code: "invalid_request", description: "The link to this page is damaged. Start connecting again from the app."}
	}
	clientID := params.Get("client_id")
	if clientID == "" {
		return authorizationRequest{}, &refusal{code: "invalid_request", description: "The link to this page names no app. Start connecting again from the app."}
	}
	client, err := local.Find(ctx, ClientsCollection, clientID, ridu.FindOptions{System: true})
	if err != nil {
		return authorizationRequest{}, &refusal{code: "invalid_client", description: "This app isn't registered, or its registration expired. Start connecting again from the app."}
	}
	var registered []string
	for value := range client.Values["redirectUris"].Elements() {
		if uri, ok := value.StringValue(); ok {
			registered = append(registered, uri)
		}
	}
	redirect := params.Get("redirect_uri")
	if redirect == "" && len(registered) == 1 {
		redirect = registered[0]
	}
	matched := false
	for _, uri := range registered {
		matched = matched || redirectMatches(uri, redirect)
	}
	if !matched {
		return authorizationRequest{}, &refusal{code: "invalid_request", description: "The app asked to send you to an address it didn't register, so you won't be sent there. Start connecting again from the app."}
	}
	approval := authorizationRequest{client: client, redirectURI: redirect, state: params.Get("state"), challenge: params.Get("code_challenge")}
	refuse := func(code, description string) (authorizationRequest, *refusal) {
		return authorizationRequest{}, &refusal{code: code, description: description, redirect: approval.errorRedirect(code, description)}
	}
	if params.Get("response_type") != "code" {
		return refuse("unsupported_response_type", "Only response_type=code is supported.")
	}
	if params.Get("code_challenge_method") != "S256" || !validChallenge(approval.challenge) {
		return refuse("invalid_request", "PKCE with code_challenge_method=S256 is required.")
	}
	for _, resource := range params["resource"] {
		if !sameResource(resource, plugin.resource(request)) {
			return refuse("invalid_target", "Tokens from this server are for "+plugin.resource(request)+" only.")
		}
	}
	return approval, nil
}

func (approval authorizationRequest) errorRedirect(code, description string) string {
	return approval.redirect(url.Values{"error": {code}, "error_description": {description}})
}

// redirect is the client's redirect URI with values and the request's state
// added to its query.
func (approval authorizationRequest) redirect(values url.Values) string {
	target, err := url.Parse(approval.redirectURI)
	if err != nil {
		return ""
	}
	query := target.Query()
	for name, items := range values {
		query[name] = items
	}
	if approval.state != "" {
		query.Set("state", approval.state)
	}
	target.RawQuery = query.Encode()
	return target.String()
}

// consentBody is what the consent page sends: the query string it was opened
// with and, for a decision, the person's answer.
type consentBody struct {
	Query string `json:"query"`
	Allow bool   `json:"allow"`
	// ActAs is the ID of the user the client will act as: the approver, or
	// one of the users Config.ActAs offers.
	ActAs string `json:"actAs"`
}

// choice is a user a client may act as.
type choice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// authorization answers POST /api/plugins/oauth/authorization for the consent
// page: which app is asking, where the person will be sent back, and whom the
// app may act as, or why the request can't go on.
func (plugin *Plugin) authorization(endpoint ridu.EndpointContext) {
	body, ok := plugin.readConsent(endpoint)
	if !ok {
		return
	}
	ctx := endpoint.Request.Context()
	approval, refused := plugin.checkRequest(ctx, endpoint.Local, endpoint.Request, body.Query)
	if refused != nil {
		plugin.writeRefusal(endpoint, refused)
		return
	}
	choices, err := plugin.choices(ctx, endpoint)
	if err != nil {
		endpoint.Error(err)
		return
	}
	target, _ := url.Parse(approval.redirectURI)
	endpoint.JSON(http.StatusOK, map[string]any{
		"client":       map[string]string{"id": approval.client.ID, "name": text(approval.client.Values, "name")},
		"redirectHost": target.Hostname(),
		"loopback":     isLoopback(target.Hostname()),
		"actAs":        choices,
	})
}

// decision answers POST /api/plugins/oauth/decision: the person's approval,
// with the user the app acts as, or their refusal. Either way the answer is
// the address to send the browser back to the app.
func (plugin *Plugin) decision(endpoint ridu.EndpointContext) {
	body, ok := plugin.readConsent(endpoint)
	if !ok {
		return
	}
	ctx := endpoint.Request.Context()
	approval, refused := plugin.checkRequest(ctx, endpoint.Local, endpoint.Request, body.Query)
	if refused != nil {
		plugin.writeRefusal(endpoint, refused)
		return
	}
	if !body.Allow {
		endpoint.JSON(http.StatusOK, map[string]string{"redirect": approval.errorRedirect("access_denied", "The person declined to connect.")})
		return
	}
	choices, err := plugin.choices(ctx, endpoint)
	if err != nil {
		endpoint.Error(err)
		return
	}
	actAs := ""
	for _, candidate := range choices {
		if candidate.ID == body.ActAs {
			actAs = candidate.ID
		}
	}
	if actAs == "" {
		endpoint.Error(&ridu.OperationError{Code: "validation", Status: http.StatusUnprocessableEntity, Message: "Choose who the app acts as from the offered users."})
		return
	}
	plugin.pruneConnections(ctx, endpoint.Local)
	code, err := newSecret(codePrefix)
	if err == nil {
		_, err = endpoint.Local.Create(ctx, ConnectionsCollection, store.Values{
			"clientName":    store.String(text(approval.client.Values, "name")),
			"client":        store.String(approval.client.ID),
			"user":          store.String(actAs),
			"approvedBy":    store.String(endpoint.Actor.ID),
			"redirectUri":   store.String(approval.redirectURI),
			"codeHash":      store.String(digest(code)),
			"codeChallenge": store.String(approval.challenge),
			"codeExpiresAt": plugin.stamp(plugin.config.CodeLifetime),
			"generation":    store.Number(0),
		}, ridu.MutationOptions{System: true})
	}
	if err != nil {
		endpoint.Error(err)
		return
	}
	endpoint.JSON(http.StatusOK, map[string]string{"redirect": approval.redirect(url.Values{"code": {code}})})
}

// readConsent reads a consent step from a signed-in member of Users.
func (plugin *Plugin) readConsent(endpoint ridu.EndpointContext) (consentBody, bool) {
	if endpoint.Actor == nil || endpoint.ActorCollection != plugin.config.Users {
		endpoint.Error(&ridu.OperationError{Code: "access_denied", Status: http.StatusUnauthorized, Message: "Sign in to connect an app."})
		return consentBody{}, false
	}
	var body consentBody
	if err := json.NewDecoder(endpoint.Request.Body).Decode(&body); err != nil {
		endpoint.Error(&ridu.OperationError{Code: "bad_request", Status: http.StatusBadRequest, Message: `Send JSON: {"query": "?client_id=…"}.`})
		return consentBody{}, false
	}
	return body, true
}

// writeRefusal tells the page why a request can't go on, and where to send
// the browser when the app should hear it too.
func (plugin *Plugin) writeRefusal(endpoint ridu.EndpointContext, refused *refusal) {
	if refused.redirect == "" {
		endpoint.Error(&ridu.OperationError{Code: "bad_request", Status: http.StatusBadRequest, Message: refused.description})
		return
	}
	endpoint.JSON(http.StatusOK, map[string]string{"error": refused.code, "errorDescription": refused.description, "redirect": refused.redirect})
}

// choices are the users the signed-in approver may let a client act as:
// themselves first, then Config.ActAs's users.
func (plugin *Plugin) choices(ctx context.Context, endpoint ridu.EndpointContext) ([]choice, error) {
	approver := *endpoint.Actor
	choices := []choice{{ID: approver.ID, Label: label(approver)}}
	if plugin.config.ActAs == nil {
		return choices, nil
	}
	others, err := plugin.config.ActAs(ctx, endpoint.Local, store.CloneDocument(approver))
	if err != nil {
		return nil, err
	}
	for _, other := range others {
		if other.ID != "" && other.ID != approver.ID {
			choices = append(choices, choice{ID: other.ID, Label: label(other)})
		}
	}
	return choices, nil
}

// label names a user for the consent page: their name, email or ID.
func label(user store.Document) string {
	for _, field := range []string{"name", "email"} {
		if value := strings.TrimSpace(text(user.Values, field)); value != "" {
			return value
		}
	}
	return user.ID
}

// pruneConnections deletes up to a page of approvals whose code was never
// exchanged and connections whose refresh token expired. Failures only leave
// them for next time.
func (plugin *Plugin) pruneConnections(ctx context.Context, local *ridu.LocalAPI) {
	now := plugin.now().UTC().Format("2006-01-02T15:04:05Z")
	stale, err := local.List(ctx, ConnectionsCollection, ridu.ListOptions{
		Where: query.Or(
			query.And(query.Exists("refreshTokenHash", false), query.LessThan("codeExpiresAt", now)),
			query.LessThan("refreshExpiresAt", now),
		),
		Limit: 100, SkipTotal: true, System: true,
	})
	if err != nil {
		return
	}
	for _, connection := range stale.Documents {
		_, _ = local.Delete(ctx, ConnectionsCollection, connection.ID, ridu.MutationOptions{System: true})
	}
}
