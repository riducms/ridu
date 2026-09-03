# `@riducms/plugin-richtext`

The Svelte/Lexical editor for Ridu's Go rich-text plugin. It exports the admin field registration and
authoring UI used by generated projects.

Add the Go and admin packages together. In an existing project:

```sh
npm run ridu -- add richtext \
  --go-package github.com/riducms/ridu/plugins/richtext \
  --admin-package @riducms/plugin-richtext
```

Add `richtext.New()` to `Config.Plugins` and author fields with `richtext.Field`. The generated admin
registry imports `richTextAdminPlugin`; do not register it again. Generation rejects incompatible Go
and npm pairing metadata before the editor loads.

Run `npm run dev`, verify the editor, then create and review the adapter migration before deployment:

```sh
npm run ridu -- migrate create --name add-rich-text
```

See the [Rich text guide](../../website/src/content/docs/rich-text.md) for new- and
existing-project setup, feature selection, uploads/relationships, portable values, constraints,
safe Go rendering, migrations, and verification.
