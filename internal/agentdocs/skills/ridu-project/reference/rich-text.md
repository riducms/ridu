<!-- Generated from website/src/content/docs/rich-text.md by scripts/sync-agent-docs.ts. -->

# Rich text

The rich-text plugin lets editors write formatted content, insert media, and add custom blocks
such as callouts. Choose the available tools and block fields in Go. The admin provides a Svelte 5
editor built with Lexical, and your frontend can display the saved content with Go, JavaScript,
or Svelte.

Content is saved as JSON. You decide how it looks on your website; the admin editor does not become
part of your frontend.

## Find what you need {#pages}

| Page                                                      | Read it to                                                                |
| --------------------------------------------------------- | ------------------------------------------------------------------------- |
| [Features](./rich-text/features.md)                     | See what editors can do, and choose links, lists, media, and other tools. |
| [Toolbar and layout](./rich-text/toolbar-and-layout.md) | Pin a toolbar above the editor, hide the gutter, or hide block controls.  |
| [Custom blocks](./rich-text/blocks.md)                  | Add structured content such as callouts, and change it safely later.      |
| [Display rich text](./rich-text/display.md)             | Understand the saved JSON and render it with Go, JavaScript, or Svelte.   |
| [Use the editor in your app](./rich-text/editor.md)     | Let your app's users write rich text, save it to a field, and restyle it. |

## Configuration {#configuration}

`richtext.Field(name)` uses the recommended tools. Pass one `richtext.Config` to change them:

```go
richtext.Field("content", richtext.Config{
	// Limit the media picker to one collection.
	UploadCollections: []string{"media"},
	// Keep formatting controls visible above the text.
	Admin: richtext.Admin{FixedToolbar: true},
}).Required()
```

| Option or method                     | Default                                                     | See                                                               |
| ------------------------------------ | ----------------------------------------------------------- | ----------------------------------------------------------------- |
| `Config.Features`                    | Links, lists, code, horizontal rule, uploads, relationships | [Choose the editing tools](./rich-text/features.md#features)    |
| `Config.UploadCollections`           | All compatible upload collections                           | [Choose the editing tools](./rich-text/features.md#features)    |
| `Config.RelationshipCollections`     | All compatible collections                                  | [Choose the editing tools](./rich-text/features.md#features)    |
| `Config.Admin`                       | Floating toolbar, gutter, and block controls shown          | [Toolbar and layout](./rich-text/toolbar-and-layout.md#options) |
| `Config.Blocks`                      | None                                                        | [Add custom blocks](./rich-text/blocks.md)                      |
| `Config.BlockReferences`             | None                                                        | [Reuse block definitions](./rich-text/blocks.md)                |
| Field `.Required()` / `.Localized()` | Optional, shared value                                      | [Translate blocks](./rich-text/blocks.md#block-localization)    |

Supplying more than one `Config` panics.

## Use it in a new project {#new-project}

The `starter` template already installs and registers rich text. Open `content/posts.go`, use
`richtext.Field("content")`, then run `ridu dev`. Do not add a second registration.

If you chose the `blank` template, follow the existing-project installation below.

## Add it to an existing project {#existing-project}

Add the Go and admin packages together:

```bash title="terminal"
ridu add richtext \
  --go-package github.com/riducms/ridu/plugins/richtext \
  --admin-package @riducms/plugin-richtext
```

The command registers the plugin for you. Keep `installedPlugins()` in your existing Go config,
and add a rich-text field to a collection. For example:

```go title="content/config.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/richtext"
)

func Config() ridu.Config {
	return ridu.Config{
		Name:        "Acme Editorial",
		Plugins:     installedPlugins(),
		Collections: []ridu.Collection{Posts},
	}
}

var Posts = ridu.Collection{
	Slug: "posts",
	Fields: field.Fields{
		field.Text("title").Required(),
		richtext.Field("content").Required(),
	},
}
```

Start the development server:

```bash title="terminal"
npm run dev
```

Open Posts in the admin and create a document. Add bold text and a link, save, then reload the page.
The formatting should remain. Read the document through your generated SDK to see its JSON value.

Before deploying the new field, create and check its database migration:

```bash title="terminal"
ridu migrate create --name add-rich-text
ridu migrate verify
ridu check
```

Review the migration, apply it with `migrate up` during deployment, and confirm `migrate status`
has no pending steps. See [Migrations](./migrations.md) for the database connection options.

## What is not available yet {#limits}

Tables, custom inline content, HTML/Markdown import and export, plain-text rendering, and
collaborative editing are not available yet.

See the [`richtext` Go reference](https://riducms.com/reference/richtext/), the
[`@riducms/plugin-richtext` reference](https://riducms.com/reference/plugin-richtext/), [Uploads](./uploads.md),
and [Build a field plugin](./custom-fields.md).
