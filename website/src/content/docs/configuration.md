---
title: 'Configuration'
description: 'Compose the application, admin, localization, collections, globals, plugins, and runtime services.'
product: core
eyebrow: 'Core'
order: 40
navigation:
  section: 'Model content'
  order: 10
  title: 'Configuration'
---

Your content model and server behaviour begin as a typed `ridu.Config` value. Ridu resolves it into
the schema used by the database, REST API, generated Go and TypeScript contracts, and the admin.

Projects put the composition function in `content/config.go` by default, but you may split config
across any number of Go files.

## Application config {#application-config}

At minimum, a config needs a non-empty application name and one collection:

```go title="content/config.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

func Config() ridu.Config {
	return ridu.Config{
		Name: "Acme Editorial",
		Collections: []ridu.Collection{
			{
				Slug: "posts",
				Fields: []field.Definition{
					field.Text("title", field.Required()),
					field.Textarea("summary"),
				},
			},
		},
	}
}
```

Because config is Go, the compiler catches misspelled fields, wrong callback signatures, and field
options used with incompatible constructors. Resolution then handles rules that require the whole
application, such as duplicate slugs, missing relationship targets, invalid locale graphs, and
plugin compatibility.

### Config options {#config-options}

| Option             | Required                 | What it controls                                                                                                                                           |
| ------------------ | ------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Name`             | Yes                      | The author-facing application name shown by Ridu tooling and the admin. Whitespace is trimmed and an empty name is rejected.                               |
| `Collections`      | Yes                      | Repeatable document models. At least one collection is required.                                                                                           |
| `Globals`          | No                       | Singleton documents such as site settings or navigation.                                                                                                   |
| `Endpoints`        | No                       | Compiled root custom endpoints below `/api`; handlers are anonymous by default.                                                                            |
| `Admin`            | When auth is enabled     | `Admin.User` selects which auth-enabled collection may enter the admin.                                                                                    |
| `Localization`     | No                       | Content locales, the default locale, ordered fallbacks, right-to-left metadata, and request-visible locales. The zero value disables content localization. |
| `Hooks`            | No                       | Application-wide failure hooks. Collection and global lifecycle hooks live on the resource they affect.                                                    |
| `Plugins`          | No                       | Compiled extensions, applied in declaration order.                                                                                                         |
| `AfterCommit`      | No                       | A dispatcher for collection and global hooks that run after a successful transaction.                                                                      |
| `Storage`          | When uploads are enabled | The object backend for upload bytes. It can instead be supplied lazily with `ridu.WithUploadStorage`.                                                      |
| `StorageNamespace` | When uploads are enabled | A stable application-owned prefix for objects in a shared backend.                                                                                         |

See the [`ridu.Config` reference](/reference/ridu/config/) for every field.

## How Ridu reads config {#source-of-truth}

Ridu does not parse Go files looking for collections. The project entry calls your `Config()`
function, so access rules, hooks, computed fields, and plugin constructors execute as Go code.
The CLI finds that entry through `ridu.toml`:

```toml title="ridu.toml"
version = 1
database = "postgres" # or "sqlite" or "mongodb"; credentials remain runtime-owned
package_manager = "npm" # or "bun", "pnpm", or "yarn"
entry = "./cmd/server"
admin = "./admin"
schema = "./generated/ridu.schema.json"
client = "./generated/ridu.generated.ts"
openapi = "./generated/ridu.openapi.json"
# Optional after enabling the GraphQL plugin:
# generated.graphql.schema = "./generated/ridu.graphql"
migrations = "./migrations"
assets = "./internal/adminassets/dist"
plugins = "./ridu.plugins.json"
plugin_go = "./content/ridu_plugins.generated.go"
```

`ridu generate`, `ridu dev`, and migration commands run the configured entry to resolve the current
schema. Plugin transforms run in declaration order before Ridu validates the application and creates
the schema manifest. Hooks, access rules, computed resolvers, storage, and endpoint handlers remain
in the Go runtime.

<aside class="callout" data-variant="tip">
<strong>Keep config pure</strong>
<p>Construct values in <code>Config()</code>, but open databases, storage clients, and other external services in lazy runtime factories. Generation should not need credentials or a reachable database.</p>
</aside>

Change `content/*.go` rather than generated JSON. The development loop regenerates the manifest and
other derived files.

## Compose a larger application {#compose-configuration}

As the application grows, declare static collections and globals as focused package variables.
Their filenames and package layout are yours to choose.

```go title="content/config.go"
package content

import "github.com/riducms/ridu"

func Config() ridu.Config {
	return ridu.Config{
		Name:        "Acme Editorial",
		Admin:       ridu.AdminConfig{User: "users"},
		Plugins:     installedPlugins(),
		Collections: []ridu.Collection{Users, Posts, Media},
		Globals:     []ridu.Global{SiteSettings},
	}
}
```

For example, the `Posts` collection can live beside it with its imports and behaviour close to the
model it belongs to:

```go title="content/posts.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/richtext"
)

var Posts = ridu.Collection{
	Slug: "posts",
	Admin: ridu.CollectionAdmin{
		UseAsTitle:     "title",
		DefaultColumns: []string{"title", "status"},
		Group:          "Editorial",
	},
	Fields: []field.Definition{
		field.Text("title", field.Required()),
		field.Select(
			"status",
			field.OneOf("draft", "published"),
			field.Default("draft"),
		),
		richtext.Field("content"),
	},
}
```

Ridu defensively copies these declarations before plugins transform or validate the configuration.
Treat exported collection variables as definitions rather than shared application state: use a
function instead when construction needs options, runtime values, fallible setup, or a fresh
caller-owned mutable value.

`ridu add` and `ridu plugin remove` update `installedPlugins()`, Go dependencies, and matching admin
imports together. Add application-local plugins directly to `Config.Plugins`.

See [Collections and globals](/docs/collections/) for capabilities and lifecycle behaviour, and
[Fields](/docs/fields/) for the complete field vocabulary.

## Configure the admin identity {#admin-config}

`Config.Admin` selects which auth collection owns admin sessions. It does not enable authentication
by itself. Set `Auth: true` on the collection, then name that collection in `Admin.User`:

```go title="content/config.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

func Config() ridu.Config {
	return ridu.Config{
		Name:  "Acme Editorial",
		Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{
				Slug: "users",
				Auth: true,
				Fields: []field.Definition{
					field.Text("email", field.Required(), field.Unique()),
				},
			},
			Posts,
		},
	}
}
```

If several collections enable auth, only the selected collection gains admin access. Ridu rejects
an absent, unknown, or non-auth-enabled `Admin.User` whenever any auth collection exists. An
application without auth collections may leave `Admin` at its zero value.

Collection list columns, labels, groups, descriptions, and live preview belong to
`ridu.CollectionAdmin`; global presentation belongs to `ridu.GlobalAdmin`. Those values shape the
admin but do not grant authorization. See [Admin](/docs/admin/) and
[Authentication](/docs/authentication/) for those separate concerns.

Admin interface language is separate from content locale. Configure `Admin.Localization` with the
allowed BCP-47 languages, default language, browser-safe IANA timezones (or fixed offsets), and the
default timezone. English, French, and Arabic catalogs ship with Ridu. Editors can persist their
choice from the account screen; the whole interface, accessibility text, plural rules, formatting,
and right-to-left layout update together. Application, resource, field, choice, relationship,
language, and timezone labels can provide `LabelTranslations` maps without changing stable IDs.

## Add content localization {#localization}

Localization is opt-in. Once it is configured, mark individual fields with `field.Localized()`;
ordinary fields continue to store one shared value.

```go title="content/config.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

func Config() ridu.Config {
	return ridu.Config{
		Name: "Acme Editorial",
		Localization: ridu.LocalizationConfig{
			DefaultLocale: "en",
			Locales: []ridu.Locale{
				{Code: "en", Label: "English"},
				{
					Code:            "fr",
					Label:           "Français",
					FallbackLocales: []schema.LocaleCode{"en"},
				},
				{
					Code:            "ar",
					Label:           "العربية",
					RTL:             true,
					FallbackLocales: []schema.LocaleCode{"en"},
				},
			},
		},
		Collections: []ridu.Collection{
			{
				Slug: "posts",
				Fields: []field.Definition{
					field.Text("title", field.Required(), field.Localized()),
					field.Text("slug", field.Required(), field.Unique()),
				},
			},
		},
	}
}
```

Every configured locale needs a unique valid code and non-empty label. `DefaultLocale` must name
one of them. Fallbacks are enabled by default and follow each locale's ordered
`FallbackLocales`; unknown locales, self-fallbacks, duplicates, and cycles fail resolution. Set
`DisableFallback: true` when reads must return only the exact requested locale.

`Localization.AvailableLocales` can reduce the locales shown to a particular admin request using
its actor and local API. It is executable request policy, so it is not serialized into the public
manifest and does not weaken locale validation or authorization. See the localization options in
the [schema reference](/reference/schema/locale-code/).

## Register plugins {#plugins}

Plugins are compiled Go values. Ridu checks compatibility and applies config transforms in
declaration order. Runtime callbacks and secrets remain in the binary.

```go title="content/config.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/plugins/graphql"
	"github.com/riducms/ridu/plugins/richtext"
)

func Config() ridu.Config {
	return ridu.Config{
		Name: "Acme Editorial",
		Plugins: []ridu.Plugin{
			richtext.New(),
			graphql.New(graphql.Options{MaxDepth: 10}),
		},
		Collections: []ridu.Collection{Users, Posts},
	}
}
```

When a plugin contributes an admin package, generation emits a static import and verifies its key,
API version, and pairing version against the backend descriptor. Production never downloads or
installs plugin code dynamically. See [Plugins](/docs/plugins/) for installation, removal, and
authoring.

## Configure root hooks and after-commit dispatch {#hooks-and-after-commit}

`Config.Hooks` contains application-wide failure observation. Put operation lifecycle behaviour on
the collection or global that owns it; those resource hooks have the document, original values,
actor, locale, and local API in their context.

```go title="content/config.go"
package content

import (
	"log/slog"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/core"
)

func reportRootFailure(ctx ridu.HookContext) error {
	slog.Error(
		"ridu operation failed",
		"operation", ctx.Operation,
		"error", ctx.Error,
	)
	return nil
}

func Config() ridu.Config {
	return ridu.Config{
		Name: "Acme Editorial",
		Hooks: core.RootHooks{
			AfterError: []ridu.Hook{reportRootFailure},
		},
		Collections: []ridu.Collection{Users, Posts},
	}
}
```

Resource `AfterCommit` hooks run only after the document transaction succeeds. Without a custom
dispatcher Ridu runs them immediately. Set `Config.AfterCommit` to an implementation of
`ridu.AfterCommitDispatcher` when the application needs one place to schedule, instrument, or
otherwise control committed effects. The dispatcher receives operation and resource identity plus
the callback to run; an error at this point cannot roll the committed document back.

See [Hooks](/docs/hooks/) for the full lifecycle and transaction boundaries.

## Configure upload storage {#upload-storage}

Upload collections store document metadata in the document-store adapter and file bytes in an
object-storage adapter. Every upload-enabled application also needs a stable `StorageNamespace`;
do not derive it from `Config.Name`, because the display name can change.

For runtime-created clients, prefer `ridu.WithUploadStorage`. Its factory is evaluated only when
the server starts, not while the CLI resolves config:

```go title="cmd/server/main.go" add={12,24-32}
package main

import (
	"context"
	"log"
	"os"

	"example.com/acme/content"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/adapters/storage/s3"
	"github.com/riducms/ridu/storage"
	"github.com/riducms/ridu/store"
)

func main() {
	err := ridu.Execute(
		content.Config(),
		ridu.WithStore(func(ctx context.Context) (store.Store, error) {
			return postgres.Open(ctx, os.Getenv("DATABASE_URL"))
		}),
		ridu.WithAddress(":8080"),
		ridu.WithUploadStorage(func(context.Context) (storage.Backend, error) {
			return s3.New(s3.Config{
				Endpoint:  os.Getenv("S3_ENDPOINT"),
				Region:    os.Getenv("S3_REGION"),
				Bucket:    os.Getenv("S3_BUCKET"),
				AccessKey: os.Getenv("S3_ACCESS_KEY"),
				SecretKey: os.Getenv("S3_SECRET_KEY"),
			})
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
}
```

The content config still declares the namespace and upload collection:

```go title="content/config.go"
package content

import "github.com/riducms/ridu"

func Config() ridu.Config {
	return ridu.Config{
		Name:             "Acme Editorial",
		StorageNamespace: "acme-content",
		Collections:      []ridu.Collection{Users, Posts, Media},
	}
}
```

`ridu.Resolve` validates the serializable upload schema. Runtime construction additionally checks
that upload storage is present and that the document store supports every enabled capability. See
[Uploads](/docs/uploads/) and [Object storage](/docs/storage/) for backend and privacy options.

## Server entry {#server-entry}

`ridu.Execute` joins config to runtime services. A PostgreSQL server entry looks like this:

```go title="cmd/server/main.go"
package main

import (
	"context"
	"log"
	"os"

	"example.com/acme/content"
	"example.com/acme/internal/adminassets"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/store"
)

func main() {
	err := ridu.Execute(
		content.Config(),
		ridu.WithStore(func(ctx context.Context) (store.Store, error) {
			return postgres.Open(ctx, os.Getenv("DATABASE_URL"))
		}),
		ridu.WithAddress(":8080"),
		ridu.WithHandlerOptions(ridu.HandlerOptions{
			AdminAssets: adminassets.FS(),
			AllowedOrigins: []string{
				"https://www.example.com",
			},
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
}
```

SQLite-selected projects import `adapters/sqlite` and open `RIDU_SQLITE_PATH`; see
[SQLite](/docs/sqlite/). MongoDB-selected projects import `adapters/mongodb`, open `DATABASE_URL`,
and verify the resolved index plan before serving; see [MongoDB](/docs/mongodb/).

Use `ridu.New(config, store)` for tests, embedded use, or a custom HTTP process. It still applies
access, validation, and hooks. Build production applications with `ridu build`; a direct `go build`
omits the migration-history fingerprint, so `/readyz` fails with an official adapter.

## Validate before runtime {#validate-before-runtime}

Use `ridu.Resolve` in focused tests to validate config and inspect the immutable manifest without
opening a database:

```go title="content/config_test.go"
package content

import (
	"errors"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/schema"
)

func TestConfigResolves(t *testing.T) {
	manifest, err := ridu.Resolve(Config())
	if err != nil {
		var validation *schema.ValidationError
		if errors.As(err, &validation) {
			for _, issue := range validation.Issues {
				t.Logf("%s [%s]: %s", issue.Path, issue.Code, issue.Message)
			}
		}
		t.Fatal(err)
	}

	if manifest.Snapshot().Application.Name != "Acme Editorial" {
		t.Fatal("resolved the wrong application config")
	}
}
```

Schema validation errors carry a stable `Code`, exact `Path`, and actionable `Message`. One pass
can report several independent issues, which is more useful than fixing a large config one failure
at a time. Runtime-only capability checks—such as requiring `store.AuthStore` for auth collections
or object storage for uploads—run when the application is bound to its backends.

## Develop and review generated contracts {#generation-workflow}

While `ridu dev` is running, saving a config change automatically resolves it, regenerates every
derived contract, synchronizes safe additive database changes, and restarts the application. Before
committing, verify that the generated files are current:

```bash title="terminal"
npm run ridu -- generate --check
npm run ridu -- check
```

If the development loop is not running, `npm run ridu -- generate` performs the same generation as
a one-shot command. It resolves the Go config once, then atomically updates the canonical manifest,
OpenAPI document, generated Go models and handles, application-bound TypeScript client, and static
admin plugin registry. `ridu check` fails when committed generated output has drifted.

```text title="Configuration flow"
content/*.go
    ↓ Config()
ridu.Resolve + plugin transforms
    ↓ immutable manifest
schema · OpenAPI · Go types · TypeScript SDK · admin forms
```

For a database change, generation describes the desired application but does not silently define a
production migration. Create and review an immutable migration artifact separately:

```bash title="terminal"
npm run ridu -- migrate create --name add-post-summary
npm run ridu -- migrate verify
```

Apply that reviewed artifact with `migrate up` during deployment. `ridu dev` handles local schema
sync.

See [Ridu CLI](/docs/cli/) for the development and release commands.
