<!-- Generated from website/src/content/docs/graphql.md by scripts/sync-agent-docs.ts. -->

# GraphQL

GraphQL is an optional compiled transport. REST-only applications do not import it and therefore do
not link the parser, executable schema, handler, or GraphQL runtime into their binary.

The plugin derives one immutable executable schema from the canonical Ridu manifest at startup.
Resolvers enter the same local operation engine as REST and the SDK; GraphQL selection does not
bypass access, validation, hooks, transactions, localization, population, or redaction.

## Use it in a new project {#new-project}

Create an ordinary `starter` or `blank` project, then add the `graphqlplugin.New()` registration
below. GraphQL is Go-only and has no admin package to install: applications that do not import it do
not link its parser or execution runtime into the binary.

## Add it to an existing project {#existing-project}

Import `github.com/riducms/ridu/plugins/graphql` from the published Ridu module already used by the
project, add it to `Config.Plugins`, and run `go mod tidy`. Do not add an npm package or generated
admin import; this plugin owns a transport, not an authoring control.

## Enable the endpoint {#enable}

```go title="content/config.go"
package content

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

The default route is `POST /api/graphql`. An invalid GraphQL name, generated type/root collision,
unknown resource override, or malformed extension stops startup instead of hiding part of the
schema. Ridu does not serve a browser playground.

Start the complete development loop:

```bash title="terminal"
go mod tidy
npm run dev
```

`ridu dev` regenerates the committed SDL and other contracts before starting the API and admin.
Enabling only the transport does not add stored fields or adapter tables, so it does not need a
database migration. If the same change also modifies resources, prepare the selected adapter's
reviewed migration before deployment. Run `npm run ridu -- check` before committing so generated
SDL drift is caught.

With the server running, send an authenticated bounded query to `POST /api/graphql`. Also confirm
that an unauthorized query receives the same denial and field redaction as REST.

## Query collections {#query-collections}

Root names come from resolved singular/plural labels. A `posts` collection normally produces
`Post`, `Posts`, and `countPosts` queries:

```graphql
query RecentPosts {
	Posts(
		where: { featured: { equals: true }, score: { greater_than: 5 } }
		sort: ["-score"]
		page: 1
		limit: 20
	) {
		docs {
			id
			title
			category {
				id
				name
			}
		}
		totalDocs
		totalPages
		hasNextPage
	}
}
```

Typed filter inputs compile into Ridu's finite [query vocabulary](./querying.md). Nested group,
array, and block filter paths use flattened double underscores such as `seo__description` and
`links__label`. Selected relationship/upload fields become bounded operation-engine population and
reapply target access and redaction.

Groups and arrays have typed nested output/input objects. Select/radio choices use GraphQL enums.
Blocks return typed output unions selected with inline fragments; mutation input uses JSON because
GraphQL has no input union. Polymorphic references use `{ relationTo, value }`.

## Create and mutate {#mutations}

Collections add generated create, update, delete, and duplicate fields when mutations are enabled:

```graphql
mutation CreatePost($category: ID!) {
	createPost(data: { title: "Hello", category: $category }) {
		id
		title
		category {
			id
			name
		}
	}
}
```

Optimistic mutations accept `expectedRevision` when the resource is versioned. Duplicate runs the
ordinary create lifecycle. Upload-enabled collections deliberately omit GraphQL create: JSON cannot
carry the byte source or server-owned metadata. Use multipart/remote [upload APIs](./uploads.md)
for new files; upload document reads and metadata remain available in GraphQL.

## Globals, drafts, versions, and trash {#editorial}

Globals expose read/update and matching version/draft operations. Versioned collections add
single/list version queries and restore; draft-enabled resources add publish/unpublish. Trash adds
`trash: true` reads, restore-deleted, and permanent-delete mutations. `draft: true` includes or
writes a draft; `draft: false` selects published behavior.

These operations use the same revision checks, access rules, hooks, retention, reference cleanup,
and scheduled-state semantics as other transports. Scheduling, document locks, previews, bulk
operations, and raw file transfer remain REST/SDK-focused surfaces rather than inferred GraphQL
fields.

## Localized content {#localization}

Configured locales become a deterministic `RiduLocale` enum:

```graphql
query LocalizedPost($id: ID!) {
	english: Post(id: $id, locale: EN) {
		title
	}
	french: Post(id: $id, locale: FR, fallbackLocale: [EN]) {
		title
	}
	exactArabic: Post(id: $id, locale: AR, disableFallback: true) {
		title
	}
}
```

The selected locale reaches filters, relationships, access, hooks, and versions. GraphQL does not
overload scalar fields with the local/REST `all` locale-keyed shape; request one locale per field
alias when a query needs several translations.

## Authentication and preferences {#authentication}

Auth collections generate login, current-user/session, refresh, logout, initialized-state, unlock,
recovery, and verification fields according to configuration. The result contains an opaque token,
expiry, exact auth collection, and redacted user.

Browser applications should prefer the REST/SDK HTTP-only cookie. A non-browser GraphQL client can
authenticate with:

```text
Authorization: Session <opaque-session-token>
Authorization: Bearer ridu_<id>_<api-key-secret>
```

Session and API-key identity includes its auth collection, so colliding document IDs across auth
collections remain distinct. Admin-identity GraphQL clients also get preference read/set/delete/
reset fields with the same exact ownership contract.

## Configure resources {#resources}

Rename or suppress a generated GraphQL surface without changing the manifest slug, REST path, or
SDK type:

```go
graphqlplugin.New(graphqlplugin.Options{
	Resources: map[string]graphqlplugin.ResourceOptions{
		"posts": {
			SingularName:     "Article",
			PluralName:       "Articles",
			DisableMutations: true,
		},
	},
})
```

Disabling a GraphQL query/mutation removes only that transport field. It is not an access rule for
REST, the local API, SDK, admin, or plugin endpoints.

## Add trusted root fields {#extensions}

Compiled extension queries/mutations receive the authenticated actor, exact auth collection, local
API, and public application facade—never a store adapter:

```go
graphqlplugin.New(graphqlplugin.Options{
	Queries: []graphqlplugin.ExtensionField{
		{
			Name: "postTotal",
			Type: graphql.NewNonNull(graphql.Int),
			Cost: 5,
			Resolve: func(
				input graphqlplugin.ExtensionContext,
			) (any, error) {
				page, err := input.Local.List(
					input.Context,
					"posts",
					ridu.ListOptions{
						Page: 1, Limit: 1,
						Actor: input.Actor, ActorCollection: input.ActorCollection,
					},
				)
				return page.Total, err
			},
		},
	},
})
```

Using the supplied API keeps application authorization and lifecycle behavior intact. Extension code
is trusted compiled code; `Cost` protects query budgeting, not sandboxing.

## Generate deterministic SDL {#sdl}

Configure a project-owned destination after enabling the plugin:

```toml title="ridu.toml"
generated.graphql.schema = "./generated/ridu.graphql"
```

The development loop now writes the exact compiled schema without enabling network introspection:

```bash
npm run dev
```

Use `npm run ridu -- generate --check` in CI to verify that the committed SDL matches executable
config. Use `npm run ridu -- generate` only when you need a one-shot write without starting the
development server.

Resource renames, disabled surfaces, and custom root fields come from the same executable plugin
instance used at runtime. They are never duplicated into the serializable manifest. Ridu validates
the project-relative path through the generic `generated.<plugin>.<artifact>` mapping and installs
the SDL atomically beside the other contracts.
`graphqlplugin.GenerateSDL` remains available when application-owned Go tooling needs the bytes
directly.

Before removing the GraphQL plugin, remove its generated mapping and delete the now-unowned SDL
file. Ridu never deletes paths that disappear from `ridu.toml`.

## Resource safeguards {#limits}

Secure zero-value defaults are:

| Limit             |  Default |
| ----------------- | -------: |
| HTTP body         |    1 MiB |
| Encoded variables |  256 KiB |
| Executable depth  |       12 |
| Aliases           |      100 |
| Total complexity  |    1,000 |
| Per-list limit    |      100 |
| Introspection     | disabled |

Fragments are expanded for list/cost analysis, nested join limits are charged, and cycles fail
closed. Hard syntax/token/fragment ceilings remain even if application options are raised. Keep
introspection disabled on untrusted production endpoints unless your operational policy needs it.

```go
graphqlplugin.New(graphqlplugin.Options{
	AllowIntrospection: true,
	MaxComplexity:      2_000,
	MaxDepth:           16,
})
```

GraphQL validation failures use the normal response shape. Operation errors expose stable Ridu
`code`, `status`, and validation `issues` extensions; unexpected internal failures do not expose a
message or stack.

See the [`graphql` plugin reference](https://riducms.com/reference/graphql/), [REST API](./rest-api.md), and
[TypeScript SDK](./typescript-sdk.md). Use [Capability status](./capabilities.md) to track the
remaining conformance boundary.
