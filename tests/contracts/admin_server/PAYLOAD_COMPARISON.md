# Payload comparison fixture

> The current screen-by-screen findings live in
> [`PAYLOAD_UI_AUDIT.md`](./PAYLOAD_UI_AUDIT.md). The older browser-pass section below is retained as
> historical evidence and should not be treated as the current gap list.

Ridu's admin contract server has a self-contained Payload `3.87.0` mirror at
[`../payload_server`](../payload_server/). The mirror uses the same eight editorial collections,
accounts, roles, access scenarios, relationships, hooks, drafts, uploads, and representative seed
data. It then adds a **Comparison reference** navigation group containing a stable spread of
Payload behavior for repeatable side-by-side review.

This is deliberately behavioral. The two frameworks do not need identical REST shapes, database
tables, component APIs, or runtime architecture to provide equivalent authoring behavior.

## Run both admins

Start Ridu from this repository. Use the contract entry so application-owned field plugins are
actually bundled:

```sh
bun run dev:admin-fixture
```

Install the Payload fixture once from this repository:

```sh
bun run setup:payload-fixture
```

Then start Payload in a second terminal:

```sh
bun run dev:payload-fixture
```

Ridu opens at <http://127.0.0.1:18081/admin/login>; Payload opens at
<http://localhost:3000/admin>. Both use the same accounts and passwords:

| Role          | Email                 | Password       |
| ------------- | --------------------- | -------------- |
| Administrator | `admin@riducms.test`  | `ridu-admin`   |
| Editor        | `editor@riducms.test` | `ridu-browser` |
| Contributor   | `demo@riducms.local`  | `ridu-demo`    |

The Payload fixture matches the stable stack used by the local `payload-playground` and owns an
isolated `bun.lock`, so its pinned dependencies cannot change Ridu's frontend dependency graph. Its
generated `payload-types.ts` is committed so schema changes can be checked for drift.

## What is genuinely comparable now

| Surface             | Payload reference                                               | Ridu today                                                                                                                                                                                                 | Status                                                                           |
| ------------------- | --------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| Executable schema   | TypeScript config                                               | Go config and deterministic manifest                                                                                                                                                                       | Equivalent promise                                                               |
| Collection CRUD     | Local API, REST, GraphQL, admin                                 | Local API, REST, SDK, admin, plus an optional experimental GraphQL plugin                                                                                                                                  | Core equivalent; wider GraphQL conformance remains experimental                  |
| Collection access   | Boolean or filtered predicates, distinct admin/version rules    | `Allow`, `Deny`, or store-enforced `Where`, with distinct `Admin` and `ReadVersions` rules                                                                                                                 | Broadly equivalent across the current collection/global surfaces                 |
| Field access        | Create/read/update and response redaction                       | Create/read/update and response redaction                                                                                                                                                                  | Equivalent for current paths                                                     |
| Admin capabilities  | Permission-aware navigation, fields, and actions                | Per-request safe operation and field booleans                                                                                                                                                              | Equivalent across primary collection/global surfaces                             |
| Auth collections    | Users, cookie/JWT sessions, roles                               | Users, cookie sessions, recovery, verification, API keys, roles, force unlock                                                                                                                              | Broadly equivalent for the admin and cookie-session surface                      |
| Relationships       | Singular, many, polymorphic, joins                              | Singular, many, polymorphic, access-aware joins with configurable tables and atomic target mutations                                                                                                       | Broadly equivalent                                                               |
| Nested data         | Groups, arrays, blocks, named and unnamed tabs                  | Groups, arrays, blocks, plus named data-bearing and unnamed presentation-only tabs at root or nested layouts; separately authored tab groups keep independent selection state                              | Broadly equivalent                                                               |
| Drafts and versions | Autosave, publish, compare, restore, restore as draft, schedule | Dedicated history/detail/diff, ordinary/draft restore, autosave, publish, localized snapshots, and durable scheduling                                                                                      | Broadly equivalent; create-route autosave and scheduled unpublish differ         |
| Uploads             | Metadata, variants, bulk intake, preview, focal/crop controls   | Metadata, variants, retryable bulk intake, SSRF-safe Paste URL, named-size preview, focal/crop regeneration, and storage-safe duplication                                                                  | Broadly comparable outside excluded localization operations                      |
| Hooks               | Broad collection/field/auth/global lifecycle                    | Collection, field, auth, global, operation, error, and commit phases                                                                                                                                       | Broad lifecycle parity                                                           |
| Rich text           | Mature Lexical default feature surface                          | Lexical Svelte with matching default relationships, uploads, check/lists, links, code, rules, alignment, indentation, and inline formats                                                                   | Broadly comparable; Payload's optional extension ecosystem is deeper             |
| Form Builder        | Official reusable forms/submissions plugin                      | Paired Go/TypeScript plugin with native block authoring, dynamic server validation, confirmations, protected email templates, uploads, payments, generated contracts, and application-owned render helpers | Broad workflow parity; spam controls remain explicit application work            |
| Generated contracts | Payload TypeScript types and their SDK default                  | Manifest, exact output/create/update/query contracts, OpenAPI, and an automatically configured Fetch SDK                                                                                                   | Equivalent automatic SDK ergonomics; Ridu keeps stronger input/output separation |
| Production runtime  | Next.js/Node plus adapter                                       | One embedded Go binary                                                                                                                                                                                     | Intentionally different; Ridu advantage                                          |

## Reference surfaces exposed directly in the Payload admin

Open **Comparison reference → Payload reference surface** and **Site settings (reference)**. They
keep a broad, stable set of Payload controls inspectable while the current gap assessment evolves.

| Payload behavior in the mirror                                                                       | Ridu state                                                                                                                                                                                                                                                                                                                            |
| ---------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Real navigation groups, collection descriptions, title fields, default columns, sort/search metadata | Grouped navigation/dashboard resources, descriptions, actions, title/default columns, access-checked relationship labels, independently selectable system columns, nested-group columns/sorts, nested repeated-field filters, page sizes, and persisted workspaces are implemented                                                    |
| Globals with their own admin route, drafts, hooks, access, and generated type                        | Implemented in the Ridu fixture, including dedicated access-redacted version history/detail/diff                                                                                                                                                                                                                                      |
| Localized fields, fallback behavior, locale-aware versions, and admin locale switcher                | Implemented for collection/global authoring, reads, version snapshots, fallback, all-locale access, copy-to-locale, and one persisted header switcher across authenticated routes; interface translation, locale-aware number/date formatting, timezones, and RTL shell layout are also implemented                                   |
| Code, radio, point, UI-only, and collapsible fields                                                  | Implemented end to end, including generated contracts and presentation-only storage omission                                                                                                                                                                                                                                          |
| Array `minRows`/`maxRows`, duplicate, collapse, and custom row labels                                | Implemented with server/client validation, per-row and all-row disclosure controls, legacy child-path labels, and paired-plugin Svelte row-label components for arrays and blocks                                                                                                                                                       |
| Inverse `join` fields                                                                                | Implemented as access-aware output with generated types, configured columns/default sorting, independently sortable linked rows, and create/choose/attach/detach controls that apply explicit deltas through one engine transaction while retaining the ordinary target update pipeline                                               |
| Virtual/computed fields                                                                              | Implemented with executable Go resolvers, exact runtime validation, generated output types, and read-only admin rendering                                                                                                                                                                                                             |
| Document locking, takeover, and presence semantics                                                   | Persisted expiring leases, owner presence, read-only inspection, heartbeats, release, and explicit takeover are implemented; takeover has its own `CollectionAccess.Unlock` rule and defaults coherently to update access                                                                                                             |
| Trash, restore, and permanent delete                                                                 | Implemented across storage, local API, REST, SDK, and admin with atomic selected restore/permanent delete and a confirmed collection-wide Empty trash action                                                                                                                                                                          |
| Relationship option filters derived from document/sibling data                                       | Implemented as combinable typed predicates over direct or nested group paths, optionally scoped per polymorphic target, in quick pickers and the full browser; presentation only, never authorization                                                                                                                                 |
| Conditional fields driven by document and sibling/row data                                           | Implemented as deterministic typed `all`/`any`/`not` expressions over strict scalar `equals`/`notEquals`/`oneOf` leaves with explicit document or current-parent/row scope; hidden values remain intact and visibility is never authorization                                                                                         |
| GraphQL and GraphQL-specific depth/complexity protections                                            | Available through the opt-in GraphQL plugin with generated SDL, shared-engine execution, depth/complexity limits, PostgreSQL CRUD, localization, filters, and population; broader pinned Payload conformance remains experimental                                                                                                     |
| Custom dashboard/routes/views, document actions, list cells, and broad admin extension points        | Typed static plugins cover dashboard, auth/account, navigation, collection list/create/edit, globals, and not-found composition/replacement; routes, logout, graphics/avatar, header/actions/settings, provider wrappers, document views/actions, and list cells; exclusive surfaces are collision checked.                           |
| Live preview, preview breakpoints, locale/draft handshake                                            | URL templates, breakpoints, dimensions, zoom, locale/draft context, and iframe/popup streaming use an origin-, source-, and ephemeral-channel-authenticated ready/reconnect handshake. Five-minute resource-scoped capabilities authorize access-checked server-rendered collection/global drafts without exposing the admin session. |
| Bulk edit/publish/unpublish/delete and collection duplication                                        | Implemented atomically through the operation engine, REST, SDK, and admin; upload duplication copies the original and every generated size to independent object keys                                                                                                                                                                 |
| Folders, saved presets, and hierarchy views                                                          | Implemented through collection admin metadata, server-backed user preferences, configured folder filters, and self-parent hierarchy views                                                                                                                                                                                             |
| Auth-user creation, recovery, verification, unlock, API keys, and pluggable strategies               | Full and inline user creation store confirmed credentials atomically; recovery, verification, force unlock, API keys, account security, profile editing, theme, preference reset, and strategies are implemented                                                                                                                      |
| Full hook matrix across auth, globals, delete, read, duplicate, errors, and operations               | Implemented with distinct collection/global/field read, change, delete, duplicate, operation, commit, resource-error, and root-error phases plus auth login/me/refresh/logout/recovery/verification/API-key hooks                                                                                                                     |

## Important differences that are not automatically Ridu defects

Payload's React/Next.js component model, exact REST response shapes, GraphQL as a mandatory
transport, and database layout are implementation choices rather than parity requirements. Ridu
should copy the useful authoring and extension promises while preserving its Go source of truth,
store-enforced authorization, generated Fetch SDK, static Svelte admin, and single-binary runtime.

The comparison fixture should evolve by adding a behavior to Payload first, then either expressing
the same behavior in Ridu or leaving it visibly unmatched. Do not weaken the Payload config to make
the table look better, and do not fake unsupported Ridu behavior in the example.

## Browser audit: Payload 3 versus Ridu

This section is the earlier browser pass. See [`PAYLOAD_UI_AUDIT.md`](./PAYLOAD_UI_AUDIT.md) for the
post-implementation re-audit and current priorities.

This section records the screen-by-screen browser pass performed on 13 August 2026. The matched
comparison used this fixture's Payload `3.87.0` admin at <http://localhost:3000/admin> and Ridu's
contract admin at <http://127.0.0.1:18081/admin>. Payload's upstream `3.x` branch at `3.88.0` was also
run separately to confirm the dashboard, list, document, global, account, and authentication UI are
representative of Payload 3 rather than a canary build.

The audit used the administrator and editor accounts. It visited the dashboard, every collection
list, every collection create screen, a seeded edit screen for every collection, the Payload-only
collection, the Payload-only global, version history and revision detail, trash, account, login, and
permission-sensitive documents. Controls were opened far enough to inspect columns, filters, bulk
selection, tabs, arrays, blocks, uploads, relationships, and document actions.

On 24 August 2026, the comparison fixture also gained Payload's official `@payloadcms/plugin-seo`
and Ridu's paired `plugins/seo` implementation on Pages and Site settings. Both fixtures expose the
same overview, localized meta title/description/image, search preview, tabbed layout, and draft-aware
title/description/image/URL generators. Ridu deliberately adds authenticated, resource-authorized,
size-bounded generation endpoints plus cancellation and stale-response suppression in the admin;
these harden the interaction without changing the generated-value authoring result.

On 27 August 2026, both fixtures gained Payload's official Form Builder workflow. Each exposes a
seeded Contact Form and submission, block-based field authoring, confirmation behavior, email
templates, uploads, and the same default/opt-in field selection. Ridu additionally exercises its
compiled payment callback, transaction-bound selected-form validation, protected notification
configuration, generated REST/SDK contracts, and framework-neutral public rendering helpers.

### Shell, dashboard, authentication, and account

| Screen           | Payload 3 behavior observed                                                                               | Ridu behavior observed                                                                                                                                                                                                                      | Gap                                                                                            |
| ---------------- | --------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| Dashboard        | Grouped collection/global cards with direct create and list links                                         | Permission-filtered grouped collection/global cards with descriptions and direct Show all, Create new, and Edit global links, alongside typed plugin panels                                                                                 | Broadly comparable; Ridu retains its own visual language                                       |
| Navigation       | Collapsible named groups, separate collection labels, globals, descriptions, breadcrumbs                  | Manifest-driven native collapsible groups combine collections and globals without mutating resource labels; document headers expose an accessible Dashboard/resource/document breadcrumb                                                    | Broadly comparable                                                                             |
| Locale chrome    | Locale switcher is present across dashboard, lists, documents, globals, and account                       | One persisted content-locale switcher in the authenticated shell header across dashboard, lists, documents, globals, versions, account, and plugin routes; inherited-value provenance, RTL authoring direction, and explicit copy-to-locale | Closed for the inspected surface; interface language remains an intentionally separate control |
| Login            | Login plus a Forgot password route                                                                        | Login, password reveal, Forgot password, reset, and verification routes                                                                                                                                                                     | Broadly comparable; email delivery is application-owned                                        |
| Account          | Dedicated account route with email, change password, force unlock, language, theme, and reset preferences | Manifest/access-driven profile route, persisted system/light/dark theme, content locale, interface language, timezone, reset preferences, security/session/API-key route, and force unlock on auth users                                    | Broadly comparable through Ridu's separate content-locale and interface-language controls      |
| Admin extensions | Payload can replace or add dashboard, list, document, account, route, and action surfaces                 | Static plugins provide typed wrappers/replacements for core views plus routes, navigation, logout, graphics/avatar, header/actions/settings, provider wrappers, list cells, document actions, and document views                            | Broadly comparable through Ridu's static Svelte plugin model                                   |

### Collection list screens

The list vertical is now a credible workspace in Ridu: every collection has search, selectable rows,
document links, counts, pagination, schema-driven columns, server-side sorting and filtering, page
sizes, and persisted named views; versioned collections add Draft/Published status tabs. Ridu now
hydrates relationship and upload cells through access-checked target reads, exposes independent ID,
Created, Updated, and Status columns, and mirrors Payload's group-leaf columns, nested
group/array/block filters, and two-stage selection: select the visible page, then explicitly freeze
every result matching the current query.

| Capability opened in the browser | Payload 3                                                                                                       | Ridu                                                                                                                                                                                                                     |
| -------------------------------- | --------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Search                           | Uses collection `useAsTitle`/search metadata and labels the query accordingly                                   | Uses the resolved title field's admin label for the accessible query label and placeholder                                                                                                                               |
| Columns                          | Eligible scalar, group-leaf, relationship, upload, structural, metadata, and system fields; preferences persist | Manifest-driven top-level/structural and group-leaf fields, access-checked relationship/upload labels, computed/join output, custom cells, and independent ID/Created/Updated/Status; server preferences persist choices |
| Sorting                          | Ascending/descending controls on sortable columns and configured default sort                                   | URL-addressable sorting on eligible title, top-level scalar, and nested group-leaf columns; repeated/structured paths stay unsortable                                                                                    |
| Filtering                        | Field/operator/value filter builder across the schema                                                           | Typed group, array-row, and block-row leaf filters plus top-level/workflow/folder predicates, compiled by both in-memory and PostgreSQL stores                                                                           |
| Page size                        | 5/10/25/50/100 selector                                                                                         | 10/25/50/100 selector, persisted in workspace preferences and named views                                                                                                                                                |
| Selection                        | Select current page or all matches; bulk Edit, Publish, Unpublish, and Delete                                   | Select the current page or resolve up to 100 filtered/read-visible matches in one server request, then atomically Edit, Publish, Unpublish, Delete, Restore, or permanently delete; larger sets require narrower filters |
| Trash                            | Collection trash route, empty-trash action, selected restore/permanent-delete workflow                          | Dedicated route with per-row and atomic selected restore/permanent delete, bulk active-to-trash, and confirmed collection-wide Empty trash                                                                               |
| Media list                       | Bulk Upload in addition to ordinary create                                                                      | Permission-aware bulk queue with per-file metadata, row status, retry, and direct links to completed assets                                                                                                              |
| Saved/hierarchical views         | Payload supports preferences, presets, folders, and hierarchy-oriented views                                    | Server-backed named views capture query, workflow, folder, hierarchy, columns, sorting, filters, and page size                                                                                                           |

### Collection create and edit screens

| Collection/screen         | What Payload exposes beyond the comparable Ridu screen                                                                                                                                                                                                                                                                                                  |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Users                     | Language/locale operations remain outside the current scope. Ridu now matches confirmed-password creation in full-page and inline relationship flows, plus account password changes, access-controlled Force Unlock, and permission-aware document actions.                                                                                             |
| Media                     | Locale operations remain outside scope. Ridu handles single and retryable bulk upload, SSRF-safe remote ingestion, named-size preview, persisted drag/tune crop rectangles, focal-aware regeneration, inline create/select/edit drawers, metadata, relationships, tags, download, deletion, and storage-safe duplication with independent object keys.  |
| Categories                | Ridu's inverse Related Posts table now mirrors Payload's configured columns, local ascending/descending controls, linked rows, inline create, choose-existing, attach, and detach workflow. One atomic delta request runs every changed target through ordinary access, validation, hooks, versions, and conflict protection.                           |
| Posts                     | Locale copy remains excluded. Ridu has relationship and upload browse/create/inspect/edit/select drawers, nested field/row copy-paste, plus dedicated paginated Versions, field-aware comparison, restore, and scheduled publishing.                                                                                                                    |
| Pages                     | Locale operations remain excluded. Ridu supports dedicated version history/diff plus drag, move, add-below, duplicate, remove, collapse/show-all, compatible nested field/row copy-paste, nested rich text, uploads, and relationships.                                                                                                                 |
| Events                    | Locale operations remain excluded. Ridu now carries Payload-familiar `dayOnly`, `dayAndTime`, and `timeOnly` picker metadata through Go config and the manifest, round-trips local date-time controls as RFC 3339, formats list cells with time-of-day, and preserves nested row controls.                                                              |
| Editorial notes           | Payload removes the protected Confidential Details field entirely for the editor role. Ridu now evaluates field capabilities for the concrete document and omits the protected field from rendering, validation, and submission.                                                                                                                        |
| Redirects                 | Payload removes Delete for editors when collection access denies it. Ridu now evaluates document-specific operation capabilities and removes the denied destructive action while preserving allowed actions such as Duplicate.                                                                                                                          |
| Forms and submissions     | Payload and Ridu expose native Forms/Form Submissions lists and document screens. Ridu's public create path reloads the selected form and rejects missing, unknown, duplicate, ill-typed, unavailable-choice, and invalid-upload values with field-addressable `422` issues; authenticated authors can inspect submissions while updates remain denied. |
| Payload reference surface | Ridu renders radio, point, code, UI-only content, virtual/computed values, collapsible layout, bounded arrays with custom labels/collapse/duplicate behavior, and real collection trash workflows. Localized text and rich text are exercised on Posts.                                                                                                 |

### Globals and versions

Payload's Site settings global has its own route, draft/publish controls, localized announcement,
array editing, relationships, hooks, and API view. Ridu now matches the singleton route, localized
fields, array/relationship editing, read/update access, lifecycle hooks, draft/publish/restore,
stable API URL, and generated Go/TypeScript contracts. Ridu's version detail and comparison
workspace also carries the selected locale across reads and restores.

Payload's version history is a dedicated paginated route. Ridu now has the same core workspace:
dedicated collection/global history and revision URLs, pagination, readable field-aware comparison,
a Modified fields only toggle, arbitrary revision selection, ordinary restore, and restore as draft.
Collection history also lists, creates, and cancels durable scheduled publishes. Separate
collection/global `ReadVersions` rules protect snapshots independently from current-document reads;
locale selection and fallback apply to version list, detail, comparison, and restore.

### Access-control finding

Both servers enforce collection predicates and field redaction. The editor pass confirmed that
Payload also translates those permissions into the form: protected fields and denied destructive
actions disappear. Ridu now does the same through per-request capability endpoints that expose only
evaluated booleans, never rules or predicates. Collection/global navigation, create controls,
document actions, custom actions, list bulk actions, trash controls, field rendering, validation,
and submission all consume those capabilities. Filtered access is still checked as a predicate in
the atomic store query, so the presentation layer does not become an authorization boundary.

### Prioritized implementation backlog from the browser pass

1. **Document operations and collaboration:** persisted expiring locks, owner presence, read-only
   inspection, release/heartbeat, and access-controlled takeover now complement optimistic revisions.
   Dedicated version list/detail/diff, ordinary/draft restore, scheduled publish, duplication,
   per-document trash, atomic selected trash restore/permanent delete, and empty trash are implemented.
2. **Asset and relationship workflows:** relationship and upload browse/create/inspect/edit/select
   drawers, storage-safe blob duplication, retryable bulk upload, SSRF-safe Paste URL, persisted
   freeform crop/focal regeneration, named-size previews, and schema-compatible nested field/row
   clipboard actions are implemented.
3. **Authentication and account:** confirmed-password creation in full and inline user authoring,
   profile editing, system/light/dark theme, reset-all-preferences, recovery, verification,
   access-controlled force unlock, API keys, password change, and active-session management are
   implemented. Admin interface translation remains separate from content localization.
4. **List depth:** relationship/upload cells resolve access-checked target labels and
   ID/Created/Updated/Status columns are independently selectable and persisted. Group-leaf
   columns/sorts, array/block summaries, nested group/array/block filters, persisted workspaces,
   named views, two-stage select-all, and atomic operations are implemented. Inverse joins also own
   their configured columns and sorting controls.
5. **Localization:** locale-aware scalar, nested, relationship, upload, and rich-text fields;
   query/sort/unique behavior; versions; explicit fallback provenance; RTL authoring; all-locale
   output; and copy/duplicate locale actions are implemented across collections and globals.
6. **Field/model coverage:** Date picker appearances, configurable/mutable Join tables,
   virtual/computed, combinable nested and polymorphic-target relationship option filters, radio,
   point, code, UI-only, collapsible, bounded-array, localized placeholder, static hidden, and
   responsive root-sidebar contracts are now represented in the comparison fixture.
7. **Authoring ergonomics:** manifest-driven live preview, configured breakpoints, collection/global
   navigation, dashboard groups, nested field/row clipboard actions, named data-bearing tabs,
   presentation-only tabs, and the packaged authenticated cross-origin draft helper are implemented.

### Launch issue found during the audit

The first Ridu pass served the generic embedded admin and produced `No admin field plugin can render
... (plugin:richtext)` on Posts and Pages. The framework bundle was behaving as designed: it cannot
contain application-owned plugins. Rebuilding and serving the contract entry through
`RIDU_BROWSER_ADMIN_DIR=.ridu/admin-fixture-build` fixed the affected screens and restored seeded
titles, nested rich text, blocks, relationships, uploads, and revision history. The new
`bun run dev:admin-fixture` script makes the correct launch mode the obvious default for future
side-by-side audits.
