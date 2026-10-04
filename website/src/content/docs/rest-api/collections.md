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

<dl class="doc-option-list">
  <div>
    <dt><code>GET /api/collections/{collection}</code></dt>
    <dd>List access-checked documents.</dd>
  </div>
  <div>
    <dt><code>POST /api/collections/{collection}</code></dt>
    <dd>Create an ordinary JSON document or upload document.</dd>
  </div>
  <div>
    <dt><code>GET /api/collections/{collection}/{id}</code></dt>
    <dd>Read one document.</dd>
  </div>
  <div>
    <dt><code>PATCH /api/collections/{collection}/{id}</code></dt>
    <dd>Update one document; accepts <code>If-Match</code>.</dd>
  </div>
  <div>
    <dt><code>DELETE /api/collections/{collection}/{id}</code></dt>
    <dd>Delete one document or move it to trash when enabled.</dd>
  </div>
  <div>
    <dt><code>GET /api/collections/{collection}/count</code></dt>
    <dd>Count documents with the same filter, locale, and trash context as list.</dd>
  </div>
  <div>
    <dt><code>POST /api/collections/{collection}/{id}/duplicate</code></dt>
    <dd>Duplicate an ordinary document with optional field overrides.</dd>
  </div>
  <div>
    <dt><code>POST /api/collections/{collection}/{id}/copy-locale</code></dt>
    <dd>Copy localized values with <code>{ "from": "en", "to": "fr" }</code>; accepts <code>If-Match</code>.</dd>
  </div>
  <div>
    <dt><code>PATCH /api/collections/{collection}/{id}/joins/{field}</code></dt>
    <dd>Add or remove targets from a writable inverse join.</dd>
  </div>
</dl>

An auth collection creates users through `/api/auth/{collection}/create-user`; its ordinary
collection `POST` route is unavailable. An upload collection accepts multipart input on its
collection `POST` route. See the relevant sections below before choosing a request body.

`If-Match` accepts a quoted or unquoted positive revision. Use the last revision read by the client;
Ridu returns a conflict instead of silently replacing a newer edit.

To inspect one saved collection document or global in the admin, open its **API** tab. It shows the
current GET URL and response; change the content locale, relationship depth, or authenticated
setting to rerun the read. The tab reads stored data, so unsaved form changes are not part of its
response. Copy the URL, or for a collection open **API reference** for list, read, create, update,
and delete examples in TypeScript, cURL, and Go. The generated OpenAPI file remains the exact route
contract.

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

<dl class="doc-option-list">
  <div>
    <dt><code>GET /api/collections/{collection}?trash=true</code></dt>
    <dd>List only trashed documents.</dd>
  </div>
  <div>
    <dt><code>POST /api/collections/{collection}/{id}/restore-deleted</code></dt>
    <dd>Restore one document.</dd>
  </div>
  <div>
    <dt><code>DELETE /api/collections/{collection}/{id}/permanent</code></dt>
    <dd>Permanently delete one document.</dd>
  </div>
  <div>
    <dt><code>DELETE /api/collections/{collection}/trash</code></dt>
    <dd>Permanently empty this collection’s trash.</dd>
  </div>
</dl>

See [Bulk operations and trash](/docs/bulk-and-trash/) for UI behavior, retention, and irreversible
deletion rules.

## Publish, schedule, and restore versions {#versions}

These routes exist only when collection versions are enabled. Unpublish, including a scheduled
unpublish action, additionally requires draft support. Publish, unpublish, restore, and schedule
operations accept `If-Match`.

For draft-enabled resources, `?draft=true` selects authorized working content on reads and saves
pending content on create/update. `?draft=false` selects the preserved live snapshot on reads;
on create it validates and publishes atomically. To publish an existing working draft, use the
publish route. Draft writes defer editorial completeness, not structural validation or access.
Live filters, counts, sorting, and pagination use the live snapshot's values.

<dl class="doc-option-list">
  <div>
    <dt><code>GET /api/collections/{collection}/{id}/versions</code></dt>
    <dd>List stored snapshots.</dd>
  </div>
  <div>
    <dt><code>GET /api/collections/{collection}/{id}/versions/{revision}</code></dt>
    <dd>Read one positive integer revision.</dd>
  </div>
  <div>
    <dt><code>POST /api/collections/{collection}/{id}/publish</code></dt>
    <dd>Publish the current document.</dd>
  </div>
  <div>
    <dt><code>POST /api/collections/{collection}/{id}/unpublish</code></dt>
    <dd>Remove the live snapshot while retaining working content.</dd>
  </div>
  <div>
    <dt><code>POST /api/collections/{collection}/{id}/discard-draft</code></dt>
    <dd>Reset saved pending changes to the live snapshot without unpublishing. Requires draft support and update access.</dd>
  </div>
  <div>
    <dt><code>POST /api/collections/{collection}/{id}/restore/{revision}</code></dt>
    <dd>Restore a snapshot and its publication state; add <code>?draft=true</code> to restore working content without changing the live snapshot.</dd>
  </div>
  <div>
    <dt><code>GET /api/collections/{collection}/{id}/schedule</code></dt>
    <dd>List scheduled publication jobs.</dd>
  </div>
  <div>
    <dt><code>POST /api/collections/{collection}/{id}/schedule</code></dt>
    <dd>Schedule a change with required <code>action</code> (<code>publish</code> or <code>unpublish</code>) and RFC 3339 <code>runAt</code> fields.</dd>
  </div>
  <div>
    <dt><code>DELETE /api/collections/{collection}/{id}/schedule/{jobId}</code></dt>
    <dd>Cancel a scheduled publication.</dd>
  </div>
</dl>

For example, schedule a publish at a known instant and retain the editor's display timezone:

```http
POST /api/collections/posts/post-1/schedule
Content-Type: application/json
If-Match: "7"

{"action":"publish","runAt":"2027-01-02T09:00:00Z","timeZone":"Europe/London"}
```

`runAt` must include an RFC 3339 offset; `timeZone` is optional and only affects how the event is
displayed. It accepts `UTC`, a `±HH:mm` offset, or an IANA region such as `Europe/London`. A
successful schedule returns `201` with `scheduledPublication`, including its `id`, `action`,
`documentId`, `expectedRevision`, `runAt`, and optional `timeZone`. The GET route returns
`scheduledPublications` for queued and failed jobs. To schedule an unpublish, use
`"action":"unpublish"` on a currently published draft-capable document.

See [Drafts and versions](/docs/drafts-and-versions/) for the status model and scheduling lifecycle.

## Upload and serve files {#uploads}

An upload-enabled collection adds these routes:

<dl class="doc-option-list">
  <div>
    <dt><code>POST /api/collections/{collection}</code></dt>
    <dd>Multipart <code>file</code>, with optional JSON <code>data</code> and <code>image</code> fields. For a draft-enabled collection, <code>?draft=false</code> validates and publishes on creation.</dd>
  </div>
  <div>
    <dt><code>POST /api/collections/{collection}/remote-upload</code></dt>
    <dd>JSON <code>url</code>, optional <code>data</code>, <code>image</code>, and <code>filename</code>. Creates a document; <code>?draft=false</code> validates and publishes on creation.</dd>
  </div>
  <div>
    <dt><code>POST /api/collections/{collection}/upload-preview</code></dt>
    <dd>JSON <code>url</code> and optional <code>id</code>; returns a remote file for inspection without saving it.</dd>
  </div>
  <div>
    <dt><code>PATCH /api/collections/{collection}/{id}/upload</code></dt>
    <dd>Replace the file with multipart input, or send JSON to update metadata/image; accepts <code>If-Match</code>.</dd>
  </div>
  <div>
    <dt><code>GET /api/collections/{collection}/{id}/upload-source</code></dt>
    <dd>Read the immutable original file through editor access.</dd>
  </div>
  <div>
    <dt><code>HEAD /api/collections/{collection}/{id}/upload-source</code></dt>
    <dd>Read original-file metadata without the body.</dd>
  </div>
  <div>
    <dt><code>GET /api/uploads/{collection}/{object-key}</code></dt>
    <dd>Return an access-checked stored object body.</dd>
  </div>
  <div>
    <dt><code>HEAD /api/uploads/{collection}/{object-key}</code></dt>
    <dd>Return the same object metadata without the body.</dd>
  </div>
</dl>

Multipart upload and file replacement require `file`. The optional `data` and `image` multipart
fields contain JSON objects. Creation uses `?draft=false` to validate and publish; an existing
asset update uses `publish=true` to commit file/metadata changes and publication in one operation.
JSON updates can change `data` or `image` without replacing the file. Multipart requests are limited
to the collection’s maximum file size plus framing allowance. Remote-host, redirect, MIME,
image-dimension, spool, and storage rules come from the upload configuration. Object responses are
private and non-cacheable; HTML, SVG, and XML-like active content is forced to download with a
sandbox policy. See [Uploads](/docs/uploads/) for complete examples and limits.

## Read and update globals {#globals}

<dl class="doc-option-list">
  <div>
    <dt><code>GET /api/globals/{global}</code></dt>
    <dd>Read the access-checked global.</dd>
  </div>
  <div>
    <dt><code>PATCH /api/globals/{global}</code></dt>
    <dd>Update the global; accepts <code>If-Match</code>.</dd>
  </div>
  <div>
    <dt><code>POST /api/globals/{global}/copy-locale</code></dt>
    <dd>Copy localized values; accepts <code>If-Match</code>.</dd>
  </div>
  <div>
    <dt><code>GET /api/globals/{global}/versions</code></dt>
    <dd>List global versions.</dd>
  </div>
  <div>
    <dt><code>GET /api/globals/{global}/versions/{revision}</code></dt>
    <dd>Read one global revision.</dd>
  </div>
  <div>
    <dt><code>POST /api/globals/{global}/publish</code></dt>
    <dd>Publish a versioned global.</dd>
  </div>
  <div>
    <dt><code>POST /api/globals/{global}/unpublish</code></dt>
    <dd>Return a versioned global to draft.</dd>
  </div>
  <div>
    <dt><code>POST /api/globals/{global}/restore/{revision}</code></dt>
    <dd>Restore a global revision; accepts <code>If-Match</code>.</dd>
  </div>
</dl>

Version routes are absent when the global does not enable versions. Global scheduled publishing is
not implemented.

## Inspect schema and process health {#schema-health}

<dl class="doc-option-list">
  <div>
    <dt><code>GET /api/schema</code></dt>
    <dd>Read the canonical public manifest envelope used by clients and the admin.</dd>
  </div>
  <div>
    <dt><code>GET /healthz</code></dt>
    <dd>Check process liveness.</dd>
  </div>
  <div>
    <dt><code>GET /readyz</code></dt>
    <dd>Check configured dependencies and migration readiness.</dd>
  </div>
</dl>

The public manifest describes fields and capabilities; it never contains executable access rules or
secrets. For exact request and response schemas, inspect `generated/ridu.openapi.json`.
