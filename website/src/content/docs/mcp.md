---
title: 'Model Context Protocol'
description: 'Give authenticated MCP clients read-only access to selected Ridu content.'
product: core
eyebrow: 'API'
order: 116
aliases:
  [
    'MCP',
    'AI agents',
    'Model Context Protocol',
    'OAuth',
    'Claude connector',
    'plugins/oauth'
  ]
capabilities: ['plugin.mcp', 'plugin.oauth']
availability:
  status: limited
  label: 'Authenticated read-only tools'
  description: 'Selected collection and global reads are supported. Writes, prompts, custom resources, and source mutation are not.'
  anchor: tools
navigation:
  section: 'Extend Ridu'
  parent: 'plugins'
  order: 50
  title: 'MCP'
---

Ridu's optional MCP plugin lets coding agents and other Model Context Protocol clients discover and
read selected CMS content. It is a compiled Go plugin served by the application binary at
`/api/mcp`; no Node server or separate authorization layer is introduced.

## Configuration {#configuration}

| Option          | Default    | What it controls                                                                   |
| --------------- | ---------- | ---------------------------------------------------------------------------------- |
| `Path`          | `/api/mcp` | Absolute transport path below `/api`, without a trailing slash.                    |
| `MaxBodyBytes`  | 1 MiB      | Maximum MCP request body size.                                                     |
| `DefaultLimit`  | `20`       | Collection documents returned when a tool call omits its limit.                    |
| `MaxLimit`      | `100`      | Highest collection limit a tool call may request.                                  |
| `Collections`   | None       | Explicit collection allowlist; each entry can add a task-focused `Description`.    |
| `Globals`       | None       | Explicit global allowlist; each entry can add a task-focused `Description`.        |
| `Authorization` | None       | The authorization server clients sign in with, such as the [OAuth plugin](#oauth). |

`ridumcp.New(config)` returns the compiled plugin. Only listed resources become tools, and every
tool call still uses the authenticated actor's ordinary Ridu access rules.

## Use it in a new project {#new-project}

Start with a generated project, enable API keys on the auth collection that will own agent
identities, and add `ridumcp.New` below. MCP is a Go-only transport: there is no npm/admin half to
install or pair. To let clients sign in instead, add the [OAuth plugin](#oauth) too.

## Add it to an existing project {#existing-project}

Import `github.com/riducms/ridu/plugins/mcp` from the project's published Ridu module and register it
in executable config. Run `go mod tidy`; do not add a JavaScript MCP server or a second content API.
Choose the smallest explicit resource allowlist and a least-privileged auth user before issuing a
key.

## Enable selected resources {#enable}

```go title="content/config.go"
import ridumcp "github.com/riducms/ridu/plugins/mcp"

func Config() ridu.Config {
	return ridu.Config{
		// ...
		Plugins: []ridu.Plugin{
			ridumcp.New(ridumcp.Config{
				Collections: []ridumcp.Resource{
					{Slug: "posts", Description: "Published editorial posts."},
				},
				Globals:      []ridumcp.Resource{{Slug: "site-settings"}},
				DefaultLimit: 20,
				MaxLimit:     100,
			}),
		},
	}
}
```

Only listed resources become tools. Configuration with an unknown resource or colliding normalized
tool name fails application startup. Saving the config while `ridu dev` is running regenerates the
tool contract before restarting the application.

Complete and verify the installation:

```bash title="terminal"
go mod tidy
npm run dev
```

The transport alone adds no adapter tables, so it does not need a migration. If enabling API keys
or the same change alters the resolved resource schema, prepare the selected adapter's reviewed
migration before deployment. Run `ridu check` before committing. Test `initialize`,
`tools/list`, and one allowed read with an expiring key; also confirm that an anonymous request, an
unlisted collection, and a field denied to that actor are rejected.

## Authenticate the client {#authenticate}

Enable API keys on the relevant auth collection, create an expiring key for a least-privileged
user, and send it as a bearer token:

```json title="mcp.json"
{
	"mcpServers": {
		"Ridu": {
			"type": "http",
			"url": "https://cms.example.com/api/mcp",
			"headers": {
				"Authorization": "Bearer ridu_<id>_<secret>"
			}
		}
	}
}
```

Anonymous requests receive `401`, unsupported methods receive `405`, and request body and result
sizes are bounded. Keep keys out of source control, set expirations, and revoke keys that are no
longer used.

## Sign in with OAuth {#oauth}

Connectors such as Claude's sign in with OAuth 2.1 instead of taking an API key. The optional OAuth
plugin makes the application their authorization server: clients register themselves, a person
approves each connection on the admin's consent page, and the connection's token then acts as the
user that person chose. Add both plugins and pass the OAuth plugin to MCP as its `Authorization`:

```go title="content/config.go"
import (
	ridumcp "github.com/riducms/ridu/plugins/mcp"
	"github.com/riducms/ridu/plugins/oauth"
)

func Config() ridu.Config {
	authorization := oauth.New(oauth.Config{})
	return ridu.Config{
		// ...
		Plugins: []ridu.Plugin{
			authorization,
			ridumcp.New(ridumcp.Config{
				Collections:   []ridumcp.Resource{{Slug: "posts"}},
				Authorization: authorization,
			}),
		},
	}
}
```

The OAuth plugin has an admin half, `@riducms/plugin-oauth`, which holds the consent page. Install it
in the admin workspace with `bun add --cwd admin @riducms/plugin-oauth`; the generated admin registry
picks it up from the Go plugin. Register the Go plugins in `content/config.go` as above rather than
with `ridu add`, which can't hand the OAuth plugin to MCP. The plugin adds the `oauth-clients` and
`oauth-connections` collections, so create a migration before deploying. Then add the server as a
connector with only its MCP URL, such as `https://cms.example.com/api/mcp`.

The client finds everything from that URL:

1. A request without a token gets `401` with a `WWW-Authenticate` challenge naming the protected
   resource metadata (RFC 9728) at `/.well-known/oauth-protected-resource/api/mcp`.
2. That names this server, whose metadata (RFC 8414) is at `/.well-known/oauth-authorization-server`.
3. The client registers itself at `/api/plugins/oauth/register` (RFC 7591) and sends the person's
   browser to `/admin/oauth/authorize`, signing them in first if needed.
4. The person chooses whom the client acts as and allows it. The client exchanges the code, with its
   PKCE verifier, at `/api/plugins/oauth/token` and calls MCP with the access token.

| Option                 | Default                 | What it controls                                                                                     |
| ---------------------- | ----------------------- | ---------------------------------------------------------------------------------------------------- |
| `Users`                | The admin's `User`      | The auth collection whose members approve connections and whom tokens act as.                        |
| `Resource`             | `/api/mcp`              | The path tokens are for; match the MCP plugin's `Path`.                                              |
| `PublicURL`            | From the request's host | The public origin in metadata, such as `https://cms.example.com`; https unless the host is loopback. |
| `ActAs`                | Only the approver       | Other users an approver may let a client act as, such as agent accounts they own.                    |
| `ManageConnections`    | No one                  | Who may see and revoke every connection, not only those they approved or that act as them.           |
| `CodeLifetime`         | 10 minutes              | How long an authorization code can be exchanged.                                                     |
| `AccessTokenLifetime`  | 1 hour                  | How long an access token works before the client refreshes it.                                       |
| `RefreshTokenLifetime` | 90 days                 | How long a connection lasts unused. Each refresh replaces the refresh token.                         |

Only public clients with PKCE (`S256`) can register, and redirect URIs must be `https`, or `http` on
a loopback address for an app on the person's computer. Codes and refresh tokens work once, even
when two requests race, and every secret is stored as a SHA-256 hash. An access token acts as its
user everywhere an API key would, under the same access rules.

People see their connections under **Connected apps** in the admin: those they approved and those
that act as them. Deleting one revokes it at once, as does revoking a token at
`/api/plugins/oauth/revoke` (RFC 7009). Deleting a user removes their connections.

## Authorization and tools {#tools}

Each collection contributes `ridu_find_collection_<slug>` with pagination, selection, locale,
fallback, all-locale, and draft inputs. Its `pagination: false` input skips the total count: the
result leaves out `totalDocs` and keeps an exact `hasNextPage`. Each global contributes
`ridu_find_global_<slug>` with selection and localization inputs.

Every call enters the ordinary Local API and operation engine with the API key's owning actor, or
the user an OAuth connection acts as.
Filtered collection access remains in the store query, field access redacts values, and localization
and draft rules are unchanged. Tool annotations describe read-only behavior; they are not the
security boundary.

The current plugin does not provide create, update, delete, prompts, custom resources, or edits
to Go config. Those require an explicit per-key side-effect capability and audit contract before
Ridu can expose them safely.

See the [`plugins/mcp`](/reference/mcp/) and [`plugins/oauth`](/reference/oauth/) API references
for the exact exported configuration types.
