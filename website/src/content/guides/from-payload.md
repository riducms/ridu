---
title: 'Move from Payload'
description: 'Compare Payload and Ridu configuration, rewrite access rules and admin components, and plan your content migration.'
product: guides
eyebrow: 'Guide'
order: 10
aliases:
  [
    'Payload migration',
    'Payload CMS alternative',
    'import Payload',
    'Payload comparison'
  ]
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

If you know Payload, the core ideas will feel familiar: collections, globals, fields, access
rules, hooks, and custom admin components. In Ridu, you define the content model and server code
in Go, write admin components in Svelte, and use a generated TypeScript client from your frontend.

Start by comparing a collection below. If you already have content to move, continue to
[planning the migration](#fit) and [exporting documents](#normalized-export).

## Compare a collection {#configuration}

These configurations define the same post fields and access rules. Both require a title, a unique
slug, and an author; the summary is optional. Anyone can read a post, and any signed-in user can
create, update, or delete one.

Choose **Payload** or **Ridu** to compare the complete files. Both examples assume an existing
`users` collection with authentication enabled. Add `Posts` to the application's `collections`
array in Payload or `Collections` list in Ridu.

```ts title="src/collections/Posts.ts" group="posts-config" tab="Payload" focus={15-18,21-25,30-33}
import type { Access, CollectionConfig } from 'payload';

const signedIn: Access = ({ req: { user } }) => Boolean(user);

export const Posts: CollectionConfig = {
	slug: 'posts',
	// Match Ridu's default: document locking is off.
	lockDocuments: false,
	admin: {
		useAsTitle: 'title',
		defaultColumns: ['title', 'author', 'updatedAt']
	},
	fields: [
		{
			name: 'title',
			type: 'text',
			required: true,
			maxLength: 120
		},
		{
			name: 'slug',
			type: 'text',
			required: true,
			unique: true,
			index: true
		},
		{ name: 'summary', type: 'textarea' },
		// Store the user's ID. Ownership permissions come later.
		{
			name: 'author',
			type: 'relationship',
			relationTo: 'users',
			required: true
		}
	],
	access: {
		read: () => true,
		create: signedIn,
		update: signedIn,
		delete: signedIn
	}
};
```

```go title="content/posts.go" group="posts-config" tab="Ridu" focus={24-28}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

func signedIn(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
	// Actor is nil when no user is signed in.
	if ctx.Actor == nil {
		// Denying access is a decision; nil means no execution error.
		return ridu.Deny(), nil
	}
	return ridu.Allow(), nil
}

var Posts = ridu.Collection{
	Slug: "posts",
	Admin: ridu.CollectionAdmin{
		UseAsTitle:     "title",
		DefaultColumns: []string{"title", "author", "updatedAt"},
	},
	Fields: field.Fields{
		field.Text("title").Required().MaxLength(120),
		field.Text("slug").Required().Unique().Index(),
		field.Textarea("summary"),
		// Store the user's ID. Ownership permissions come later.
		field.Relationship("author", "users").Required(),
	},
	Access: ridu.CollectionAccess{
		Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
			return ridu.Allow(), nil
		},
		Create: signedIn,
		Update: signedIn,
		Delete: signedIn,
	},
}
```

Payload's field objects become Go constructors followed by options:

| In Payload                              | In Ridu                                 |
| --------------------------------------- | --------------------------------------- |
| `{ name: 'title', type: 'text' }`       | `field.Text("title")`                   |
| `required: true`                        | `.Required()`                           |
| `maxLength: 120`                        | `.MaxLength(120)`                       |
| `unique: true`                          | `.Unique()`                             |
| `relationTo: 'users'`                   | `field.Relationship("author", "users")` |
| `admin.useAsTitle`                      | `Admin.UseAsTitle`                      |
| `req.user` in a collection access rule  | `ctx.Actor`                             |
| Return `true` or `false` from that rule | Return `ridu.Allow()` or `ridu.Deny()`  |

`slug` is ordinary text in both examples: the editor supplies it. Use Ridu's
[Slug field](/docs/fields/slug/) if you want to generate it from the title. The Payload example
turns document locking off to match Ridu's default; use `LockDocuments: true` in Ridu when you
want editing locks.

Run `bun run dev` in your Ridu project after adding the collection. Open Posts in the admin and
create a post with a title, slug, and author. Saving without a title or reusing a slug should fail.
A public API request can read posts, but creating one without signing in should be rejected.

The `author` field records authorship; it does not restrict editing by itself. The next example
adds that restriction.

## Let authors edit only their own posts {#ownership}

Both frameworks let an access rule return a filter. This rule permits an operation only when the
saved post's `author` matches the signed-in user's ID:

```ts title="src/access/ownPosts.ts" group="post-access" tab="Payload" focus={5-8}
import type { Access } from 'payload';

export const ownPosts: Access = ({ req: { user } }) => {
	if (!user) return false;
	// Apply this author filter within the database operation.
	return {
		author: { equals: user.id }
	};
};
```

```go title="content/post_access.go" group="post-access" tab="Ridu" focus={16-19}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/query"
)

func ownPosts(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
	if ctx.Actor == nil {
		return ridu.Deny(), nil
	}
	author, err := query.NewPath("author")
	if err != nil {
		return ridu.Deny(), err
	}
	// Check ownership in the database operation, without a prior read.
	return ridu.Where(
		query.Equal(author, query.String(ctx.Actor.ID)),
	), nil
}
```

In the collection above, replace `signedIn` with `ownPosts` for `update` and `delete` in Payload,
or `Update` and `Delete` in Ridu. Import the function in the Payload collection file; Go files in
the same `content` package can already use it. Leave the create and read rules as they are.

Now a signed-in user can create a post, but only its current author can edit or delete it. Try
editing another user's post: the operation should be rejected. The filter is checked by the
database as part of the update or delete, so you do not need to fetch the document first.

Payload calls return values `true`, `false`, or a query object; Ridu calls the equivalent
choices `Allow`, `Deny`, and `Where`. A create rule uses allow or deny because there is no saved
document to filter yet. See [Access control](/docs/access-control/) for field permissions and
rules that depend on nearby values.

When testing these rules through Payload's Local API, pass `overrideAccess: false` and the
intended `user`; that API skips access checks by default. Ridu's local API applies access rules
and treats a nil actor as anonymous. Carry the user into local calls when moving server code.

## Find the matching Ridu feature {#feature-map}

| Payload feature                           | Start here in Ridu                                                                      |
| ----------------------------------------- | --------------------------------------------------------------------------------------- |
| `buildConfig`, collections, and globals   | [Configuration](/docs/configuration/) and [Collections and globals](/docs/collections/) |
| Field objects and reusable field helpers  | [Fields](/docs/fields/)                                                                 |
| Custom field validation                   | [Custom validation](/docs/fields/validation/)                                           |
| Collection, global, and field hooks       | [Hooks](/docs/hooks/)                                                                   |
| Custom React inputs, views, and providers | [Custom components](/docs/custom-components/): write and register Svelte components     |
| `versions` and `drafts`                   | [Drafts and versions](/docs/drafts-and-versions/)                                       |
| `@payloadcms/plugin-seo`                  | [SEO plugin](/docs/seo/)                                                                |
| `@payloadcms/plugin-form-builder`         | [Form Builder](/docs/form-builder/)                                                     |
| `payload.find(...)`                       | [Local Go API](/docs/local-api/) or [TypeScript SDK](/docs/typescript-sdk/)             |

You do not need a Go plugin just to customize the admin. Register your Svelte components in
`admin/src/admin.config.ts`. Build a [plugin](/docs/plugins/) when you need a new field type or a
reusable server extension. React components need to be rewritten in Svelte; they cannot be
imported directly into the Ridu admin.

Payload `endpoints` become Go `ridu.Endpoint` handlers on the app, a collection, or a global.
Collection URLs change from `/api/<collection-slug>/…` to
`/api/collections/<collection-slug>/…`; globals use `/api/globals/<global-slug>/…`.
Ridu supports whole path parameters such as `/:id`, but not optional segments or wildcards.
See [Custom endpoints](/docs/custom-endpoints/) when moving a handler.

## Plan the content migration {#fit}

List the content and workflows your application uses before moving data. Check the
[capability status](/docs/status/) for the features you depend on, then decide how to move each
part:

| What you have              | What to decide                                                                        |
| -------------------------- | ------------------------------------------------------------------------------------- |
| Collections and fields     | Which fields map directly, and which values need conversion?                          |
| Globals and translations   | How will you copy these with your own migration code?                                 |
| Access rules and hooks     | Which rules need rewriting, and should notifications run during import?               |
| Drafts and version history | Which revision becomes the new document, and where will old history remain available? |
| Uploaded files             | How will you copy the original files, image sizes, and matching metadata?             |
| User accounts              | How will users receive new credentials or password-reset invitations?                 |
| Custom admin components    | Which inputs and views need Svelte replacements?                                      |
| Frontend callers           | Which calls become Go local API calls, and which use the TypeScript SDK?              |

Ridu's importer accepts prepared collection records. It does not extract your Payload database or
copy globals, non-default translations, credentials, uploaded files, or a complete version history
for you. The steps below explain how to prepare the records and import them.

## Use a coding agent to help {#agent-guidance}

New projects can include the `payload-to-ridu` coding-agent skill. It guides the agent through
inventorying the Payload project, mapping its features, and migrating one complete feature at a
time. Review decisions about missing features, data loss, credentials, and production changes.

For an existing project, install it with `ridu agent install --agent codex` (or choose `claude`,
`cursor`, or `all`). After a CLI upgrade, run `ridu agent sync` to update managed guidance that
you have not edited locally.

## Export documents for Ridu {#normalized-export}

Import from a `migration/payload.Export` instead of reading Payload's database tables directly, which
can vary by version and adapter.

```go
import payloadmigration "github.com/riducms/ridu/migration/payload"

// Relationship values below are IDs, not populated user objects.
source := payloadmigration.Export{
	Collections: []payloadmigration.Collection{
		{
			Slug: "posts",
			Documents: []payloadmigration.Record{
				{
					ID: "post_01",
					Data: json.RawMessage(`{
  "title": "Hello",
  "slug": "hello",
  "author": "user_01"
}`),
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

## Check the export before importing {#assessment}

`payload.Assess` compares the export with Ridu's resolved manifest. It reports unknown collections,
missing document IDs, version selection against a non-versioned target, and a selected revision that
does not exist. Issues are sorted so repeated runs are easy to diff.

```go
manifest, err := ridu.Resolve(content.Config())
if err != nil {
	return err
}

// Inspect the export without writing any documents.
assessment := payloadmigration.Assess(manifest, source)
log.Printf(
	"collections=%d documents=%d versions=%d",
	assessment.Collections,
	assessment.Documents,
	assessment.Versions,
)
if len(assessment.Issues) > 0 {
	return fmt.Errorf(
		"migration assessment failed: %v",
		assessment.Issues,
	)
}
```

Assessment is read-only. Run it against a production-shaped export before allocating
a maintenance window. It is a structural check, not a complete data preflight: decoding, required
fields, relationships, row keys, rich-text nodes, upload keys, and hook behavior are validated only
when records enter the operation engine. A project migration should add its own read-only checks for
those shapes before cutover.

## Import the prepared documents {#import}

The normalized importer calls the local API's migration operation. Stable IDs, status, and
timestamps are preserved, while access rules, normalization, validation, hooks, transactions,
relationship checks, and version logic still run.

```go
result, err := payloadmigration.Import(
	ctx,
	app.Local(),
	source,
	// Import uses this user's permissions and runs normal hooks.
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
<strong>Choose which version to import</strong>
<p>The importer selects the base record or <code>SelectedRevision</code> and creates that as the Ridu document. It counts and validates exported versions, but it does not recreate the complete historical revision timeline. Archive old history separately if it must remain queryable.</p>
</aside>

## Copy uploaded files before importing their records {#uploads}

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
