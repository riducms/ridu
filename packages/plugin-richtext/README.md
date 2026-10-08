# `@riducms/plugin-richtext`

The Svelte/Lexical editor for Ridu's Go rich-text plugin. It exports the admin field registration and
authoring UI used by generated projects.

Add the Go and admin packages together. In an existing project:

```sh
ridu add richtext \
  --go-package github.com/riducms/ridu/plugins/richtext \
  --admin-package @riducms/plugin-richtext
```

Add `richtext.New()` to `Config.Plugins` and author fields with `richtext.Field`. The generated admin
registry imports `richTextAdminPlugin`; do not register it again. Generation rejects incompatible Go
and npm pairing metadata before the editor loads.

Run `bun run dev`, verify the editor, then create and review the adapter migration before deployment:

```sh
ridu migrate create --name add-rich-text
```

See the [Rich text guide](../../website/src/content/docs/rich-text.md) for new- and
existing-project setup, feature selection, uploads/relationships, portable values, constraints,
safe Go rendering, migrations, and verification.

Configure block-level embeds with executable `richtext.Config.Blocks` definitions. The paired
editor uses the resolved allowlist for immediate slash/toolbar insertion. Each block exposes its
ordinary fields inside an expandable section in the editor. Changes participate in the parent
document save and Lexical history; nested rich-text fields keep their own selection and history.
Configured names appear in the header, and field errors expand the block before focusing its
invalid child. Collapse retains mounted fields and unfinished input.

Content consumers import portable document types and HTML rendering from `@riducms/sdk/richtext`;
installing `@riducms/sdk` is sufficient and does not install Svelte or Lexical.
Optional typed Svelte rendering is available from `@riducms/plugin-richtext/svelte`, which does not
load the admin editor. Applications that let their users write rich text import `RichTextEditor`
from `@riducms/plugin-richtext/editor`, and can configure its styles with
`@riducms/plugin-richtext/editor.scss`; see
[Use the editor in your app](https://riducms.com/docs/rich-text/editor/). See the rich-text guide for strict payloads, localization, typed renderers
and experimental-content migration.

The supported authoring UI uses semantic SCSS, compact grouped insertion menus, six heading levels,
Markdown shortcuts, interactive checklists, custom-URL link drawers and intrinsic-ratio media cards.
See [customizing admin CSS](https://riducms.com/docs/admin/customizing-css/) for styling and the
[rich-text guide](https://riducms.com/docs/rich-text/) for supported features and limits.
No utility stylesheet is needed for this plugin's UI.
