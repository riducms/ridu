# `@riducms/translations`

Typed interface-language catalogs, locale negotiation, plural selection, and `Intl` formatting for
the Ridu admin and statically registered admin plugins. It translates the authoring interface;
content localization is configured separately in executable Go config.

Generated projects can expose the built-in catalogs through their thin admin entry:

```ts
import { mountAdmin } from "@riducms/admin";
import { ar, en, fr } from "@riducms/translations";

mountAdmin({
	target,
	clientFactory,
	languages: [en, fr, ar],
});
```

The Go `AdminLocalization` declaration and these static imports must agree. Missing languages,
catalog keys, or changed interpolation placeholders fail validation/build instead of silently
falling back. Use `defineTranslationLanguage` for a complete custom language and
`extendTranslationLanguage` to extend an existing one.

See [Localization](../../website/src/content/docs/localization.md) for interface and content locale
configuration, fallback behavior, SDK requests, migration implications, and current limits.
