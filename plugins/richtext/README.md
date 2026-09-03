# Ridu rich-text plugin

`github.com/riducms/ridu/plugins/richtext` provides the rich-text field, portable Lexical document
schema, server validation, reference metadata, and safe Go HTML renderer. Pair it with
`@riducms/plugin-richtext` for the Svelte/Lexical editor.

Register the backend once, then use its field constructor:

```go
ridu.Config{
	Plugins: []ridu.Plugin{richtext.New()},
	Collections: []ridu.Collection{{
		Slug: "posts",
		Fields: []field.Definition{
			richtext.Field("content", field.Required()),
		},
	}},
}
```

In an existing project, add both packages:

```sh
ridu add richtext \
  --go-package github.com/riducms/ridu/plugins/richtext \
  --admin-package @riducms/plugin-richtext
```

Then generate and create the selected adapter's migration. The generated registry imports the admin
pair and rejects incompatible pairing metadata.

See the [Rich text guide](../../website/src/content/docs/rich-text.md) for installation, features,
constraints, verification, SDK values, and server rendering.
