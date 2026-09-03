# Ridu GraphQL plugin

`plugins/graphql` adds an optional, manifest-derived GraphQL transport to a published Ridu
application. Applications that do not import this package do not link the GraphQL parser or
execution runtime. Resolvers enter the same Local API as REST and the SDK, preserving access,
validation, hooks, transactions, localization, population, and redaction.

## Install and register

Add the package to an existing Ridu project and register it in the executable Go config:

```go
import (
	"github.com/riducms/ridu"
	graphqlplugin "github.com/riducms/ridu/plugins/graphql"
)

func Config() ridu.Config {
	return ridu.Config{
		Name: "Acme Editorial",
		Plugins: []ridu.Plugin{
			graphqlplugin.New(),
		},
		Collections: []ridu.Collection{Users, Posts},
	}
}
```

GraphQL has no admin companion package. Finish the integration with the ordinary project checks:

```sh
go mod tidy
ridu generate
ridu migrate plan
ridu check
ridu dev
```

The default endpoint is `POST /api/graphql`. Enabling the transport alone does not add stored
fields or require a migration; create and apply one when the same change also modifies the schema.
For deterministic SDL drift checks, configure and commit the generated schema described in the
guide.

## Verify the outcome

Send an authenticated bounded query to `/api/graphql`, then verify the same actor receives the same
denial and field redaction through REST. Ridu rejects invalid generated names and collisions at
startup and applies configurable body, depth, alias, list, variable, and complexity bounds before
execution.

GraphQL reads, CRUD, authentication, localization, relationships, globals, drafts, versions, and
trash are generated from the resolved manifest. File transfer, scheduling, document locks, bulk
operations, and live preview remain REST/SDK-focused workflows.

See the complete [GraphQL adoption guide](https://ridu.dev/docs/graphql/) for resource overrides,
trusted extensions, committed SDL generation, security limits, examples, and troubleshooting. The
[Go API reference](https://ridu.dev/reference/graphql/) documents `New`, `Options`, and the extension
contracts.
