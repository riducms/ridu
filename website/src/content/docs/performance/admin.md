---
title: 'Admin performance'
description: 'Keep custom Svelte components responsive and control the JavaScript bundled into Ridu’s embedded admin.'
product: admin
eyebrow: 'Performance'
order: 233
aliases:
  [
    'custom component performance',
    'admin bundle',
    'Svelte performance'
  ]
navigation:
  section: 'Develop & operate'
  parent: performance
  order: 30
  title: 'Admin'
---

The built admin is static browser code embedded in the Go binary. A separate JavaScript server is
not required in production, but every application component and imported browser dependency still
has download, parse, and interaction cost. Measure the production build in a browser; development
hot reload is designed for feedback speed and is not a bundle-size result.

## Match the extension to the job {#configuration}

| Need                                    | Registration                                  | Performance boundary                                                                        |
| --------------------------------------- | --------------------------------------------- | ------------------------------------------------------------------------------------------- |
| Change one form control                 | `fields['app:name'] = defineFieldEditor(...)` | Runs with that field occurrence; keep input handling local.                                 |
| Summarize a repeated row                | `rowLabels['app:name'] = defineRowLabel(...)` | Can rerender while unsaved row values change; avoid scanning unrelated rows.                |
| Format a collection cell                | `listCells[]`                                 | Runs for every visible cell; render supplied data instead of fetching per cell.             |
| Add document UI                         | `documentActions[]` or `documentViews[]`      | Receives one saved document; call the SDK only for a deliberate user action or needed view. |
| Add application navigation/page content | `navigation[]`, `dashboard[]`, or `routes[]`  | Statically registered and included in the application admin build.                          |
| Share Svelte context                    | `providers[]`                                 | Wraps the complete admin; do only setup that every screen needs.                            |

Use the smallest extension surface that owns the behavior. A provider that fetches data for one
page runs more broadly than a request started by that page. A list cell that makes its own request
multiplies calls by visible rows; include needed data in the list selection or use a custom view
when the interaction needs a separate query.

## Keep field updates local {#fields}

A field editor receives a stable binding for its current occurrence. Read `field.value`, update it
with `field.set(...)`, and use `form.get(path)` only for values the control actually depends on.
Do not mirror the complete form into another state object or rebuild every row on each keystroke.

| Field API              | Use                                                                                          |
| ---------------------- | -------------------------------------------------------------------------------------------- |
| `field.value`          | Current unsaved value for this occurrence.                                                   |
| `field.set(value)`     | Update this field through the form controller.                                               |
| `field.inputProps`     | Connect the control to its label, description, and validation messages.                      |
| `field.readOnly`       | Disable or mark the control read-only when editing is not allowed.                           |
| `form.get(path)`       | Read one other unsaved field when the control depends on it.                                 |
| `field.stale`          | Discard an asynchronous result after navigation, row removal, locale change, save, or reset. |
| `field.liveValidation` | Display Ridu's current advisory server-check state.                                          |

The built-in live validation controller cancels obsolete requests. For a custom request, keep an
`AbortController`, abort it when the input changes or the component is destroyed, and check
`field.stale` before applying the response. Trigger expensive requests after a short pause, on
blur, or from a button instead of on every keypress.

## Avoid per-row network requests {#lists}

List cells, row labels, and repeated field components may appear many times. Use data already
provided in props for display. When a screen needs related data, prefer one bounded SDK request and
index the result by document ID rather than starting one request from every component instance.

Relationship and upload controls already use paginated, access-checked browsers. Keep option
queries bounded and filter on the server; do not download the whole target collection to filter it
in Svelte.

## Control browser dependencies {#bundle}

Imports in `admin/src/admin.config.ts` and the components it references are part of the static
admin module graph. Before adding a large editor, charting library, date package, or SDK, check
whether browser APIs or `@riducms/ui` already cover the task. Import the focused module you use
rather than a package-wide namespace when the dependency supports it.

Build the same artifact you will deploy:

```sh title="terminal"
bun run build
```

Inspect emitted JS/CSS sizes and test the route that loads the extension with browser performance
tools. Record compressed transfer, parse/evaluation time, long tasks, and the interaction you care
about. The Go binary size includes embedded assets, but binary size alone cannot tell you how much
JavaScript a browser downloads for one route.

## Test realistic author interactions {#measure}

Use documents large enough to expose repeated-field behavior and target collections large enough
to exercise picker pagination. Measure typing, opening drawers, reordering, saving, switching
locales, and loading rich text separately; one fast initial HTML response does not prove those
interactions are fast.

Run the application checks after changing an extension, then use
[Measure performance](/docs/performance/measurement/) for comparable browser and server results.
The repository's controlled browser thresholds run separately in `make performance-check`.
