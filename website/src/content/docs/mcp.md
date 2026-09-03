---
title: 'Model Context Protocol'
description: 'Give authenticated MCP clients read-only access to selected Ridu content.'
product: core
eyebrow: 'API'
order: 116
aliases: ['MCP', 'AI agents', 'Model Context Protocol']
capabilities: ['plugin.mcp']
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

## Use it in a new project {#new-project}

Start with a generated project, enable API keys on the auth collection that will own agent
identities, and add `ridumcp.New` below. MCP is a Go-only transport: there is no npm/admin half to
install or pair.

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
migration before deployment. Run `npm run ridu -- check` before committing. Test `initialize`,
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

## Authorization and tools {#tools}

Each collection contributes `ridu_find_collection_<slug>` with pagination, selection, locale,
fallback, all-locale, and draft inputs. Each global contributes
`ridu_find_global_<slug>` with selection and localization inputs.

Every call enters the ordinary Local API and operation engine with the API key's owning actor.
Filtered collection access remains in the store query, field access redacts values, and localization
and draft rules are unchanged. Tool annotations describe read-only behavior; they are not the
security boundary.

The current plugin does not provide create, update, delete, prompts, custom resources, or edits
to Go config. Those require an explicit per-key side-effect capability and audit contract before
Ridu can expose them safely.

See the [`plugins/mcp` API reference](/reference/mcp/) for the exact exported configuration types.
