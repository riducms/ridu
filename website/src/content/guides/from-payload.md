---
title: 'Move from Payload'
description: 'Translate a Payload project into Ridu config, normalized content, and a rehearsed production cutover.'
product: guides
eyebrow: 'Guide'
order: 10
aliases: ['Payload migration', 'Payload CMS alternative', 'import Payload', 'Payload comparison']
availability:
  status: limited
  label: 'Importer building block available'
  description: 'Ridu can assess and import prepared collection records through its operation engine. Extraction, globals, locales, value mapping, object copying, credentials, version history, and cutover remain project-specific.'
  anchor: fit
navigation:
  section: 'Get started'
  order: 60
  title: 'Move from Payload'
---

Payload and Ridu share executable configuration, collections, globals, access rules, hooks, drafts,
uploads, generated types, plugins, and an admin. Ridu defines the config and server runtime in Go,
generates a Fetch client for TypeScript, and serves the admin from the same binary.

Treat a migration as a model translation and a data migration—not a line-by-line conversion of
Payload configuration or a copy of Payload's database tables.

## Give an agent the migration contract {#agent-guidance}

New projects can include the `payload-to-ridu` coding-agent skill. Ask the agent to inventory the
Payload application, keep a source-to-target ledger, and migrate one complete feature at a time. It
must stop for decisions about missing semantics, credentials, production writes, or lossy
conversion.

For an existing project, install it with `ridu agent install --agent codex|claude|cursor|all`. After
a CLI upgrade, run `ridu agent sync`; it will not overwrite a managed file with local edits.

## Decide whether the shape fits {#fit}

Start with the workflows people rely on, not only the field list.

| Evaluate                     | Ridu equivalent                                  | Migration question                                                               |
| ---------------------------- | ------------------------------------------------ | -------------------------------------------------------------------------------- |
| Collections and globals      | `ridu.Collection` and `ridu.Global`              | Do slugs, IDs, timestamps, and singleton semantics remain stable?                |
| Fields                       | Constructors in `field`, plus plugin fields      | Which values need conversion rather than a direct JSON mapping?                  |
| Access callbacks             | allow, deny, or filtered `Where` decisions       | Can every rule be expressed without depending on a Node-only service?            |
| Hooks                        | typed Go collection, field, and global hooks     | Which side effects must move, and which should become durable tasks?             |
| Drafts and versions          | versioned resources with optional drafts         | Which revision becomes the imported document? Is old history required elsewhere? |
| Upload collections           | upload documents plus a storage backend          | How will bytes, checksums, metadata, and derived sizes be copied and verified?   |
| Admin components             | statically registered Svelte plugins             | Which custom React views or fields need a Svelte replacement?                    |
| Local API and Payload client | local Go API and generated `@riducms/sdk` client | Which callers move in-process and which remain HTTP clients?                     |

The normalized importer accepts collection records only. Payload globals, non-default
locale values, credentials, upload bytes, and complete revision timelines require custom migration
code or an archival decision.

Review the [capability status](/docs/status/) before committing to a cutover. In particular, custom
rich-text blocks, UI localization, resumable uploads, and some long-tail query operators have
narrower contracts than their Payload counterparts.

## Translate the configuration {#configuration}

The closest conceptual mappings are:

| Payload                           | Ridu                                                                     |
| --------------------------------- | ------------------------------------------------------------------------ |
| `buildConfig({...})`              | `func Config() ridu.Config`                                              |
| `CollectionConfig`                | `ridu.Collection`                                                        |
| `GlobalConfig`                    | `ridu.Global`                                                            |
| field object                      | `field.*` constructor plus typed options                                 |
| access callback                   | `ridu.AccessRule` returning allow, deny, or a query predicate            |
| lifecycle callback                | a function in `ridu.CollectionHooks` or `ridu.GlobalHooks`               |
| server plugin                     | a compiled Go `ridu.Plugin`                                              |
| admin component/import map        | a Svelte/TypeScript plugin registration                                  |
| `@payloadcms/plugin-seo`          | paired `plugins/seo` and `@riducms/plugin-seo` packages                  |
| `@payloadcms/plugin-form-builder` | paired `plugins/formbuilder` and `@riducms/plugin-form-builder` packages |
| `payload.find(...)`               | `app.Local().List(...)` or a generated typed handle                      |
| generated Payload client/types    | `generated/ridu.generated.ts`, built on `@riducms/sdk`                   |

For example, a versioned post collection becomes:

```go title="content/posts.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/richtext"
)

var Posts = ridu.Collection{
	Slug:     "posts",
	Versions: true,
	VersionConfig: ridu.VersionConfig{
		Drafts: true,
	},
	Admin: ridu.CollectionAdmin{
		UseAsTitle:     "title",
		DefaultColumns: []string{"title", "author", "updatedAt"},
	},
	Fields: []field.Definition{
		field.Text("title", field.Required()),
		field.Text("slug", field.Required(), field.Unique(), field.Index()),
		field.Relationship("author", field.To("users"), field.Required()),
		richtext.Field("content"),
	},
	Access: ridu.CollectionAccess{
		Read: postReadAccess,
	},
}
```

Start `ridu dev` early so config resolution, generation, and the local schema stay together.
`ridu check` catches invalid field paths, relationships, plugin pairing, and manifest problems
before any data is moved. The [configuration](/docs/configuration/),
[fields](/docs/fields/), [access control](/docs/access-control/), and [hooks](/docs/hooks/) guides
cover the corresponding contracts in depth.

Payload `endpoints` map to `ridu.Endpoint` values on the root config, a collection, or a global.
Move the handler to compiled Go, read complete `/:param` segments from
`EndpointContext.RouteParams`, and use `EndpointContext.Local` for access-controlled content
operations. Ridu route parameters cover complete `/:param` segments, not Payload's broader
`path-to-regexp` wildcard, optional, or partial-segment grammar. Encoded slash and backslash values do
not match, and declarations sharing one path shape across methods must use the same parameter names.
Collection URLs also change from Payload’s
`/api/<collection-slug>/…` to Ridu’s `/api/collections/<collection-slug>/…`; global endpoints use
`/api/globals/<global-slug>/…`. Like Payload, custom endpoints are anonymous unless the handler
requires an actor. See [Custom endpoints](/docs/custom-endpoints/) for the complete route and
security contract.

[SEO](/docs/seo/) provides localized metadata fields and server-side generators. [Form
Builder](/docs/form-builder/) provides reusable forms and submissions; your application supplies the
public renderer, email transport, and payment callbacks.

## Produce a normalized export {#normalized-export}

Import from a `migration/payload.Export` instead of reading Payload's database tables directly, which
can vary by version and adapter.

```go
import payloadmigration "github.com/riducms/ridu/migration/payload"

source := payloadmigration.Export{
	Collections: []payloadmigration.Collection{
		{
			Slug: "posts",
			Documents: []payloadmigration.Record{
				{
					ID:        "post_01",
					Data:      json.RawMessage(`{"title":"Hello","author":"user_01"}`),
					Status:    store.StatusPublished,
					CreatedAt: createdAt,
					UpdatedAt: updatedAt,
				},
			},
		},
	},
}
```

Write a small extractor inside the Payload project so it can use that project's exact generated
types and Payload APIs. Normalize these shapes:

- preserve document IDs and UTC creation/update timestamps;
- convert relationship objects to stable target IDs;
- carry polymorphic relationships as both target collection and ID where the Ridu field requires
  them;
- preserve array and block row identity as non-empty, unique `_key` strings;
- emit Ridu rich-text documents with `version: 1`, and convert or reject nodes outside the enabled
  feature set;
- emit a valid draft or published status;
- distinguish an absent optional value from a meaningful zero, `false`, or empty string;
- keep upload metadata and the corresponding object-copy manifest together.

Do not export Payload password hashes, sessions, API keys, or reset tokens as content. Provision Ridu
credentials independently and use a password-reset or invitation process for users.

## Assess before writing {#assessment}

`payload.Assess` compares the export with Ridu's resolved manifest. It reports unknown collections,
missing document IDs, version selection against a non-versioned target, and a selected revision that
does not exist. Issues are sorted so repeated runs are easy to diff.

```go
manifest, err := ridu.Resolve(content.Config())
if err != nil {
	return err
}

assessment := payloadmigration.Assess(manifest, source)
log.Printf(
	"collections=%d documents=%d versions=%d",
	assessment.Collections,
	assessment.Documents,
	assessment.Versions,
)
if len(assessment.Issues) > 0 {
	return fmt.Errorf("migration assessment failed: %v", assessment.Issues)
}
```

Assessment is read-only. Run it against a production-shaped export before allocating
a maintenance window. It is a structural check, not a complete data preflight: decoding, required
fields, relationships, row keys, rich-text nodes, upload keys, and hook behavior are validated only
when records enter the operation engine. A project migration should add its own read-only checks for
those shapes before cutover.

## Import through the operation engine {#import}

The normalized importer calls the local API's migration operation. Stable IDs, status, and
timestamps are preserved, while access rules, normalization, validation, hooks, transactions,
relationship checks, and version logic still run.

```go
result, err := payloadmigration.Import(
	ctx,
	app.Local(),
	source,
	migrationActor,
)
if err != nil {
	return err
}

log.Printf("imported=%d", result.Imported)
log.Printf("Payload post_01 became %s", result.IDs["posts"]["post_01"])
```

Use a dedicated actor with the permissions required for the import. A `nil` actor is not an
administrative bypass: collections whose access rules require a user will reject it.

Import referenced collections before their dependants. Because IDs are preserved, most references
need no remapping, but validation still rejects a relationship to a document that is not present.
Resolve dependency cycles in the extractor or with a custom staging pass.

<aside class="callout" data-variant="important">
<strong>Version history boundary</strong>
<p>The importer selects the base record or <code>SelectedRevision</code> and creates that as the Ridu document. It counts and validates exported versions, but it does not recreate the complete historical revision timeline. Archive old history separately if it must remain queryable.</p>
</aside>

## Stage upload bytes before their documents {#uploads}

An upload row is not a complete file migration. Ridu opens the original and configured variants
while admitting an imported upload document, so the target objects must already exist. For each
asset:

1. transform the document metadata into the target upload collection shape;
2. copy the original object and named variants into an isolated target prefix;
3. compare every staged size and checksum with the source manifest;
4. import the upload document through the operation engine;
5. exercise public and private delivery paths; and
6. remove failed-run orphans only after the configured reconciliation grace period.

Do not manufacture storage keys by string concatenation. Follow the target backend's namespace and
the [upload contract](/docs/uploads/).

## Rehearse the migration {#rehearsal}

Run the complete process more than once against a fresh target restored from production-shaped
backups. Record at least:

- document counts by collection and status;
- IDs and timestamps that failed to match;
- relationship and upload referential integrity;
- checksums for stored objects;
- selected rich-text documents rendered in the frontend;
- representative localized, draft, versioned, and access-filtered reads;
- hook side effects and tasks created during import;
- total duration and the final-delta duration.

Make the migration command fail on the first rejected document and print its collection and ID. The
importer returns completed mappings up to that point, but it is not a resumable whole-export
transaction or a mapping UI. The safest retry target is a fresh database and object prefix.

## Cut over and roll back {#cutover}

1. Take and verify database and object-store backups.
2. Put Payload into read-only mode for the final delta window.
3. Run the final normalized export, assessment, upload-object staging, document import, and
   verification.
4. Run `ridu migrate verify` and the Ridu readiness checks.
5. Switch application reads to the generated Ridu client behind a reversible flag.
6. Canary a small cohort, watching structured operation errors, audit events, and storage health.
7. Keep the Payload database and objects untouched until the rollback window closes.

Rollback means switching traffic back to the still-intact Payload deployment. Do not plan a reverse
data conversion during an incident. The [production](/docs/production/),
[migrations](/docs/migrations/), and [security](/docs/security/) guides cover the surrounding
operational controls.

## Common migration failures {#failures}

| Symptom                                    | Likely cause                                                    | What to do                                                            |
| ------------------------------------------ | --------------------------------------------------------------- | --------------------------------------------------------------------- |
| `collection ... is not present`            | The export slug has no Ridu collection                          | Add the model or exclude and archive that source collection.          |
| `selected revision ... is missing`         | The extractor named a revision it did not export                | Correct selection in the source export; do not silently fall back.    |
| Relationship validation fails              | Targets were omitted, renamed, or imported later                | Preserve IDs and import dependencies first.                           |
| `missing_row_key` or `duplicate_row_key`   | Payload row identity was discarded                              | Map row IDs to stable `_key` values before import.                    |
| Rich text is rejected                      | The document version or enabled-node set differs                | Convert to the Ridu document contract and report lossy nodes.         |
| Upload metadata imports but delivery fails | The corresponding object was not copied or namespaced correctly | Verify backend keys, object checksums, and collection storage config. |
| Access is denied                           | The migration actor cannot perform the create                   | Use a dedicated, least-privileged actor and test its rules.           |

For the exact importer types and functions, see the
[`migration/payload` API reference](/reference/migration-payload/).
