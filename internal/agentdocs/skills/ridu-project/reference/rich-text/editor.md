<!-- Generated from website/src/content/docs/rich-text/editor.md by scripts/sync-agent-docs.ts. -->

# Use the editor in your app

Your app's users can write rich text with the editor the admin uses. It edits exactly the document
a `richtext.Field` stores, so your app saves it through the generated client like any other value.

## Install the editor {#install}

Add the package to your SvelteKit app:

```bash title="terminal" package-manager="bun"
bun add @riducms/plugin-richtext
```

```bash title="terminal" package-manager="npm"
npm install @riducms/plugin-richtext
```

```bash title="terminal" package-manager="pnpm"
pnpm add @riducms/plugin-richtext
```

```bash title="terminal" package-manager="yarn"
yarn add @riducms/plugin-richtext
```

The editor needs no Vite plugin, preprocessing or Sass. It ships its own styles and loads them
when it renders.

## Edit a field {#edit}

Import `RichTextEditor` from `@riducms/plugin-richtext/editor` and bind it to the field's value.
This page loads a post on the server and saves its `body` from the browser with the client from
[SvelteKit](../sveltekit.md):

```svelte title="src/routes/posts/[id]/edit/+page.svelte"
<script lang="ts">
	import { RichTextEditor } from '@riducms/plugin-richtext/editor';
	import { ridu } from '$lib/ridu';

	let { data } = $props();
	const client = ridu.use();
	// The editor reads the document when it mounts and writes each edit back.
	// svelte-ignore state_referenced_locally
	let body = $state.raw(data.post.body ?? null);

	async function save() {
		await client.update('posts', data.post.id, { body });
	}
</script>

<RichTextEditor
	bind:value={body}
	label="Body"
	features={['links', 'lists']}
	toolbar="fixed"
	placeholder="Write what your readers should know"
/>
<button onclick={save}>Save</button>
```

Pass the same `features` as the Go field, such as `richtext.FeatureLinks` and
`richtext.FeatureLists` here. The editor then offers only what the field accepts, and every edit
it writes passes the field's validation. See [Rich text features](./features.md#features).

The editor reads `value` when it mounts. To replace the document from outside, such as after
discarding changes, remount the editor with a `{#key}` block. You can also pass `value` one way and
follow `onchange`, which receives each accepted document.

The editor opens text, headings, quotes, links, lists, code and dividers, within the `features` you
pass. A document holding anything else, such as uploads, relationships or blocks, which only the
admin can edit, shows a message and an **Export document JSON** button instead of the editor.

## Save it with a form {#form}

Give the editor a `name` to submit its document with a form, like a native input: it adds a hidden
field holding the document as JSON. Without JavaScript, the form submits the saved document.

```svelte title="src/routes/posts/[id]/edit/+page.svelte"
<form method="POST" use:enhance>
	<RichTextEditor
		bind:value={body}
		name="body"
		label="Body"
		features={['links', 'lists']}
	/>
	<button>Save</button>
</form>
```

```ts title="src/routes/posts/[id]/edit/+page.server.ts"
import { fail } from '@sveltejs/kit';
import { RiduError } from '@riducms/sdk';

export const actions = {
	default: async ({ locals, params, request }) => {
		const field = String(
			(await request.formData()).get('body') ?? ''
		);
		let body;
		try {
			// An editor with no document submits an empty field.
			body = field === '' ? null : JSON.parse(field);
		} catch {
			return fail(400, { issues: ["The body isn't JSON."] });
		}
		try {
			// Ridu checks the document against the field.
			await locals.ridu.update('posts', params.id, { body });
		} catch (error) {
			if (!(error instanceof RiduError)) throw error;
			return fail(error.status, {
				issues: error.issues.map((issue) => issue.message)
			});
		}
	}
};
```

## Turn Markdown into a document {#markdown}

`convertMarkdownToLexical` turns Markdown, such as a language model's answer or imported content,
into the document the editor's Markdown shortcuts would make. It runs on the server or in the
browser, without the editor:

```ts
import { convertMarkdownToLexical } from '@riducms/plugin-richtext/markdown';

const body = convertMarkdownToLexical(answer, {
	features: ['links', 'lists']
});
await locals.ridu.update('posts', id, { body });
```

Pass the Go field's `features`. Markdown for anything else, such as a code block in a field without
code, stays text, and so does a link with an unsafe URL. To show the document in an open editor,
remount the editor with a `{#key}` block, because it reads `value` when it mounts.

## Choose the toolbar {#toolbar}

`toolbar` sets where formatting controls appear:

- `"floating"`, the default, shows them over selected text.
- `"fixed"` pins them above the text, where they stay in view as the document scrolls.
- `"both"` does both.

`hideGutter`, `hideDraggableBlockElement`, `hideAddBlockButton` and `hideInsertParagraphAtEnd`
match the Go field's [layout options](./toolbar-and-layout.md). `readonly` shows the
document without editing controls, and `disabled` blocks editing for now, for example while your
app saves.

## Translate the editor {#translate}

The editor's buttons, menus and messages are in English, French and Arabic. Set `lang` to your
page's language, and `dir` for right-to-left text; other languages use English. Replace any
message with `messages`:

```svelte
<RichTextEditor
	bind:value={body}
	label="Note"
	lang="fr"
	messages={{ 'editor.placeholder': 'Écrivez une note' }}
/>
```

The editor reads `lang`, `dir` and `messages` when it mounts.

## Restyle the editor {#style}

The editor's colors, fonts and main sizes are `--ridu-richtext-*` custom properties. Each one
follows your app's theme when it uses the same names as shadcn, such as `--foreground`,
`--background`, `--border`, `--popover` and `--primary`, and otherwise falls back to a neutral
light theme. The editor's text inherits your page's font.

Set the properties on `:root` to restyle the editor, including its menus and dialogs. Write them
outside any `@layer`, such as Tailwind's `base`: the editor's defaults sit in its own cascade layer,
which beats layers your app declared before it.

```css title="src/app.css"
:root {
	--ridu-richtext-link: #b45309;
	--ridu-richtext-radius: 8px;
	--ridu-richtext-font: 'Source Serif 4', serif;
}
```

| Property                                                          | Styles                                                        |
| ----------------------------------------------------------------- | ------------------------------------------------------------- |
| `--ridu-richtext-text`, `-text-muted`, `-placeholder`             | Text, secondary text such as icons, and the placeholder       |
| `--ridu-richtext-link`, `-code`, `-danger`                        | Links, inline code, and error messages                        |
| `--ridu-richtext-background`, `-surface`                          | The fixed toolbar, and menus, popovers and code blocks        |
| `--ridu-richtext-surface-hover`, `-surface-active`                | Hovered and pressed controls                                  |
| `--ridu-richtext-border`, `-invalid`, `-accent`                   | Lines, the invalid state, and where a dragged block will land |
| `--ridu-richtext-radius`, `-shadow`                               | Corners, and the shadow under menus                           |
| `--ridu-richtext-focus`, `-focus-offset`                          | The keyboard focus outline                                    |
| `--ridu-richtext-font`, `-heading-font`, `-ui-font`, `-mono-font` | The text, headings, menus and toolbars, and code              |
| `--ridu-richtext-font-size`, `-line-height`                       | The text's size and spacing                                   |
| `--ridu-richtext-ui-font-size`, `-ui-line-height`                 | Menus and toolbars                                            |
| `--ridu-richtext-gutter`                                          | The space beside the text that holds each block's handle      |
| `--ridu-richtext-target`                                          | The size of toolbar buttons and menu options                  |
| `--ridu-richtext-sticky-offset`                                   | How far below the top the fixed toolbar sticks                |

If your app compiles Sass, configure the defaults instead. Each variable sets the property of the
same name:

```scss title="src/routes/posts/[id]/edit/theme.scss"
@use '@riducms/plugin-richtext/editor.scss' with (
	$radius: 10px,
	$background: #fffbf2
);
```

Custom properties set on an element around the editor style its text and fixed toolbar, but not
the floating toolbar, menus and dialogs, which render at the end of the page.

Inside the editor and its popups, your app's resets and global rules outside a cascade layer,
such as `ul { list-style: none }` or `[type='button'] { ... }`, are rolled back, so lists keep
their bullets under a reset such as UnoCSS's. To change one of the editor's rules, write yours in
Ridu's `app` layer, which comes after the editor's. Declare Ridu's layers after any stylesheet you
import, such as Tailwind's, so they follow its reset's layer whichever stylesheet loads first:

```css title="src/app.css"
@import 'tailwindcss';
@layer ridu, ridu-plugins, app;

@layer app {
	.post-form .ridu-richtext-editor blockquote {
		border-inline-start-color: #b45309;
	}
}
```

A class you pass to the editor styles its frame from anywhere.

## Render on the server {#ssr}

The editor is safe to render with SvelteKit's server rendering. On the server it renders the saved
document with the same styles, and the browser swaps in the editor without moving the page. On a
narrow screen, a fixed toolbar that wraps onto a second row adds that row when the editor mounts. To
show a saved document elsewhere, render it with [`RichText`](./display.md#frontend)
instead, which loads no editor.

The editor's JavaScript is about 205 KB compressed, and it loads when the editor first renders.
Keep it off pages that only display content.

## Use it on phones {#phones}

On a touch screen, toolbar buttons and menu options are 44 pixels, the size of a fingertip. A fixed
toolbar stays in view while the on-screen keyboard is open. Pass `hideGutter` on narrow screens to
give the text the full width, and set `--ridu-richtext-sticky-offset` to your header's height if it
stays at the top of the page.
