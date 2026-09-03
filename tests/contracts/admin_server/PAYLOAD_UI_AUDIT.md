# Payload 3 versus Ridu admin UI audit

This is the current screen-by-screen comparison of the Payload `3.87.0` fixture at
<http://localhost:3000/admin> and the Ridu contract fixture at
<http://127.0.0.1:18081/admin>. It was performed in the Codex in-app Browser on 13 August 2026.
It supersedes the older browser-pass notes embedded in
[`PAYLOAD_COMPARISON.md`](./PAYLOAD_COMPARISON.md).

The pass used both administrator and editor accounts. It inspected the rendered DOM, opened the
relevant menus and controls, and checked browser warnings and errors. A focused follow-up on 14
August added Payload and Ridu live-preview fixtures and exercised their split panes, viewport
controls, unsaved draft streaming, and locking/takeover states. It visited both dashboards;
every collection list; every collection create screen; one seeded edit screen per collection; the
global; account, login, and recovery screens; list columns, filters, selection, trash, versions,
version detail, API output, and Ridu's static plugin route and custom document view.

## Executive result

Ridu now has a credible core authoring vertical and several real advantages: a compact responsive
shell, exact generated input/output contracts, configured folders and hierarchy, server-backed
saved views, atomic bulk actions, static typed extension points, joins and computed fields, and a
single-binary runtime. The browser pass found no console failures in the stable Payload screens and
no remaining console failures in Ridu after fixing the saved-view naming bug described below.

Outside the explicitly excluded localization and GraphQL work, the inspected Payload 3 authoring
surfaces are now broadly covered. Permission-aware presentation was the highest-risk finding in the original pass and is now closed as described below. The original
list-workspace gap is also substantially closed: Ridu now has schema-driven persisted columns,
sorting, general filters, selectable page sizes, and named views that capture the whole workspace.
Nested list paths and all-filtered-results are now substantially closed. Ridu mirrors Payload's
group-leaf columns, nested group/array/block filters, and visible-page selection followed by an
explicit Select all promotion. Relationship and upload cells now resolve access-checked target
labels, while ID, Created, Updated, and Status are independently selectable persisted columns.

The shell follow-up is now closed too. Ridu consumes collection and global `admin.group` metadata in
one shared grouping model, renders native collapsible sidebar sections, and presents the same
permission-filtered groups on the dashboard with descriptions and direct actions.

1. **Document collaboration and operations.** Dedicated version list/detail/compare routes,
   modified-only controls, restore-as-draft, durable scheduled publishing, and stable SDK/REST
   routes, manifest-driven live preview, and scoped server-rendered draft authorization are now
   implemented. Content localization is now included; realtime presence beyond Payload's inspected
   exclusive edit lease is optional future collaboration work.
2. **Asset and relationship authoring.** Ridu now exposes browse/create/inspect/edit/select drawers,
   permission-aware bulk queues, per-file metadata/retry, SSRF-safe Paste URL, named-size previews,
   persisted freeform crop/focal regeneration, and storage-safe duplication with independent object
   keys, plus compatible nested field/row clipboard actions with fresh stable row identities.
3. **Account and localization settings.** Ridu now combines atomic credential-backed user creation,
   manifest/access-driven profile editing, persisted system/light/dark theme, reset preferences,
   password, active sessions, recovery, optional API keys, and access-controlled force unlock.
   One content-locale switcher persists in the authenticated header across every route. Admin
   interface language and timezone remain separate author preferences.

## Shell, dashboard, authentication, and account

| Screen               | Payload 3 observed                                                                       | Ridu observed                                                                                                                                                                                                             | Remaining Ridu gap                                                                   |
| -------------------- | ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| Dashboard            | Named groups; each card has direct Create and Show all links; locale control             | Permission-filtered named groups, descriptions, collection/global cards, direct Show all/Create new/Edit global links, typed plugin panels, and the global persisted content-locale header control                         | Grouping and content-locale behavior are broadly comparable                           |
| Navigation           | Collapsible named groups, collections and globals in the configured group, breadcrumbs   | Native collapsible manifest groups combine collections and globals while keeping resource labels independent; document headers expose an accessible Dashboard → resource → document breadcrumb                                                                                         | Broadly comparable                                                                   |
| Login                | Email/password and Forgot password                                                       | Email/password, password reveal, and Forgot password when recovery is configured                                                                                                                                          | Broadly comparable; Ridu retains its visual language                                |
| Forgot password      | Dedicated email form and return link                                                     | Dedicated email form and return link                                                                                                                                                                                      | Broadly comparable                                                                   |
| Account              | Editable user profile, Change Password, Force Unlock, language, theme, Reset Preferences | `/admin/account` has schema/access-driven profile editing, interface language, timezone, persisted theme/content locale and reset; `/admin/account/security` has password, sessions, sign-out-all, and API keys; auth-user edits expose access-controlled Force unlock | Broadly comparable through separate interface- and content-language controls          |
| Dashboard extensions | Payload can replace/add broad dashboard and core admin surfaces                          | Typed static core-view composition/replacement plus routes, logout, graphics/avatar, header/actions/settings, provider wrappers, document views/actions, and list cells                                                        | Broadly comparable through Ridu's static Svelte plugin model                         |
| Global navigation    | Global sits in its configured Payload navigation group                                   | Globals author their own group and description and render in the matching sidebar/dashboard section                                                                                                                       | Closed for the inspected surface                                                     |

Ridu's account menu now links to both `/admin/account` and `/admin/account/security`. The profile
route reuses the generated collection manifest, field plugins, validation, and per-document field
access rather than maintaining a second user schema. Preferences are scoped to the authenticated
admin identity and reset atomically through the official stores.

## Collection lists

Every inspected list in both products rendered searchable rows, create navigation, selection, and
pagination without console errors. Payload's current fixture lists Users, Media, Posts, Categories,
Pages, Events, Editorial notes, Redirects, and the Payload reference surface. Ridu lists those equivalent
surfaces plus the configured Folders collection.

| Capability      | Payload 3 observed                                                                           | Ridu observed                                                                                                        | Verdict                                                                             |
| --------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| Search          | Labelled from `useAsTitle`, for example `Search by Title`                                    | Labelled from the resolved title field's admin label, for example `Search by Title`                                  | Broadly comparable                                                                  |
| Default columns | Configured columns per collection                                                            | Manifest `defaultColumns`, `useAsTitle`, relationship labels, status, updated time, and registered custom list cells | Broad parity for top-level fields                                                   |
| Column chooser  | Scalar, group-leaf, relationship, upload, structural, metadata, and system fields            | Top-level/structural and group-leaf fields plus ID, Created, Updated, Status, computed/join output, and custom cells | Broad parity; repeated structures use bounded summaries                             |
| Sorting         | Ascending/descending buttons on sortable scalar paths                                         | URL-backed sorting on eligible title, top-level scalar, and nested group-leaf columns                                | Broad parity; repeated structures are intentionally summarized rather than sortable |
| Filters         | General schema-driven Add Filter builder                                                     | Typed top-level, group, array-row, block-row, relationship, workflow, and folder filters, all server-applied         | Broad parity for declared paths                                                      |
| Page size       | 10/25/50/100-style selector                                                                  | 10/25/50/100 selector backed by the SDK list limit                                                                   | Broad parity                                                                        |
| Saved views     | Payload persists list preferences; the fixture does not expose a named-preset control        | Server-backed named views preserve query, workflow, folder, hierarchy, columns, sort, filters, and page size         | Ridu has a broader explicit named-view surface in this fixture                      |
| Folders         | Not configured in this Payload mirror, so no honest on-screen comparison was possible        | Explicit Folder collection, Manage folders link, Post folder filter, and persisted selection                         | Ridu fixture now proves the full configured path                                    |
| Hierarchy       | Not configured in this Payload mirror                                                        | List/Hierarchy switch on Folders and Posts, with parent-indented rows                                                | Ridu fixture now proves the path                                                    |
| Selection       | Select the visible page, promote to all filtered matches, then Edit/Publish/Unpublish/Delete | The same two-stage workflow up to Ridu's 100-document atomic bound; one server request freezes read-visible IDs and full capabilities before bulk actions | Broad parity with an intentional bounded/all-or-nothing difference                  |
| Trash           | Active/Trash switch, selected Restore/Delete, and empty-trash action                         | Dedicated trash route with per-row and atomic selected restore/permanent delete plus confirmed Empty trash           | Broadly comparable for the inspected surface                                        |
| Media list      | Bulk Upload and Paste URL                                                                    | New asset, SSRF-safe Paste URL, and a permission-aware bulk queue with metadata, retry, and completed links          | Broad parity for the inspected intake surface                                       |

### Per-list observations

| Collection                | Payload-specific list behavior not matched by Ridu                                                                 |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| Users                     | Access-checked relationship labels render through each target collection's configured title field                  |
| Media                     | Upload cells resolve asset labels; bulk local-file intake is comparable                                            |
| Posts                     | Relationship labels and independently selectable ID/Created/Updated/Status metadata are persisted                  |
| Categories                | Related Posts owns configured columns, sorting, links, inline create, choose-existing, attach, and detach controls |
| Pages                     | Block roots summarize in columns and block leaves filter through typed server predicates                          |
| Events                    | Day-only, date-time, and time-only metadata drive matching inputs; list cells and filters retain time-of-day       |
| Editorial notes           | Protected fields correctly disappear when field read access denies them                                            |
| Redirects                 | Top-level fields are configurable/sortable/filterable; document delete remains access-aware                        |
| Payload reference surface | Point/code fields can be columns but deliberately lack generic sort/filter operators                               |

## Create and edit screens

The following routes were opened for both the create and seeded-edit state of every comparable
collection. Payload's draft-enabled create routes immediately autosaved blank draft records during
the audit; Ridu's create routes remained local until Save.

| Collection          | Payload 3 observed                                                                                                                   | Ridu observed                                                                                                                                                                                                                                 | Main Ridu shortfall                                        |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------- |
| Users               | Email, new/confirm password on create; Change Password and Force Unlock on edit; API route                                           | Full-page and inline create flows require confirmed credentials and create the document/password atomically; identity/profile/security tabs, account-security password flow, Force unlock, and locale-aware profile-field creation                                 | Admin interface translation remains separate             |
| Media               | Select file, Paste URL, Bulk Upload from list, Preview Sizes, Edit Image, create/select drawers                                      | File drop/select, inline browse/create/edit drawers, retryable bulk queue, per-file metadata, SSRF-safe Paste URL, named-size preview, persisted crop/focal regeneration, storage-safe duplicate, relationships, tags, download, trash/delete | Broad parity outside localization                          |
| Categories          | Editable inverse join table with its own columns, sorting, Add new, Create New, Choose existing                                      | Configured inverse Posts table with sortable columns, links, inline create/choose-existing, attach/detach, access-checked target writes, and computed label                                                                                   | Broad parity for the inspected join surface                |
| Posts               | Dedicated Versions and API routes, scheduled publish affordance, create/edit drawers, localization, rich field actions, live preview | Dedicated history/detail/diff and scheduling, publish/unpublish, autosave, duplicate hooks, relationship/upload drawers, localized text/rich text with fallback provenance and copy actions, nested field/row clipboard, groups/tabs, static action/Insights view, and live preview | Broad parity for the inspected surface                    |
| Pages               | Dedicated history/API routes and block clipboard/add-below actions                                                                   | Dedicated history/detail/diff, drag/reorder, add-below, collapse all, duplicate/remove, schema-compatible field/row copy-paste, nested rich text/uploads/relationships                                                                        | Broad parity outside localization                          |
| Events              | Date-time authoring with time of day, richer row controls                                                                            | Date-time start/end and nested schedule inputs round-trip local values through RFC 3339; list/filter presentation retains time-of-day; bounded rows support drag/move/add-below/duplicate/copy/paste/collapse                                 | Broad parity outside localization                          |
| Editorial notes     | Permission-aware protected-field removal for editor                                                                                  | Protected field is omitted from rendering, validation, and submission                                                                                                                                                                         | Closed for the inspected surface                           |
| Redirects           | Delete action disappears for editor when delete access denies                                                                        | Delete is omitted while allowed Duplicate remains visible                                                                                                                                                                                     | Closed for the inspected surface                           |
| Capability showcase | Localized text, radio, point, code, UI-only component, collapsible group, computed field, bounded rows, trash                        | Radio, point, code, UI-only, collapsible, computed/virtual, bounded rows, custom row labels, duplicate/collapse, generated contracts, with localization exercised by the Posts and global fixtures                                             | Broad parity across the combined fixture                   |
| Folders             | Not configured in this Payload mirror                                                                                                | Name, self-parent relationship, hierarchy list, duplicate, and CRUD                                                                                                                                                                           | No direct screen comparison in the current mirror          |

## Globals, versions, API views, trash, and extensions

| Screen               | Payload 3 observed                                                                                                                             | Ridu observed                                                                                                                                              | Gap                                                                                                     |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| Site settings global | Dedicated route, Save Draft/Publish, Versions and API routes, localized announcement, array/relationship editor                                | Dedicated route and locale-aware version history/detail/diff, publish/unpublish, restore, stable API route, localized announcement, array/relationship editor | Broad parity                                                                                            |
| Version list         | Paginated dedicated route with revision ID and status                                                                                          | Dedicated paginated collection/global route plus inline document summary                                                                                   | Broad parity                                                                                            |
| Version detail       | Compare Versions, Modified only, locale set, previous/current labels, Restore this version and Restore as draft                                | Locale-aware field comparison, Modified fields only, arbitrary revision selection, Restore, and Restore as draft for draft resources                        | Broad parity                                                                                            |
| API view             | Dedicated `/api` admin route with expandable JSON tree                                                                                         | Dedicated collection, create, and global `/api` routes that survive direct navigation and reload, with a recursive expandable and copyable JSON tree         | Broadly comparable                                                                                      |
| Trash                | Empty-trash action plus active/trash switching and selected Restore/Delete                                                                     | Dedicated route with per-row and atomic selected restore/permanent delete plus confirmed collection-wide Empty trash                                       | Broad parity for the inspected surface                                                                  |
| Custom route         | Broad component replacement model                                                                                                              | Typed static plugins compose or replace dashboard, auth/account, list/create/edit/global/not-found views and add routes, actions, cells, shell slots, providers, and document views | Broadly comparable through Ridu's compile-time Svelte model                                             |
| Live preview         | Split editor/iframe, Responsive plus configured viewport presets, editable width/height, zoom, open in new window, and unsaved draft streaming | Matching authoring controls plus cross-origin iframe/popup streaming, origin/source/channel authentication, five-minute document-scoped server draft capabilities, and locale-preserving document routes | Broad parity                                                                                            |
| Document lock        | Expiring editor lease, owner dialog, View read-only, Go back, and Take over                                                                    | Persisted expiring lease, owner dialog/banner, read-only form and actions, heartbeat/release, and access-controlled Take over                              | Broad parity for the inspected exclusive-editing surface; realtime multi-user presence remains optional |

## Access-control audit

The editor account was used on the same protected Editorial note and Redirect in both products.

- Payload omitted `Confidential Details` completely for the editor and omitted the denied Redirect
  delete action.
- Ridu now omits the same protected field, omits the denied Redirect delete action, and preserves the
  separately allowed Duplicate action.
- The contributor pass also confirmed that a collection denied at read level is removed from the
  navigation rather than advertised as a route that will fail.
- A dedicated API-only auth user proves that authentication/API use can remain valid while the auth
  collection's `Admin` rule denies entry to the admin shell. A separate contributor case keeps Pages
  readable while a denied `ReadVersions` rule removes history actions and rejects direct history
  requests.

The fix adds non-secret, per-request collection/global operation and field capabilities. It exposes
only evaluated booleans; executable rules and filtered predicates remain server-side. The admin
uses those booleans for navigation, forms, list and document actions, custom actions, trash, field
validation, and submission. Browser coverage repeats the editor and contributor scenarios and
asserts that the protected controls are absent.

This pass exposed an access-coupling bug in the document controller: it loaded the current document
and version history in one `Promise.all`, so a denied history request blanked an otherwise readable
document. Version loading now begins only after the document-specific `ReadVersions` capability is
evaluated; ordinary authoring remains usable when history is intentionally restricted.

## Browser issues found and fixed during this pass

The first saved-view test produced the only new Ridu console error:

```text
Error: prompt() is not supported.
```

The saved-view action used `window.prompt`, which is not supported by the in-app admin runtime. It
was replaced with an inline labelled name input and Save button. Saved-view deletion now also
handles and reports request failures. The flow was rebuilt and repeated: folder and hierarchy state
survived reload, `Editorial hierarchy` appeared in the menu, and no new console error was emitted.

The audit also exposed fixture under-coverage rather than a framework defect: password recovery and
API keys were implemented but disabled in the example. The fixture now enables both with a no-op
test delivery callback that never logs reset tokens.

The expanded list pass exposed two further defects. Numeric filter values arrived from the input as
numbers but the URL normalizer accepted only strings, so the chip rendered while the server query
silently omitted the filter. The normalizer now accepts string, number, and boolean input and emits
the correct typed `where` value. It also exposed a session-lifecycle race: an aborted navigation
could cancel identity lookup and that transient error revoked the otherwise valid session. Session
resolution now revokes only a genuinely missing identity; ordinary `GET /api/auth/me` is stable and
explicit rotation lives at `POST /api/auth/refresh`.

The version-workspace implementation exposed three further defects. Scheduled jobs discarded the
requesting user and therefore failed role-based publish access when a worker executed them; jobs now
persist and rehydrate that auth identity and re-check access. Version snapshots were returned in a
raw storage shape and without field-level read redaction; they now use the public document shape and
cannot reveal protected historical fields. Finally, the SDK request merger discarded internally
generated `If-Match` headers, so revision preconditions on existing mutations were not sent; request
headers now merge in the correct order and the scheduled-publish regression test covers it.

The asset-workspace pass found that focal coordinates were already present in the manifest and
stored document shape but were presentation-only: every configured `cover` size still cropped from
the centre. Cover generation now uses the persisted focal coordinates. Regeneration stages fresh
variant objects, commits their metadata through ordinary update access and revision checks, rolls
them back on failure, and removes superseded variants after success. The first end-to-end preview
pass then found that authenticated upload delivery matched only the original object key, so newly
generated `card` and `thumbnail` URLs returned 500. Delivery now recognizes every configured size
key through the same access-filtered document query; local API, REST, E2E, and Browser coverage all
exercise the variant path.

The grouped-shell pass exposed a public authoring-contract gap: the manifest representation already
allowed admin metadata on globals, but `ridu.Global` had no way to author a group or description.
`GlobalAdmin` now carries both values through the root facade, resolver, immutable manifest, and
admin. The comparison fixture no longer fakes group membership by prefixing collection labels. The
rebuilt Ridu dashboard and collapsed/expanded sidebar were checked beside Payload in the in-app
Browser with no warnings or errors.

The auth-user creation pass exposed a data-integrity defect behind the missing UI: both the
full-page form and inline relationship drawer could create an auth document without a credential,
leaving a user that could never sign in. Auth creation now runs the ordinary create operation and a
store-specific credential mutation in one transaction. The public local API, REST route, generated
OpenAPI surface, Fetch SDK, full-page form, and inline drawer all use that path. Passwords remain
outside document values and generated document contracts. Browser verification matched Payload's
new/confirm-password flow, exercised mismatch focus and successful creation, then signed in as the
new user without console errors.

The version follow-up now mirrors Payload's alternate **Restore as draft** action on both
collection and global history. Local APIs, typed globals, REST/OpenAPI, and the Fetch SDK carry an
explicit draft restore option through the same access-checked update pipeline as ordinary restore;
resources without drafts reject the option. The in-app Browser restored a published Page revision
as a draft, returned to its edit route, and emitted no console warnings or errors.

The trash follow-up matches Payload's populated collection workspace: selected documents can be
restored or permanently deleted atomically, while a separate confirmed **Empty trash** action
operates on the whole collection rather than the current page. Local API, REST/OpenAPI, Fetch SDK,
admin, and regression coverage share the same access-checked `RestoreDeleted` and
`DeletePermanent` operation paths. Payload and Ridu were both returned to an empty trash state in
the in-app Browser without console errors.

The nested-authoring follow-up mirrors Payload's **Copy Field**, **Paste Field**, **Add Below**,
**Copy Row**, and **Paste Row** workflows for groups, arrays, and blocks. Ridu encodes a versioned
schema signature with copied data, rejects incompatible fields and row kinds, honors row bounds,
and regenerates every nested `_key` during paste so copied rows never reuse drag/store identity.
The in-app Browser copied and pasted both a Page block and its whole Layout field without console
errors; unit and E2E coverage also exercise the bounded array path.

The list-selection follow-up mirrors Payload's two-stage **Select all** workflow. Selecting the
header checkbox first targets only the visible page; the bulk bar then offers the exact filtered
total. Promotion now sends one abortable filter/trash request to a server-owned resolver. The
resolver combines that filter with Read access, freezes canonical IDs and full capabilities, and
rejects 101+ matches without truncation so the existing batch engine can remain all-or-nothing.
Changing the query or manual selection aborts pending promotion, and stale responses cannot replace
newer intent. The in-app Browser confirmed Payload's 1-of-4 promotion to 4 selected; Ridu's E2E
fixture proves one-request promotion and cancellation across pagination.

The nested-list follow-up mirrors Payload's actual split rather than the older blanket gap note:
group leaves such as **SEO > Title** are columns, filters, and sortable values; repeated roots such
as **Links** and **Layout** render compact row/block summaries; their declared scalar leaves are
filterable. Dotted paths are schema-validated at the HTTP boundary. The in-memory store traverses
objects, arrays, and block discriminators, while PostgreSQL uses typed JSON extraction and
existential row predicates; repeated paths are rejected for sorting. Browser checks filtered Posts
by SEO title, link-row label, and quote-block text with no console errors. The implementation also
caught an ambiguous preference bug: a configured top-level `title` could resolve to a nested child
also named `title`. Nested columns now require their canonical dotted path, preserving existing
top-level defaults and preferences.

The rich-text follow-up exposed an incomplete plugin contract rather than a cosmetic gap. The Go
plugin and serialized configuration already declared relationship nodes, but the Svelte admin did
not register a relationship node or picker, so enabling the feature produced documents the editor
could not author or reopen. Relationship cards now use the ordinary access-aware reference browser
for insert, inspect, replace, and remove. Checklists, underline, subscript/superscript, block
alignment, and indentation close the remaining default-feature differences visible in Payload 3;
the Go validator and safe HTML renderer cover the corresponding stored shapes.

The live-preview follow-up first fixed the Payload reference itself: the comparison config claimed
the feature as a gap but had no `livePreview` URL or breakpoint configuration. With that fixture in
place, Payload exposed a split pane with Responsive/Mobile/Tablet/Desktop presets, manual
dimensions, zoom, open-in-new-window, and immediate draft streaming. Ridu now resolves a
serializable Go URL template (`{collection}`, `{id}`, and `{field:path}`) into the immutable
manifest and exposes the same authoring controls. Preview messages target the resolved origin
rather than `*`, and only current form values are sent. The in-app Browser confirmed unsaved title
and summary updates in both products. It also caught a Ridu readout bug: selecting Mobile resized
the iframe correctly but left Responsive dimensions in the disabled inputs. The controls now show
the selected preset's real 375×667 dimensions, with E2E coverage.

The locking follow-up reproduced Payload's owner dialog with a second authenticated Browser
session. Ridu now stores atomic expiring leases in both strict in-memory and PostgreSQL stores,
refreshes an owned lease while editing, releases only the current owner's lease, and switches every
write action to read-only when another editor owns it. Takeover is separately authorized through
`CollectionAccess.Unlock` (falling back to update access when omitted). The Browser pass caught and
fixed a router-base bug in **Go back**, plus lifecycle hazards where autosave could race a takeover
and a dismissed dialog could remain dismissed for another locked document. The final pass exercised
View read-only and Take over with no console errors.

The event follow-up exposed a separate list-preference race: an author who changed columns before
the initial preference read settled could have the choice silently overwritten because Ridu was
aborting the saved-view request instead of the workspace request. Those request lifecycles are now
owned separately, and rapid column changes are based on synchronously updated workspace state.
The same pass added manifest-backed `dayOnly`, `dayAndTime`, and `timeOnly` date appearances,
RFC 3339 conversion for date-time values, time-aware list/filter presentation, and nested-field E2E
coverage.

The inverse-join follow-up replaced the compact read-only link stack with a manifest-configured
table. `JoinColumns`, `JoinDefaultSort`, and `JoinAllowCreate` now flow from executable Go config to
the immutable manifest. The admin can sort the embedded rows, create a target with the inverse
relationship prefilled, choose existing targets, and attach or detach them. Those controls do not
bypass the engine: one explicit-delta request wraps the ordinary target update pipeline in a single
transaction, preserving target read/update and field access, validation, hooks, versions, and
relationship integrity. An observed inverse-value predicate rejects concurrent reparenting without
lock upgrades. The in-app Browser confirmed the News category table and its four configured columns;
focused E2E coverage proves one pending-protected request adds and then removes an existing Post.

The relationship-filter follow-up generalizes the earlier one-field equality shortcut without
turning picker filtering into authorization. `FilterOptionRules` combines typed predicates over
direct or nested group paths, and `OptionFilterFor` scopes a rule to one target in a polymorphic
relationship. The quick picker and full browser derive those rules from the current form and issue
the existing server-side where query. This pass also found a polymorphic hydration bug: a stored
Pages relationship initially rendered the first configured target, Posts. The controller now derives
its initial and reloaded target from `relationTo`; E2E coverage confirms the saved About Ridu page
opens under Pages.

The API-view follow-up replaced ephemeral in-page mode with stable collection-document, create, and
global `/api` routes. Edit/API controls are ordinary router links, direct navigation and reload keep
the selected mode, and the explicit create route avoids treating `create` as a document ID. The
view now presents recursive expandable object/array branches and copies the complete JSON payload;
the same viewer is reused by the relationship editor API mode.

## Remaining excluded and optional work

Content localization is now in scope and covered by the comparison fixture; admin interface
translation remains separate. GraphQL is not required for this admin authoring comparison.
Realtime multi-user cursors/presence could be added later, but Payload 3's
inspected behavior is the exclusive edit lease that Ridu already matches; it is not an open parity
gap in this audit.
