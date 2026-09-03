# `@riducms/ui`

Svelte interaction components for the Ridu admin and admin plugins. They wrap Bits UI behavior and
apply Ridu's theme tokens.

Import components and shared class/variant composition by name:

```ts
import {
	CommandInput,
	CommandRoot,
	ConfirmationDialog,
	PopoverContent,
	PopoverRoot,
	tv,
} from "@riducms/ui";
```

Keep every part of a compound primitive, such as `CommandRoot`, `CommandList`, and `CommandItem`,
behind this package boundary. Mixing wrappers from separate Bits UI installations can split their
Svelte context identity.

Use `ConfirmationDialog` for plugin confirmations instead of browser modal APIs. It focuses
the cancel action first, prevents dismissal while an asynchronous confirmation is pending, and
requires the consuming admin/plugin to supply localized labels and description text.

Import `tv()`, `cn()`, and their types from `@riducms/ui` so the admin and plugins use the same class
composition runtime.
