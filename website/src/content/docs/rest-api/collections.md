---
title: 'Collections, globals, and files'
description: 'Use Ridu’s collection, global, trash, version, publishing, scheduling, and upload routes.'
product: data
eyebrow: 'REST API'
order: 101
navigation:
  section: 'Work with data'
  parent: 'rest-api'
  order: 10
  title: 'Collections, globals, and files'
---

Collection routes use the authored collection slug as `{collection}` and the public document ID as
`{id}`. Ridu only exposes routes supported by that resource’s configuration.

## Read and write collections {#collections}

| Method and path                                          | What it does                                                                   |
| -------------------------------------------------------- | ------------------------------------------------------------------------------ |
| `GET /api/collections/{collection}`                      | List access-checked documents.                                                 |
| `POST /api/collections/{collection}`                     | Create an ordinary JSON document or upload document.                           |
| `GET /api/collections/{collection}/{id}`                 | Read one document.                                                             |
| `PATCH /api/collections/{collection}/{id}`               | Update one document; accepts `If-Match`.                                       |
| `DELETE /api/collections/{collection}/{id}`              | Delete one document or move it to trash when enabled.                          |
| `GET /api/collections/{collection}/count`                | Count documents with the same filter, locale, and trash context as list.       |
| `POST /api/collections/{collection}/{id}/duplicate`      | Duplicate an ordinary document with optional field overrides.                  |
| `POST /api/collections/{collection}/{id}/copy-locale`    | Copy localized values with `{ "from": "en", "to": "fr" }`; accepts `If-Match`. |
| `PATCH /api/collections/{collection}/{id}/joins/{field}` | Add or remove targets from a writable inverse join.                            |

An auth collection creates users through `/api/auth/{collection}/create-user`; its ordinary
collection `POST` route is unavailable. An upload collection accepts multipart input on its
collection `POST` route. See the relevant sections below before choosing a request body.

`If-Match` accepts a quoted or unquoted positive revision. Use the last revision read by the client;
Ridu returns a conflict instead of silently replacing a newer edit.

## Run one action for several documents {#bulk}

Send `POST /api/collections/{collection}/bulk` with 1–100 unique, non-empty IDs:

```json title="request.json"
{
	"action": "update",
	"ids": ["post-1", "post-2"],
	"data": {
		"status": "archived"
	}
}
```

| `action`          | Required input and result                           |
| ----------------- | --------------------------------------------------- |
| `update`          | Requires `data`; updates every admitted document.   |
| `publish`         | Publishes every admitted versioned document.        |
| `unpublish`       | Returns every admitted versioned document to draft. |
| `delete`          | Deletes or trashes every admitted document.         |
| `restoreDeleted`  | Restores every admitted trashed document.           |
| `deletePermanent` | Permanently deletes every admitted document.        |

Capability and access checks run for each document. A bulk request groups work; it does not bypass
authorization or validation.

## Work with trash {#trash}

These routes exist only for a trash-enabled collection:

| Method and path                                           | What it does                               |
| --------------------------------------------------------- | ------------------------------------------ |
| `GET /api/collections/{collection}?trash=true`            | List only trashed documents.               |
| `POST /api/collections/{collection}/{id}/restore-deleted` | Restore one document.                      |
| `DELETE /api/collections/{collection}/{id}/permanent`     | Permanently delete one document.           |
| `DELETE /api/collections/{collection}/trash`              | Permanently empty this collection’s trash. |

See [Bulk operations and trash](/docs/bulk-and-trash/) for UI behavior, retention, and irreversible
deletion rules.

## Publish, schedule, and restore versions {#versions}

These routes exist only when collection versions are enabled. Publish, unpublish, restore, and
schedule operations accept `If-Match`.

| Method and path                                              | What it does                                                    |
| ------------------------------------------------------------ | --------------------------------------------------------------- |
| `GET /api/collections/{collection}/{id}/versions`            | List stored snapshots.                                          |
| `GET /api/collections/{collection}/{id}/versions/{revision}` | Read one positive integer revision.                             |
| `POST /api/collections/{collection}/{id}/publish`            | Publish the current document.                                   |
| `POST /api/collections/{collection}/{id}/unpublish`          | Return the current document to draft.                           |
| `POST /api/collections/{collection}/{id}/restore/{revision}` | Restore a snapshot; add `?draft=true` to restore it as a draft. |
| `GET /api/collections/{collection}/{id}/schedule`            | List scheduled publication jobs.                                |
| `POST /api/collections/{collection}/{id}/schedule`           | Schedule publication with `{ "runAt": "…" }`.                   |
| `DELETE /api/collections/{collection}/{id}/schedule/{jobId}` | Cancel a scheduled publication.                                 |

See [Drafts and versions](/docs/drafts-and-versions/) for the status model and scheduling lifecycle.

## Upload and serve files {#uploads}

An upload-enabled collection adds these transports:

| Method and path                                    | Input or result                                                                   |
| -------------------------------------------------- | --------------------------------------------------------------------------------- |
| `POST /api/collections/{collection}`               | `multipart/form-data` with required `file` and optional `data` JSON-object field. |
| `POST /api/collections/{collection}/remote-upload` | JSON `{ "url": "https://…", "data": { … } }`.                                     |
| `PATCH /api/collections/{collection}/{id}/image`   | JSON focal point and optional crop coordinates; accepts `If-Match`.               |
| `GET /api/uploads/{collection}/{object-key}`       | Return an access-checked object body.                                             |
| `HEAD /api/uploads/{collection}/{object-key}`      | Return the same object metadata without the body.                                 |

Multipart requests are limited to the collection’s maximum file size plus framing allowance.
Remote-host, redirect, MIME, image-dimension, spool, and storage rules come from the upload
configuration. Object responses are private and non-cacheable; HTML, SVG, and XML-like active
content is forced to download with a sandbox policy. See [Uploads](/docs/uploads/) for complete
examples and limits.

## Read and update globals {#globals}

| Method and path                                 | What it does                                   |
| ----------------------------------------------- | ---------------------------------------------- |
| `GET /api/globals/{global}`                     | Read the access-checked global.                |
| `PATCH /api/globals/{global}`                   | Update the global; accepts `If-Match`.         |
| `POST /api/globals/{global}/copy-locale`        | Copy localized values; accepts `If-Match`.     |
| `GET /api/globals/{global}/versions`            | List global versions.                          |
| `GET /api/globals/{global}/versions/{revision}` | Read one global revision.                      |
| `POST /api/globals/{global}/publish`            | Publish a versioned global.                    |
| `POST /api/globals/{global}/unpublish`          | Return a versioned global to draft.            |
| `POST /api/globals/{global}/restore/{revision}` | Restore a global revision; accepts `If-Match`. |

Version routes are absent when the global does not enable versions. Global scheduled publishing is
not implemented.

## Inspect schema and process health {#schema-health}

| Method and path   | What it does                                                               |
| ----------------- | -------------------------------------------------------------------------- |
| `GET /api/schema` | Read the canonical public manifest envelope used by clients and the admin. |
| `GET /healthz`    | Check process liveness.                                                    |
| `GET /readyz`     | Check configured dependencies and migration readiness.                     |

The public manifest describes fields and capabilities; it never contains executable access rules or
secrets. For exact request and response schemas, inspect `generated/ridu.openapi.json`.
