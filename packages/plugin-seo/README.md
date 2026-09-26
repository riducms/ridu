# `@riducms/plugin-seo`

The Svelte admin UI for Ridu's Go SEO plugin. It provides a metadata overview, title and description
guidance, upload selection, generator controls, and a live search-result preview.

For an existing project, install it in the root and admin workspaces, configure `seo.New(...)`, then
start development:

```sh
bun add @riducms/plugin-seo
bun add --cwd admin @riducms/plugin-seo
bun run dev
```

`ridu dev` regenerates contracts and synchronizes safe additive development changes. Before
deployment, create and review the immutable adapter migration with
`ridu migrate create --name add-seo`.

The generated registry imports `seoAdminPlugin`; do not register it again. Generated title,
description, image, and URL values remain editable and pass through normal validation and access
rules when saved.

The field stack follows Payload's overview, inline generation, length guidance, upload intake, and
search-preview anatomy. Its `ridu-seo-*` presentation is semantic SCSS in the shared component
layer; applications can override those hooks without depending on UnoCSS utility output.

See the [SEO guide](../../website/src/content/docs/seo.md) for setup, callbacks, migrations, and
verification. See [customizing admin CSS](https://riducms.com/docs/admin/customizing-css/) for
application styling overrides.
