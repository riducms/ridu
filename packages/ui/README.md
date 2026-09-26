# `@riducms/ui`

Svelte interaction components for the Ridu admin and admin plugins. They wrap Bits UI behavior and
apply Ridu's theme tokens.

Import components by name:

```ts
import { CommandInput, CommandRoot, ConfirmationDialog, PopoverContent, PopoverRoot, cv, type VariantProps } from "@riducms/ui";
```

Keep every part of a compound primitive, such as `CommandRoot`, `CommandList`, and `CommandItem`,
behind this package boundary. Mixing wrappers from separate Bits UI installations can split their
Svelte context identity.

Use `ConfirmationDialog` for plugin confirmations instead of browser modal APIs. It focuses
the cancel action first, prevents dismissal while an asynchronous confirmation is pending, and
requires the consuming admin/plugin to supply localized labels and description text.

Compose conditional classes with Svelte's native class arrays or objects. Use `cv()`
for reusable semantic variants:

```ts


const notice = cv({
	base: "notice",
	variants: {
		tone: { neutral: "notice--neutral", warning: "notice--warning" },
	},
	defaultVariants: { tone: "neutral" },
});

type NoticeTone = VariantProps<typeof notice>["tone"];
notice({ tone: "warning", class: "app-notice" });
```

The helper supports a base string, named string choices, optional defaults, inferred prop types,
and an optional additional `class` string. Omitted or `undefined` selections use defaults; absent
defaults and empty class choices add nothing. `class` is reserved and cannot be a variant axis.
Compose class arrays or objects at the Svelte boundary: `class={[notice({ tone }), className]}`.
CSS layers and specificity govern overrides. The helper has no utility merging, slots, compound
variants, inheritance, or dependency on a styling runtime.

`buttonVariants()` uses this same contract for links and third-party triggers; `Button` itself
accepts Svelte's native class values. `cv()` is the only variant helper; utility conflict merging
and the `tailwind-variants` dependency have been removed.

## Styling

Import `@riducms/ui/theme.css` once at your admin entry. The Ridu admin already does this. Shared
input/button/field styles are imported by their components and compile to static CSS.
`PasswordInput` supplies a bound string value, standard input attributes, and required localized
`showLabel`/`hideLabel` props. Its value and autocomplete behavior belong to the native input.

The `ridu` layer orders reset, theme, base, components, and transitional utilities. Plugins can
use `ridu-plugins`; applications can use `app` or ordinary unlayered CSS. Import
`@riducms/ui/layers.css` before declaring a framework/plugin layer, including in lazy stylesheets.
Application authors can customize the admin with plain CSS:

```css
@import "@riducms/ui/layers.css";

@layer app {
	:root {
		--control-radius: 6px;
		--control-height: 44px;
	}
	.ridu-auth {
		--auth-width: 520px;
	}
	.ridu-input {
		border-color: var(--foreground-muted);
	}
}
```

Public classes include `.ridu-auth`, `.ridu-auth__content`, `.ridu-auth__brand`,
`.ridu-auth__heading`, `.ridu-auth__submit`, `.ridu-input`, `.ridu-password-input`,
`.ridu-password-input__toggle`, `.ridu-button`, `.ridu-field`, `.ridu-field-label`,
`.ridu-field-help`, `.ridu-field-error`, and `.ridu-field-error-tooltip`. Error bubbles use
`--field-error-surface` and `--field-error-foreground`. Prefer semantic variables for colors and geometry;
do not target generated Svelte hashes. Public UI controls, the main admin workflows, and the
rich-text and SEO plugins use named component classes. See the
[Customizing CSS guide](../../website/src/content/docs/admin/customizing-css.md) for stylesheet
registration, light and dark themes, and selectors for individual screens.

### Sass authoring

`@riducms/ui/styles.scss` exports the shared Sass definitions and includes the layer-order
declarations. One import establishes the cascade order before your rules. In Ridu's Vite build:

```scss
@use "@riducms/ui/styles.scss" as ui;

@layer ridu-plugins {
	.plugin-control {
		@include ui.control-surface;
		padding: 8px var(--control-padding-inline);

		&:focus-visible {
			@include ui.focus-outline;
		}
	}

	@media (max-width: ui.$small) {
		.plugin-control {
			width: 100%;
		}
	}
}
```

For standalone Dart Sass, enable its `NodePackageImporter` (CLI: `--pkg-importer=node`) and use
`pkg:@riducms/ui/styles.scss` instead. Bare package resolution in the example is supplied by Vite.
Plain CSS consumers continue to import `@riducms/ui/layers.css` directly. The Sass entry emits only
the layer-order declarations; component rules are emitted where their mixins are included.

- `control-surface` supplies the shared resting border, radius, background and shadow. Geometry,
  text and hover/focus/invalid/disabled behavior stay with the consuming control.
- `focus-outline($offset: var(--focus-outline-offset))` supplies outline declarations inside the
  caller's focus selector. Components retain their own focus rules so they also work outside the
  admin's global reset. Inputs retain their separate border-only focus treatment.
- `$small`, `$medium` and `$large` are the existing inclusive layout boundaries: 768px, 1024px and
  1440px. Use ordinary media queries; the shell's desktop minimum remains `$small + 1px`.
  Content-specific boundaries stay local.

Keep full semantic class names searchable; nest related states and shallow descendants without
changing selector specificity or cascade order. Reuse shared declarations for a common design
rule, not simply because two components happen to have the same dimensions. Add no theme values
or configurable component variants to Sass: runtime CSS variables and `cv()` retain those roles.
