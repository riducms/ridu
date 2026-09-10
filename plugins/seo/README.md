# SEO plugin

`plugins/seo` adds a `meta` group with an overview, localized title and description, an optional
upload image, and a live search-result preview. The fields use normal access, validation,
localization, drafts, versions, REST routes, and generated types.

```go
seo.New(seo.Config{
	Collections:       []schema.CollectionSlug{"pages", "posts"},
	Globals:           []schema.CollectionSlug{"site-settings"},
	UploadsCollection: "media",
	TabbedUI:          true,
	GenerateTitle: func(ctx seo.GenerateContext) (string, error) {
		return ctx.Document["title"].(string) + " | Example", nil
	},
	GenerateURL: func(ctx seo.GenerateContext) (string, error) {
		return "https://example.com/" + ctx.ID, nil
	},
})
```

Add `seo.New(...)` to the application's Go config and run `ridu generate`. Generated projects already
include `@riducms/plugin-seo`; existing projects must install a version compatible with the Go
plugin before generating. Configure the plugin in Go because it requires resource options and may
use callbacks.

Generator callbacks receive the current unsaved form snapshot, resource definition, document ID,
locale, actor collection, actor-aware endpoint context, and local API. Their endpoints require an
authenticated actor and the corresponding collection/global access before running. Generator
responses are discarded if the draft changes while a request is pending.

`Fields` can replace or extend the ordered default definitions. The exported `Overview`,
`MetaTitle`, `MetaDescription`, `MetaImage`, and `Preview` constructors support direct placement.
`Collections` and `Globals` select automatic field injection. Directly placed SEO fields can also
use generator endpoints outside those selectors. Ridu checks the actor's resource access before
invoking a generator.

Each factory returns a concrete immutable field. Refine it directly; access, hooks and editor
configuration follow every placement:

```go
title := seo.MetaTitle(true).Localized(false).Required().MaxLength(80)
image := seo.MetaImage(seo.MetaImageConfig{Collection: "media"}).Localized(false)
overview := seo.Overview(seo.OverviewConfig{})
preview := seo.Preview(seo.PreviewConfig{Generate: true})
```

`MetaTitle`, `MetaDescription` and `MetaImage` are localized by default. Use `.Localized(false)`
when placing them in an application without localization. Overview, image and preview settings
are policy structs; ordinary field refinements use the concrete fluent surface.

See the [SEO guide](../../website/src/content/docs/seo.md) for installation, callbacks, field
placement, migrations, and verification.
