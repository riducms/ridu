# Ridu GraphQL plugin

`plugins/graphql` adds an optional, manifest-derived GraphQL transport to a published Ridu
application. Applications that do not import this package do not link the GraphQL parser or
execution runtime. Resolvers enter the same Local API as REST and the SDK, preserving access,
validation, hooks, transactions, localization, population, and redaction.

## Install and register

Add the Go plugin and its admin playground package together:

```sh
ridu add graphql \
  --go-package github.com/riducms/ridu/plugins/graphql \
  --admin-package @riducms/plugin-graphql
```

To register it by hand, install `@riducms/plugin-graphql` in the admin workspace and add the plugin
to the executable Go config:

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

The generated admin registry imports the playground from `@riducms/plugin-graphql`. Set
`Options.DisablePlayground` to serve GraphQL without it; the admin package is then not needed.
Finish the integration with the ordinary project checks:

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

Open **GraphQL** in the admin and run the starter query. The playground sends operations with the
admin's session and receives the schema through an admin loader governed by the same admin access
policy. This does not enable GraphQL endpoint introspection. Verify that the same actor receives the
same denial and field redaction through REST. Ridu rejects invalid generated names and collisions at startup and applies
configurable body, depth, alias, list, variable, and complexity bounds before execution.

GraphQL reads, CRUD, authentication, localization, relationships, globals, drafts, versions, and
trash are generated from the resolved manifest. File transfer, scheduling, document locks, bulk
operations, and live preview remain REST/SDK-focused workflows.

See the complete [GraphQL adoption guide](https://ridu.dev/docs/graphql/) for the playground,
resource overrides, trusted extensions, committed SDL generation, security limits, examples, and
troubleshooting. The [Go API reference](https://ridu.dev/reference/graphql/) documents `New`,
`Options`, and the extension contracts.
