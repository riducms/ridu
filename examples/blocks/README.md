# Blocks and rich-text reference

This finite application composes pages from reusable Hero, Content, Media and CTA
schemas. Hero and CTA are also used by campaigns. Articles embed Callout, Media and CTA in
rich text; Callout contains two finite editors, one with its own CTA allowlist. The same Go configuration drives
Go, TypeScript and OpenAPI generation; none of the files in `generated/` is edited
by hand.

The example includes localized block children, draft revisions, bounds, row
summaries, a nested group and array, an ordinary rich-text child, and populated
relationships to asset records. Assets here are metadata records with URLs; file
upload setup is outside this example. Rich text remains the existing versioned
plugin document with configured schema-backed block payloads in articles.

## Run the reference checks

From the repository root:

```sh
(cd examples/blocks && go run ../../cmd/ridu generate)
(cd examples/blocks && go run ../../cmd/ridu generate --check)
go test ./examples/blocks/...
bunx svelte-check --workspace examples/blocks --tsconfig ./tsconfig.json
go test ./internal/cli -run '^TestFreshProjectBlocksReferenceWorkflow$' -count=1
```

The last command creates a fresh SQLite `ridu new` application outside the
repository, copies this application configuration and Go consumer into it,
generates its contracts, creates and applies an initial migration, creates and
reads localized content, adds an optional Hero field, creates and applies the
next migration, regenerates, and verifies both existing content and the new
field. It does not hand-edit generated output.

## Use in a generated application

Create a normal starter with `ridu new --database sqlite`, copy
`content/config.go` into its `content/` directory, and run `ridu generate`.
The reference's default access policy is for local exploration. A deployed
application should define its own collection access rules and authenticated admin.
The generation-only entry in this directory is deliberately small; the generated
starter supplies the normal server, embedded admin and runtime configuration.

Create and apply the initial migration with `ridu migrate create --name initial`
and `ridu migrate up`. The SDK example in `scripts/seed.ts` sends keyless block
inputs; the server returns stable `_key` values. Its only environment setting is
`RIDU_URL` (default `http://localhost:8080`).

Go creation uses the generated typed collection and immutable discriminator codecs:

```go
page, err := generated.PagesCollection.With(app.Local()).Create(ctx,
    generated.PageCreate{
        Title: "Home",
        Layout: core.NonNull(generated.PagesLayoutInput{
            &generated.HeroInput{Heading: "Welcome"},
            &generated.CTAInput{Label: "Read more"},
        }),
    }, nil)
```

Optional mutation values use `core.Set(value)` or `core.Null[T]()`, and nil
omits the field. Outputs make authored fields optional because access rules and
projection can remove them. Nullable Go block children use `BlockOptional[T]`: a
nil wrapper means omitted, a nil `Value` means explicit null, and `Get()` safely
extracts a concrete value. Required children still use pointers for omission.
Block output `_key` remains mandatory. A Hero's
`BlockType()` and JSON discriminator are always `hero`; there is no mutable
`BlockType` member to accidentally set to `cta`.

### Choose the type for the task

| Task | Type or binding |
| --- | --- |
| Create a page / add a new block | `PageCreate`, `HeroInput`, `PagesLayoutInput` |
| Render one locale | `Page`, `Hero`, `PagesCollection` |
| Edit existing blocks | `page.Layout.Retain()`, then `HeroUpdate` inside `PagesLayoutUpdate` |
| Export or inspect every translation | `PageAllLocales`, `HeroAllLocales`, `PagesCollectionAllLocales` |

`AllLocalesValue` is a contextual type for children beneath an already localized
container. Let the containing field select it; ordinary rendering uses `Hero`.

### Read and write field presence

| Field representation | Read or write it |
| --- | --- |
| Read `*T` | Check for nil before dereferencing; nil means the field was omitted. |
| Read `*BlockOptional[T]` | `value, ok := field.Get()`; false means omitted or null. |
| Write `*T` | A pointer sends a value; nil leaves it omitted. |
| Write `*core.Input[T]` | `core.Set(value)` sends a value; `core.Null[T]()` clears it; nil omits it. |
| Write `core.NonNullInput[T]` | `core.NonNull(value)` supplies a required value such as a block list. |
| Write `*core.NonNullInput[T]` | `core.SetNonNull(value)` sends a value; nil omits it. |

Mutation wrappers also expose `Get()` to inspect a concrete value using the same
`value, ok` convention as nullable reads. `core.Input.Get()` reports false for nil
or explicit null; `core.NonNullInput.Get()` reports false only for an omitted wrapper.
Encoding still rejects values that encode as null in a non-null field.

These are the same rules for create and update fields. An omitted create field
uses its configured default when one exists; an omitted update field stays unchanged.
An empty string, false or zero is still a value. In particular, keep a pointer to
false when updating a checkbox. Do not turn omitted or null reads into default
values and write them back. Rendering may choose a fallback locally:

```go
caption, _ := media.Caption.Get() // Empty display text for null or omitted caption.
// Escape caption when inserting it into HTML; render/blocks.go demonstrates this.
```

### Edit existing blocks

Existing rows have separate keyed update contracts. An update can omit children
that were redacted or are unrelated to the edit:

```go
heading := "Bonjour"
patch, err := page.Layout.Retain()
if err != nil {
    return err
}
for _, value := range patch {
    if hero, ok := value.(*generated.HeroUpdate); ok {
        hero.Heading = &heading
    }
}
_, err = pages.UpdateRevision(ctx, pageID,
    generated.PageUpdate{Layout: core.SetNonNull(patch)}, revision, nil,
    core.TypedLocaleOptions{Locale: "fr"})
```

`Retain()` preserves every occurrence and its order using fresh key-only updates.
It never copies authored fields back from reads. It rejects absent layouts, nil rows,
empty keys and duplicate keys without returning a partial list. An explicitly empty
layout produces an empty update list. Edit the returned pointers, append a new
`&generated.HeroInput{...}`, or explicitly remove/reorder entries. The saved list
still determines order and membership: omitted occurrences are removed. Use the
revision returned by the read to reject stale edits. A `HeroUpdate` requires its existing
key; runtime validation confirms
that identity belongs to an existing Hero before treating it as a retained patch.
A new or replaced variant uses `HeroInput` with its required authoring fields.
`PagesLayoutUpdate` accepts both retained updates and new strict inputs.
The reference workflow verifies omitted appearance, links, rich text and the
required media relation remain intact. Never edit generated types to loosen inputs.

### Edit a nested link

Arrays inside blocks expose concrete read rows and an identity-only `Retain()` method:

```go
links, present := content.Links.Get()
if !present {
    return errors.New("links were omitted or null")
}
changes, err := links.Retain()
if err != nil {
    return err
}
for _, value := range changes {
    if value.RowKey() == linkKey {
        link := value.(*generated.ContentLinksRowUpdate)
        href := "/about-us"
        link.Href = &href
    }
}
contentUpdate.Links = core.Set(changes)
```

Here `content` is the read block, and `contentUpdate` is its matching pointer from
`page.Layout.Retain()`. Save the page with the revision returned by the read. The helper
preserves every link and its order without copying labels, destinations, or other fields.
It rejects nil lists, missing keys and duplicate keys. An explicit empty list remains empty.
Append `&generated.ContentLinksRowInput{...}` to add a link. Array update containers use
pointers; ordinary array creation slices still use concrete `ContentLinksRowInput` values.
Both new and retained array update rows expose `RowKey()` for finding, removing or reordering
entries without a type assertion. It returns the supplied identity, or an empty string for
a nil or unkeyed row. Concrete array read rows expose the same accessor.

The workflow edits one link while required labels are redacted, then reads it again to
verify both labels, the sibling destination, row identities and unrelated blocks survived.

`render/Blocks.svelte` consumes the generated discriminated union directly,
using `_key` for keyed rendering. `render/blocks.go` switches on `*generated.Hero`
and the other generated pointers, and escapes text with the standard library.
Its generic `HTML` function accepts both `PagesLayout` and `CampaignsLayout` without
converting their slices; the element type is inferred at each call:

```go
if page.Layout != nil {
    if err := render.HTML(w, *page.Layout); err != nil {
        return err
    }
}
if campaign.Layout != nil {
    if err := render.HTML(w, *campaign.Layout); err != nil {
        return err
    }
}
```

Each generated list still admits only its configured block variants.
Both renderers handle omitted authored values.
They intentionally use ordinary application rendering, with no renderer registry.
The Svelte renderer displays Content's body through the plugin's read-only `RichText`
component. Its final branch accepts `never`, so adding a generated block variant requires
adding its renderer before type checking passes. Unexpected variants from stale runtime
data show an alert while the other blocks remain visible. The small Go layout renderer
shows headings only; `render.ArticleHTML` demonstrates full rich-text rendering in Go.
Rich-text reference nodes need an application-owned `references` renderer for their presentation.
The example supplies a visible `fallback` for these nodes or unsupported documents, keeping
the remaining page content visible instead of throwing a rendering error.
Block lists admit pointers only; JSON decoding returns those same pointer types.
`BlockKey()` exposes the occurrence identity without casts or JSON conversion.
Go does not provide compiler-enforced exhaustive type switches.

Select and radio fields generate named string types and typed constants, shared
across create, update and read types. For example:

```go
appearance := generated.HeroAppearanceInput{
    Tone: core.Set(generated.HeroAppearanceToneDark),
}
```

Constants provide autocomplete and prevent mixing unrelated named choice types.
The engine still validates configured choices, including values supplied through
explicit conversions. Generated field GoDoc explains presence wrappers at the point
of use; nil, explicit null and concrete values retain their existing meanings.

The small preview in `App.svelte` loads a published page through the generated SDK.
Run `(cd examples/blocks && RIDU_URL=http://localhost:8080 bunx vite)` and open
`?page=<page ID>`. The browser acceptance test starts isolated API and Vite servers,
creates and publishes content through the SDK, verifies all four rendered variants
(including formatted rich text and a loaded media image), and reads the API again to confirm
identity. It also checks that an unexpected API variant produces visible feedback without
hiding the known blocks, and that an authored relationship node without a reference renderer
shows the rich-text fallback while later sections remain visible.
Its local workspace aliases use package sources so concurrent package builds cannot
change the running fixture. A generated application uses installed package exports.

The full Go workflow in `workflow_test.go` also shows nested typed inputs,
relationship population, French updates that preserve occurrence keys,
optimistic revision checks and typed all-locales reads. For an all-locales read,
use `PagesCollectionAllLocales`; each localized child has a locale-code map.
Normal reads and renderer inputs use `PagesCollection` and single-locale
variants. Populated Go relations expose `ID` and optional `Document`, while
canonical mutation input takes the target ID.

### List pages with populated media

```go
assetPath, err := query.ParsePath("layout.media.asset")
if err != nil {
    return err
}
result, err := generated.PagesCollection.With(app.Local()).List(ctx,
    core.TypedListOptions{
        Page: 1,
        Limit: 20,
        Populate: []query.Population{{Path: assetPath, Depth: 1}},
    })
if err != nil {
    return err
}
for _, page := range result.Documents {
    if page.Layout == nil {
        continue // The layout was omitted by access rules or projection.
    }
    for _, row := range *page.Layout {
        media, ok := row.(*generated.Media)
        if !ok || media.Asset == nil || media.Asset.Document == nil {
            continue
        }
        // media.Asset.ID is the canonical ID; Document is the typed asset.
    }
}
```

Use the same `List` operation on `PagesCollectionAllLocales` to return all
translations, including typed populated documents. `Where`, `Sort`, `Select`,
`OutputFields`, `Draft`, `TrashOnly` and actor/locale options run through the
normal engine. Access predicates apply before pagination, and `Total` counts
only matching readable documents. An empty result has an empty `Documents` slice;
a decode failure returns an error with no partial page.

### Diagnose malformed blocks

Generated codecs and `Retain` report `*generated.ContractError`. `Operation` is
`decode`, `encode` or `retain`; `Container` names the generated type and `Path`
locates the field or occurrence within it. For example:

```text
decode PagesLayoutInput[0].links[1].label: required field is absent or null
```

Use `errors.As(err, &detail)` with `var detail *generated.ContractError` to inspect
those fields. Underlying JSON and plugin errors remain available through
`errors.As` and `errors.Is`. Decoding fails without replacing the destination
with a partial list. Unknown variants remain errors; migrate their data or
restore their schema before using the ordinary typed API.

## Evolve a block

Add `field.Text("eyebrow")` to Hero, then run:

```sh
ridu generate
ridu migrate create --name hero-eyebrow
ridu migrate up
ridu generate --check
```

Old rows keep their discriminator and occurrence key, and omit the new optional
field. New Go inputs expose `HeroInput.Eyebrow`; TypeScript inputs expose
`HeroInput.eyebrow`. Do not rename `hero` to rename a generated type: use
`TypeName` for generated symbol identity and labels for presentation.
Removing or changing a stored variant needs an explicit compatible data migration;
ordinary typed APIs fail closed on unknown variants. Recovery tooling remains
separate from this application's typed models.

## Typed article rendering

`render/Article.svelte` uses the public read-only `@riducms/plugin-richtext/svelte` entry with a
checked map of Callout, Media and CTA components. `Callout.svelte` renders a nested document with
its own narrower CTA allowlist. `render.ArticleHTML` demonstrates the same finite composition in
Go using generated payload unions. Neither renderer loads an editor or fetches per block; callers
supply access-approved values and populate references through the ordinary API when needed.
