# Ridu OAuth plugin

`plugins/oauth` makes a Ridu application an OAuth 2.1 authorization server for its MCP endpoint, so
connectors such as Claude's sign in instead of being given an API key. It supports dynamic client
registration (RFC 7591), authorization codes with PKCE `S256` (RFC 7636), rotating refresh tokens,
revocation (RFC 7009), and authorization server and protected resource metadata (RFC 8414, RFC 9728)
at `/.well-known/`.

A person approves each connection on the admin consent page from `@riducms/plugin-oauth`, choosing
the user the client acts as. The connection's access token then authenticates as that user through
an auth strategy, so every MCP call runs under that user's ordinary access rules. Connections are
documents in `oauth-connections`, listed in the admin as **Connected apps**; deleting one revokes it
at once.

## Install and register

Install the consent page in the admin workspace and register both plugins in the executable config,
passing the OAuth plugin to MCP so its `401` challenge tells clients where to sign in:

```sh
bun add --cwd admin @riducms/plugin-oauth
```

```go
authorization := oauth.New(oauth.Config{})
config.Plugins = append(config.Plugins, authorization, mcp.New(mcp.Config{
	Collections:   []mcp.Resource{{Slug: "posts"}},
	Authorization: authorization,
}))
```

The plugin adds the `oauth-clients` and `oauth-connections` collections; create a migration before
deploying. `Config` chooses the user collection, the protected resource path, the public origin,
other users an approver may delegate to (`ActAs`), who may manage every connection, and the code and
token lifetimes.

See the [MCP guide](../../website/src/content/docs/mcp.md#oauth) for the flow and options.
