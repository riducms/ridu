---
title: 'Localize the admin and authoring workflow'
description: 'Configure interface catalogs, translated labels, RTL, timezones, and a separate content-locale workflow.'
product: admin
eyebrow: 'Admin tasks'
order: 126
aliases:
  ['admin translation', 'interface language', 'admin timezone', 'RTL admin', 'localized labels']
capabilities: ['content.localization']
navigation:
  section: 'Admin & workflows'
  parent: admin
  group: 'Coordinate work'
  order: 60
  title: 'Localization'
---

Ridu separates two choices: the **content locale** selects stored/fallback values, while the
**interface language** translates admin controls and application labels. An editor can author
Arabic content with the French interface and a Europe/Paris display timezone.

## Configure interface choices {#configure}

Declare languages/timezones in `Admin.Localization` and pass matching static catalogs to
`mountAdmin`. Ridu publishes complete English, French, and Arabic catalogs in
`@riducms/translations`:

```ts title="admin/src/main.ts"
import { ar, en, fr } from '@riducms/translations';

mountAdmin<RiduConfig>({
	target,
	clientFactory: () => createClient({ baseURL: window.location.origin }),
	plugins: adminPlugins,
	languages: [en, fr, ar]
});
```

The matching Go config declares canonical language codes, labels, RTL state, IANA timezone IDs,
and defaults. A configured language without a matching static catalog—or conflicting RTL metadata—
fails closed. Authors persist interface language and timezone in their account preferences.

Application name, collections/globals, fields, choices, blocks, tabs, row labels, groups, and
timezones support typed translated labels. Plugin messages use namespaced build-validated catalogs.
Dates, numbers, plurals, and relative time use the active language/timezone through `Intl`.

## Author localized content {#content-locale}

The global content-locale switcher controls exact/fallback values, list filters, relationships,
versions, and copy-locale operations. The admin shows the fallback source, protects dirty changes
during a locale switch, and uses RTL editing direction where configured. Interface language never
changes stored content, and content locale never changes interface copy automatically.

## Verify the workflow {#verify}

1. switch English/French/Arabic interface catalogs and confirm direction/labels;
2. switch timezones and confirm one stored timestamp formats differently without mutating it;
3. edit one localized field in two content locales and verify the fallback source;
4. reload/sign in again and confirm account preferences; and
5. read exact and `locale: 'all'` shapes through the generated SDK.

The complete Go configuration, catalog installation, fallback rules, APIs, and limitations are in
[Content localization](/docs/localization/#admin-language).
