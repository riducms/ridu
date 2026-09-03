# `@riducms/plugin`

Svelte and TypeScript APIs for Ridu admin plugins. Import field props, extension types, and
`defineFieldPlugin` from this package.

```ts
import { defineFieldPlugin, type FieldComponentProps } from "@riducms/plugin";
```

`defineAdminPlugin` also exposes collision-checked static extension surfaces for login, profile,
security, navigation, logout, dashboard, routes, list cells, document actions, and document views.
Core-view replacements receive a `defaultView` snippet so a plugin can wrap the existing screen.
Auth and account components receive hosts for login, logout, identity refresh, and notifications.

Collection list/create/edit, global, and not-found replacements use the same model. They can be
registered for one resource or as a surface-wide fallback; exact resource registrations win over
fallbacks. Their host exposes manifest refresh, document-change invalidation, and notifications,
and application data remains available through the generated SDK.

Fine-grained shell registrations cover login/navigation graphics, the account avatar, global
header and action slots, account settings items, and ordered provider wrappers. Replacement
graphics allow one registration per surface; additive shell components and providers keep plugin
order.

A paired backend plugin may also assign an exact `componentKey` to a renderer for a built-in field
type. These renderers keep the core field's storage semantics and receive the public form resource,
content locale, a detached current-form `snapshot()`, and the narrow `requestPlugin` authoring host.
That host calls only the paired backend plugin's namespaced endpoints.

Arrays and blocks may also select an exact paired-plugin row-label component. Register it with
`defineRowLabelPlugin({ key, componentKey, component })` and author the matching Go option with
`field.RowLabelComponent`. The component receives the manifest field, a detached deeply frozen row
snapshot, its 1-based visual `rowNumber`, current i18n, and deterministic JSON config. Registration
identities are collision checked before mount, and a manifest that selects a missing
plugin/component fails with a field-specific diagnostic. The legacy single-child
`field.RowLabel(path)` behavior remains available for arrays.

Use `@riducms/ui` for shared interaction components and the generated SDK for application data.
