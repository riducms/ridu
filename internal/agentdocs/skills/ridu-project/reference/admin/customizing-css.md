<!-- Generated from website/src/content/docs/admin/customizing-css.md by scripts/sync-agent-docs.ts. -->

# Customizing CSS

Change the admin's colors, typography, spacing, and built-in controls with a stylesheet in your
application. Ridu exposes CSS variables and named classes, so you can style the existing screens
without replacing their Svelte components.

Use [custom components](../custom-components.md) when you need different content or behavior,
such as a new dashboard panel or field editor.

## Add your stylesheet {#global-css}

Create `admin/src/custom.css`. This example rounds the controls and dashboard cards and makes
the login form wider:

```css title="admin/src/custom.css"
@import '@riducms/ui/layers.css';

@layer app {
	:root {
		--control-radius: 6px;
	}

	.ridu-auth {
		--auth-width: 520px;
	}

	.ridu-dashboard__card {
		border-radius: 10px;
	}
}
```

Import it from the admin entry:

```ts title="admin/src/main.ts" add={8}
import { mountAdmin } from '@riducms/admin';
import {
	createClient,
	type RiduConfig
} from '../../generated/ridu.generated';

import adminConfig from '@/admin.config';
import './custom.css';

const target = document.getElementById('app');
if (target === null) throw new Error('Missing admin mount element.');

mountAdmin<RiduConfig>({
	target,
	clientFactory: () =>
		createClient({ baseURL: window.location.origin }),
	...adminConfig
});
```

Run `ridu dev` and open `/admin` to see your changes. The entry already loads Ridu's base theme
through `@riducms/admin`. Your stylesheet is bundled into the admin when you run `ridu build`.

## Override built-in styles {#specificity}

Ridu puts its styles in CSS **cascade layers**: named groups that determine which styles take
precedence. For normal declarations, the order from lowest to highest is:

1. `ridu` — the framework's reset, theme, base styles, components, and utilities.
2. `ridu-plugins` — styles added by plugins.
3. `app` — your application's overrides.
4. Unlayered CSS — ordinary rules outside any `@layer`.

The `layers.css` import establishes this order before components load. Use `@layer app` as in
the example above, or write ordinary unlayered CSS. A selector in `app` can override a more
specific framework selector without adding `!important`. Within the same layer, normal CSS
specificity and source order apply. Inline styles and `!important` declarations follow their
own cascade rules.

Keep global overrides in `custom.css`. For a custom Svelte component, put its own presentation
in a `<style>` block or import a stylesheet beside the component.

## Change colors and typography {#theme-variables}

CSS variables let many components share the same value. Set them on `:root` to change the whole
admin, including menus and dialogs rendered outside the current page.

For example, give primary actions different colors in each theme:

```css title="admin/src/custom.css"
@import '@riducms/ui/layers.css';

@layer app {
	:root {
		--font-sans: 'Trebuchet MS', sans-serif;
		--control-radius: 6px;

		/* Dark theme: keep text readable on the new action color. */
		--primary: #a5b4fc;
		--primary-foreground: #1e1b4b;
		--accent-hover: #c7d2fe;
	}

	:root[data-theme='light'] {
		--primary: #4338ca;
		--primary-foreground: #ffffff;
		--accent-hover: #3730a3;
	}
}
```

The admin applies `data-theme="light"` or `data-theme="dark"` to the root `<html>` element,
including when the user chooses the system theme. Ridu's default variables adapt to that choice.
Your literal color overrides need their own light and dark values when the same color would
be unreadable in one theme. Check both from the account settings.

### Colors {#colors}

<dl class="doc-option-list">
  <div>
    <dt><code>--background</code> and <code>--foreground</code></dt>
    <dd>Page background and main text.</dd>
  </div>
  <div>
    <dt><code>--background-surface</code> and <code>--background-layer</code></dt>
    <dd>Cards, controls, and raised surfaces.</dd>
  </div>
  <div>
    <dt><code>--foreground-muted</code></dt>
    <dd>Secondary text.</dd>
  </div>
  <div>
    <dt><code>--primary</code> and <code>--primary-foreground</code></dt>
    <dd>Primary action background and text. Set both so text stays readable.</dd>
  </div>
  <div>
    <dt><code>--accent-hover</code></dt>
    <dd>Primary action hover color.</dd>
  </div>
  <div>
    <dt><code>--control-surface</code> and <code>--control-border</code></dt>
    <dd>Input backgrounds and borders. Use <code>--control-surface-hover</code> and <code>--control-border-hover</code> for hover states.</dd>
  </div>
  <div>
    <dt><code>--field-error-surface</code> and <code>--field-error-foreground</code></dt>
    <dd>Field error bubble background and text.</dd>
  </div>
  <div>
    <dt><code>--focus-outline</code> and <code>--focus-outline-offset</code></dt>
    <dd>Keyboard focus outline and its distance from the control.</dd>
  </div>
</dl>

### Fonts and control sizes {#sizes}

<dl class="doc-option-list">
  <div>
    <dt><code>--font-sans</code></dt>
    <dd>Admin text and controls. Defaults to the system sans-serif stack.</dd>
  </div>
  <div>
    <dt><code>--font-mono</code></dt>
    <dd>Code and structured values. Defaults to the system monospace stack.</dd>
  </div>
  <div>
    <dt><code>--font-richtext-body</code></dt>
    <dd>Rich-text editor content. Defaults to a serif stack beginning with Georgia.</dd>
  </div>
  <div>
    <dt><code>--font-size-body</code> and <code>--line-height-body</code></dt>
    <dd>Controls and admin areas that use the body-size variables. Defaults to <code>13px</code> text with a <code>20px</code> line height.</dd>
  </div>
  <div>
    <dt><code>--control-height</code></dt>
    <dd>Inputs and large buttons. Defaults to <code>40px</code>.</dd>
  </div>
  <div>
    <dt><code>--control-radius</code></dt>
    <dd>Input and button corners. Defaults to <code>3px</code>.</dd>
  </div>
  <div>
    <dt><code>--control-padding-inline</code></dt>
    <dd>Horizontal padding in controls. Defaults to <code>15px</code>.</dd>
  </div>
  <div>
    <dt><code>--admin-nav-width</code></dt>
    <dd>Desktop navigation width. Defaults to <code>275px</code>.</dd>
  </div>
  <div>
    <dt><code>--admin-gutter</code></dt>
    <dd>Page gutters. Defaults to <code>60px</code> on desktop, <code>40px</code> at 1024px, and <code>16px</code> at 768px.</dd>
  </div>
  <div>
    <dt><code>--admin-sticky-offset</code></dt>
    <dd>Set by the document editor to the height of its sticky action bar. Use it as <code>top</code> for your own sticky content so it stays below the bar. It is <code>0px</code> in the live-preview editor pane, which scrolls on its own.</dd>
  </div>
</dl>

The installed `@riducms/ui/src/theme.css` contains the complete theme. Feature styles can set more local values:
for example, set `--auth-width` on `.ridu-auth`, where the login layout defines it. A value set
directly on an element takes precedence over a value inherited from an ancestor, even when the
ancestor's rule is in a higher layer.

## Style one part of the admin {#component-classes}

Inspect the element in your browser and use its named `ridu-` class. Classes such as
`.ridu-dashboard__card` identify a component part; modifiers such as `.ridu-button--primary`
identify a variant. Use these names instead of generated `svelte-…` hashes or utility classes.

For example, keep the rest of the theme and add an accent to the active navigation link:

```css title="admin/src/custom.css"
/* Inside your existing @layer app block. */
.ridu-nav__link--active {
	color: var(--primary);
}

.ridu-nav__link--active::before {
	background: currentColor;
}
```

Useful starting selectors include:

<dl class="doc-option-list">
  <div>
    <dt>Login and recovery</dt>
    <dd><code>.ridu-auth</code>, <code>.ridu-auth__content</code>, <code>.ridu-auth__heading</code></dd>
  </div>
  <div>
    <dt>Navigation</dt>
    <dd><code>.ridu-nav__link</code>, <code>.ridu-nav__link--active</code></dd>
  </div>
  <div>
    <dt>Dashboard</dt>
    <dd><code>.ridu-dashboard</code>, <code>.ridu-dashboard__card</code></dd>
  </div>
  <div>
    <dt>Collection list</dt>
    <dd><code>.ridu-list</code>, <code>.ridu-list-results</code>, <code>.ridu-list-table__scroll</code></dd>
  </div>
  <div>
    <dt>Document editor</dt>
    <dd><code>.ridu-document</code>, <code>.ridu-document-header</code>, <code>.ridu-document-fields</code></dd>
  </div>
  <div>
    <dt>Inputs and buttons</dt>
    <dd><code>.ridu-input</code>, <code>.ridu-textarea</code>, <code>.ridu-button</code></dd>
  </div>
  <div>
    <dt>Field labels and feedback</dt>
    <dd><code>.ridu-field-label</code>, <code>.ridu-field-help</code>, <code>.ridu-field-error-tooltip</code></dd>
  </div>
  <div>
    <dt>Rich text</dt>
    <dd><code>.ridu-richtext-content</code>, <code>.ridu-richtext-floating-toolbar</code></dd>
  </div>
  <div>
    <dt>SEO</dt>
    <dd><code>.ridu-seo-overview</code>, <code>.ridu-seo-field</code>, <code>.ridu-seo-preview</code></dd>
  </div>
</dl>

Field wrappers also expose `data-field-path`. Use the path shown in the DOM to narrow an override
to an individual field:

```css title="admin/src/custom.css"
/* Inside your existing @layer app block. */
[data-field-path='title'] .ridu-input {
	font-weight: 600;
}
```

This matches the `title` field wherever that path appears. Use a wrapper class from a
[custom view](../custom-components/custom-views.md) when the change belongs to only one collection.

Menus and drawers may be mounted outside that field wrapper. Target their own classes for popup
presentation; use `:root` variables when the value should apply throughout the admin.

## Use Sass in your own components {#scss}

Plain CSS is enough for theme overrides. If you prefer Sass, rename the file to `custom.scss` and
change the entry import to `import './custom.scss'`. Generated projects use
`@riducms/build`, which already includes the Sass compiler.

Import `@riducms/ui/styles.scss` with `@use` to reuse control styles and breakpoints. It also
declares Ridu's layer order, so this file needs no separate `layers.css` import:

```scss title="admin/src/custom.scss"
@use '@riducms/ui/styles.scss' as ui;

@layer app {
	.editor-note {
		@include ui.control-surface;
		padding: 12px var(--control-padding-inline);
		color: var(--foreground);
	}

	@media (max-width: ui.$small) {
		.editor-note {
			padding: 10px;
		}
	}
}
```

Apply `class="editor-note"` to an element in your component. The mixin adds its border, radius,
background, and shadow. It does not create an input or supply keyboard behavior; use the
[UI components](https://riducms.com/reference/ui/) for interactive controls.

<dl class="doc-option-list">
  <div>
    <dt><code>control-surface</code></dt>
    <dd>Shared resting control border, radius, background, and shadow.</dd>
  </div>
  <div>
    <dt><code>focus-outline($offset: var(--focus-outline-offset))</code></dt>
    <dd>Outline declarations to include inside your <code>:focus-visible</code> rule. Pass an offset to change its distance from the control.</dd>
  </div>
  <div>
    <dt><code>visually-hidden</code></dt>
    <dd>Hide content visually while keeping it available to assistive technology.</dd>
  </div>
  <div>
    <dt><code>confirmation-overlay</code>, <code>confirmation-content</code>, <code>confirmation-title</code></dt>
    <dd>Shared confirmation dialog presentation. Use <code>ConfirmationDialog</code> when you also need its behavior.</dd>
  </div>
  <div>
    <dt><code>$small</code>, <code>$medium</code>, <code>$large</code></dt>
    <dd>Inclusive <code>max-width</code> breakpoints: <code>768px</code>, <code>1024px</code>, and <code>1440px</code>.</dd>
  </div>
</dl>

These Sass definitions reuse the runtime CSS variables. To change a color or size, override the
variable in CSS; there is no separate Sass theme to configure.

## If an override does not appear {#troubleshooting}

- Check that `admin/src/main.ts` imports the stylesheet and that the selector matches the rendered
  element. Creating a file alone does not load it.
- Put application rules in `app` or leave them unlayered. Import `layers.css` before declaring
  custom layers; the Sass entry does this for you.
- Check the element's computed styles. A local variable, inline style, or another application rule
  may be winning. Put a local variable on the component that defines it.
- Svelte `<style>` blocks are scoped to their component. Use the imported global stylesheet to
  target built-in admin elements.
- Check light and dark mode and a narrow viewport. If you override `--admin-gutter` at `:root`,
  add your own media queries to retain smaller gutters on mobile.
