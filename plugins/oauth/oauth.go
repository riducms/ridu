// Package oauth makes a Ridu application an OAuth 2.1 authorization server
// for its MCP endpoint, so a remote MCP client such as Claude's connector
// signs in instead of being given an API key. It supports dynamic client
// registration (RFC 7591), authorization codes with PKCE (RFC 7636), rotating
// refresh tokens, token revocation (RFC 7009), authorization server metadata
// (RFC 8414) and protected resource metadata (RFC 9728).
//
// A person approves each connection on the admin's consent page, choosing the
// user the client acts as. The connection's access token then authenticates
// as that user through an auth strategy, so access rules apply exactly as
// they do to the user's own requests. Connections are documents in the
// oauth-connections collection: the user who approved one, the user it acts
// as, and anyone ManageConnections allows can see and delete it, and deleting
// it stops its tokens at once.
//
// Pair it with the MCP plugin:
//
//	authorization := oauth.New(oauth.Config{})
//	config.Plugins = append(config.Plugins, authorization, mcp.New(mcp.Config{
//		Collections:   []mcp.Resource{{Slug: "posts"}},
//		Authorization: authorization,
//	}))
package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const (
	// Key is the stable compiled plugin identity.
	Key = "oauth"
	// ClientsCollection holds registered clients. Only the plugin reads or
	// writes it.
	ClientsCollection = "oauth-clients"
	// ConnectionsCollection holds approved connections and the hashes of
	// their pending code and current tokens.
	ConnectionsCollection = "oauth-connections"
	// AdminPluginPairingVersion changes when the Go plugin and its admin
	// package stop agreeing on the consent page's endpoints.
	AdminPluginPairingVersion = 1

	adminPackage  = "@riducms/plugin-oauth"
	consentRoute  = "oauth/authorize"
	defaultPath   = "/api/mcp"
	maxBodyBytes  = 16 << 10
	strategyName  = "oauth-access-token"
	accessPrefix  = "riduoauth_at_"
	refreshPrefix = "riduoauth_rt_"
	codePrefix    = "riduoauth_ac_"
)

// Config configures the authorization server. The zero value serves the MCP
// plugin's default endpoint for the admin's users.
type Config struct {
	// Users is the auth collection whose members approve connections and
	// whom tokens act as. It defaults to the admin's user collection.
	Users schema.CollectionSlug
	// Resource is the path of the protected resource tokens are for. It
	// defaults to /api/mcp, the MCP plugin's default path.
	Resource string
	// PublicURL is the server's public origin, such as
	// https://cms.example.com, used in metadata and redirects. When empty it
	// comes from the request's Host header, which Ridu checks against
	// AllowedHosts: https, or http for a loopback host.
	PublicURL string
	// ActAs lists the other users an approver may let a client act as, such
	// as agent accounts they own. The approver is always a choice. Nil means
	// a client can act only as the person who approves it.
	ActAs func(ctx context.Context, local *ridu.LocalAPI, approver store.Document) ([]store.Document, error)
	// ManageConnections reports whether the actor may see and revoke every
	// connection, not only those they approved or that act as them.
	ManageConnections func(ridu.AccessContext) (bool, error)
	// CodeLifetime bounds how long an authorization code can be exchanged.
	// It defaults to ten minutes.
	CodeLifetime time.Duration
	// AccessTokenLifetime defaults to one hour.
	AccessTokenLifetime time.Duration
	// RefreshTokenLifetime defaults to 90 days. Each refresh replaces the
	// refresh token, so a connection lasts while the client keeps using it.
	RefreshTokenLifetime time.Duration
}

// Plugin is one configured authorization server.
type Plugin struct {
	config Config
	now    func() time.Time
}

// New returns the plugin. Pass it to Config.Plugins and, as Authorization,
// to the MCP plugin whose endpoint it protects.
func New(config Config) *Plugin {
	if strings.TrimSpace(config.Resource) == "" {
		config.Resource = defaultPath
	}
	config.PublicURL = strings.TrimSuffix(strings.TrimSpace(config.PublicURL), "/")
	if config.CodeLifetime <= 0 {
		config.CodeLifetime = 10 * time.Minute
	}
	if config.AccessTokenLifetime <= 0 {
		config.AccessTokenLifetime = time.Hour
	}
	if config.RefreshTokenLifetime <= 0 {
		config.RefreshTokenLifetime = 90 * 24 * time.Hour
	}
	return &Plugin{config: config, now: time.Now}
}

func (*Plugin) Key() string { return Key }

func (*Plugin) Descriptor() ridu.PluginDescriptor {
	return ridu.PluginDescriptor{
		Version: ridu.FrameworkVersion, GoPackage: "github.com/riducms/ridu/plugins/oauth",
		APIVersion: ridu.PluginAPIVersion,
		Ridu:       ridu.RiduCompatibility{Minimum: ridu.FrameworkVersion},
		Admin: &ridu.AdminPluginMetadata{
			Package: adminPackage, Export: "oauthAdminPlugin",
			APIVersion: ridu.AdminPluginAPIVersion, PairingVersion: AdminPluginPairingVersion,
			Routes: []string{consentRoute},
		},
	}
}

// TransformConfig adds the clients and connections collections and the auth
// strategy that accepts connections' access tokens.
func (plugin *Plugin) TransformConfig(config ridu.Config) (ridu.Config, error) {
	if plugin.config.Users == "" {
		plugin.config.Users = config.Admin.User
	}
	users := -1
	for index, collection := range config.Collections {
		switch collection.Slug {
		case plugin.config.Users:
			users = index
		case ClientsCollection, ConnectionsCollection:
			return config, fmt.Errorf("the OAuth plugin defines the %q collection, which the application already has", collection.Slug)
		}
	}
	if users < 0 || !config.Collections[users].Auth {
		return config, fmt.Errorf("the OAuth plugin needs an auth collection for its users; set Config.Users or Admin.User")
	}
	usersCollection := &config.Collections[users]
	usersCollection.AuthConfig.Strategies = append(usersCollection.AuthConfig.Strategies, plugin.strategy())
	config.Collections = append(config.Collections, clientsCollection(), plugin.connectionsCollection())
	return config, nil
}

// Endpoints serve registration, tokens, revocation and the consent page's
// two steps below /api/plugins/oauth/.
func (plugin *Plugin) Endpoints() []ridu.Endpoint {
	endpoint := func(path, summary string, handler ridu.EndpointHandler) ridu.Endpoint {
		return ridu.Endpoint{Method: http.MethodPost, Path: path, Summary: summary, MaxBodyBytes: maxBodyBytes, Handler: handler}
	}
	return []ridu.Endpoint{
		endpoint("register", "Register an OAuth client (RFC 7591)", plugin.register),
		endpoint("token", "Exchange an authorization code or refresh token", plugin.token),
		endpoint("revoke", "Revoke an OAuth token (RFC 7009)", plugin.revoke),
		endpoint("authorization", "Check an authorization request for the consent page", plugin.authorization),
		endpoint("decision", "Approve or refuse an authorization request", plugin.decision),
	}
}

// BindTransports serves the metadata where clients look for it: the
// authorization server's at the root (RFC 8414) and the protected
// resource's both at the root and under the resource's path (RFC 9728).
func (plugin *Plugin) BindTransports(ridu.PluginTransportContext) ([]ridu.Endpoint, error) {
	metadata := func(path, summary string, handler ridu.EndpointHandler) ridu.Endpoint {
		return ridu.Endpoint{Method: http.MethodGet, Path: path, Summary: summary, Handler: handler}
	}
	return []ridu.Endpoint{
		metadata("/.well-known/oauth-authorization-server", "OAuth authorization server metadata", plugin.authorizationServer),
		metadata("/.well-known/oauth-protected-resource", "OAuth protected resource metadata", plugin.protectedResource),
		metadata("/.well-known/oauth-protected-resource"+plugin.config.Resource, "OAuth protected resource metadata for "+plugin.config.Resource, plugin.protectedResource),
	}, nil
}

// Challenge is the WWW-Authenticate value for a request to the protected
// resource without a working credential. It points the client to the
// protected resource metadata, where it finds this server, and tells a
// client whose bearer token was refused to refresh it.
func (plugin *Plugin) Challenge(request *http.Request) string {
	challenge := `Bearer realm="ridu"`
	if _, presented := bearerToken(request.Header); presented {
		challenge += `, error="invalid_token"`
	}
	return challenge + `, resource_metadata="` + plugin.origin(request) + `/.well-known/oauth-protected-resource` + plugin.config.Resource + `"`
}

// strategy authenticates a connection's access token as the user it acts
// as. Ridu runs strategies only for bearer tokens that aren't its API keys.
func (plugin *Plugin) strategy() ridu.AuthStrategy {
	return ridu.AuthStrategy{
		Name: strategyName,
		Authenticate: func(ctx ridu.AuthStrategyContext) (ridu.AuthStrategyResult, error) {
			token, _ := bearerToken(ctx.Headers)
			if !strings.HasPrefix(token, accessPrefix) {
				return ridu.AuthStrategyResult{}, nil
			}
			connection, found, err := findConnection(ctx.Context, ctx.Local, "accessTokenHash", digest(token))
			if err != nil || !found || !plugin.before(connection.Values, "accessExpiresAt") {
				return ridu.AuthStrategyResult{}, err
			}
			user := reference(connection.Values, "user")
			return ridu.AuthStrategyResult{Authenticated: user != "", UserID: user}, nil
		},
	}
}

// origin is the server's public origin for request.
func (plugin *Plugin) origin(request *http.Request) string {
	if plugin.config.PublicURL != "" {
		return plugin.config.PublicURL
	}
	host := request.Host
	hostname := host
	if name, _, err := net.SplitHostPort(host); err == nil {
		hostname = name
	}
	if isLoopback(hostname) {
		return "http://" + host
	}
	return "https://" + host
}

// resource is the protected resource's URL, as tokens name it.
func (plugin *Plugin) resource(request *http.Request) string {
	return plugin.origin(request) + plugin.config.Resource
}

// bearerToken is the token of an Authorization: Bearer header, and whether
// the header names one at all.
func bearerToken(headers map[string][]string) (string, bool) {
	scheme, token, found := strings.Cut(http.Header(headers).Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

// before reports whether the time stored in values[name] is still ahead.
func (plugin *Plugin) before(values store.Values, name string) bool {
	raw, _ := values[name].StringValue()
	at, err := time.Parse(time.RFC3339, raw)
	return err == nil && plugin.now().Before(at)
}

func (plugin *Plugin) stamp(after time.Duration) store.Value {
	return store.String(plugin.now().Add(after).UTC().Format(time.RFC3339))
}

// findConnection reads the connection whose field holds hash, as the
// plugin's own trusted code.
func findConnection(ctx context.Context, local *ridu.LocalAPI, field, hash string) (store.Document, bool, error) {
	page, err := local.List(ctx, ConnectionsCollection, ridu.ListOptions{Where: query.Equal(field, hash), Limit: 1, SkipTotal: true, System: true})
	if err != nil || len(page.Documents) == 0 {
		return store.Document{}, false, err
	}
	return page.Documents[0], true, nil
}

func text(values store.Values, name string) string {
	value, _ := values[name].StringValue()
	return value
}

// reference is a relationship's target ID, whether or not it was populated.
func reference(values store.Values, name string) string {
	if id, ok := values[name].StringValue(); ok {
		return id
	}
	if document, ok := values[name].CopyDocument(); ok {
		return document.ID
	}
	return ""
}

var (
	_ ridu.DescriptorProvider = (*Plugin)(nil)
	_ ridu.ConfigTransformer  = (*Plugin)(nil)
	_ ridu.EndpointProvider   = (*Plugin)(nil)
	_ ridu.TransportProvider  = (*Plugin)(nil)
)
