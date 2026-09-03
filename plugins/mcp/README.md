# Ridu MCP plugin

This optional compiled plugin exposes explicitly selected, read-only content tools at `/api/mcp`.
Every call uses the authenticated Ridu actor and the ordinary Local API, so collection access,
atomic filtered predicates, field redaction, localization, and response bounds are unchanged.

```go
import ridumcp "github.com/riducms/ridu/plugins/mcp"

ridu.Config{
	Plugins: []ridu.Plugin{
		ridumcp.New(ridumcp.Config{
			Collections: []ridumcp.Resource{{Slug: "posts"}},
			Globals:     []ridumcp.Resource{{Slug: "site-settings"}},
		}),
	},
}
```

Create an API key for an auth-enabled collection and configure the client with:

```json
{
	"mcpServers": {
		"Ridu": {
			"type": "http",
			"url": "http://127.0.0.1:8080/api/mcp",
			"headers": {
				"Authorization": "Bearer ridu_<id>_<secret>"
			}
		}
	}
}
```

The current slice does not expose create, update, delete, source-code mutation, prompts, or custom
resources. Tool annotations are descriptive hints; Ridu authorization remains the enforcement
boundary.
