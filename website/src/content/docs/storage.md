---
title: 'Object storage'
description: 'Configure local, S3-compatible, or custom storage safely for upload bytes.'
product: adapters
eyebrow: 'Adapters'
order: 210
navigation:
  section: 'Develop & operate'
  parent: 'adapters'
  order: 40
  title: 'Object storage'
---

Upload documents live in the selected database; their bytes live in a `storage.Backend`. Configure
the backend once for the application. Ridu coordinates writes with document transactions and checks
references again before cleanup.

## Set a stable application namespace {#namespace}

Every application with an upload collection must declare `Config.StorageNamespace`:

```go title="content/config.go"
func Config() ridu.Config {
	return ridu.Config{
		Name:             "Acme Editorial",
		StorageNamespace: "acme-production",
		Collections:      []ridu.Collection{Media},
	}
}
```

The namespace is an object prefix, not a display label. Keep it unchanged when
`Config.Name` or an upload collection slug changes, and never reuse it for another application that
shares the backend. It must contain 3–64 lowercase letters, digits, underscores, or hyphens.

New objects use a slug-independent `ridu/<namespace>/objects/<nonce>/...` layout, so renaming a
collection does not move bytes.

Create the backend lazily through `WithUploadStorage` so manifest resolution and generation never
touch the filesystem or network.

## Implement a backend {#backend-contract}

```go
type Backend interface {
	Put(context.Context, string, io.Reader, int64, string) error
	Open(context.Context, string) (io.ReadCloser, Object, error)
	Delete(context.Context, string) error
	List(context.Context, ListRequest) (ListPage, error)
}

type Object struct {
	Key         string
	Size        int64
	ContentType string
	ModifiedAt  time.Time
}

type ListRequest struct {
	Prefix string
	Cursor string
	Limit  int
}

type ListPage struct {
	Objects    []Object
	NextCursor string
}
```

The small interface has strict semantics:

| Operation | Required behaviour                                                                                                                                                           |
| --------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Put`     | Honour context cancellation and the declared byte count. Replacing an existing key is atomic; a partial object must never appear as committed.                               |
| `Open`    | Return a streaming body plus authoritative metadata, or `storage.ErrNotFound` for absence. The caller closes the body.                                                       |
| `Delete`  | Deleting a missing key succeeds as a no-op so retries are safe.                                                                                                              |
| `List`    | Return one bounded page in unique key order under the requested prefix. Every object has an authoritative non-zero `ModifiedAt`; `NextCursor` advances until the final page. |

`ModifiedAt` is a safety input, not optional decoration. Reconciliation uses it to enforce the grace
period and fails closed if an adapter cannot prove object age. Ridu still validates namespaced keys,
application namespace, committed references, and fresh object-lock state immediately before a
destructive call.

Production backends should also implement:

```go
type HealthBackend interface {
	Ping(context.Context) error
}
```

`Ping` must be bounded, repeatable, and safe without reading or mutating an application object.
`ridu.Execute` includes it in startup preflight and `/readyz`. The optional `storage.URLSigner`
provides short-lived direct downloads, but private framework delivery remains access checked.

## Local filesystem {#local-storage}

The local backend suits development and a single node with a durable mounted volume:

```go title="cmd/server/storage.go"
func openUploadStorage(context.Context) (storage.Backend, error) {
	return localstorage.New("/var/lib/acme/uploads")
}
```

Register it with the server:

```go
ridu.WithUploadStorage(openUploadStorage)
```

Writes use a temporary file, sync it, atomically rename it into place, and sync the directory.
Object paths reject absolute paths and traversal. The readiness probe verifies the root is a
directory, performs a durable private probe write, removes it, and syncs cleanup.

A local volume must be durable across replacement and mounted at the same path before readiness.
It is not shared automatically across replicas: use a shared object service for a multi-node
deployment rather than putting independent local disks behind one application.

## S3-compatible storage {#s3-storage}

The dependency-free S3 backend implements SigV4 PUT, GET, DELETE, complete paginated listing,
bucket health, and signed GET URLs:

```go title="cmd/server/storage.go"
func openUploadStorage(context.Context) (storage.Backend, error) {
	return s3storage.New(s3storage.Config{
		Endpoint:       os.Getenv("S3_ENDPOINT"),
		Region:         os.Getenv("S3_REGION"),
		Bucket:         os.Getenv("S3_BUCKET"),
		AccessKey:      os.Getenv("S3_ACCESS_KEY"),
		SecretKey:      os.Getenv("S3_SECRET_KEY"),
		// 256 × 2²⁰ = 268,435,456 bytes (256 MiB)
		MaxSpoolBytes:  256 << 20,
		SpoolDirectory: "/var/tmp/ridu-spool",
	})
}
```

Endpoint, region, bucket, access key, and secret key are required. Supply a custom `http.Client` to
set deployment-specific transport and timeout policy; the default client timeout is 30 seconds.

S3 signing needs the payload hash before upload. A seekable input is hashed and rewound. A
non-seekable input is copied to a private temporary file, bounded by `MaxSpoolBytes`; zero selects
256 MiB. Size mismatch, cancellation, or signing failure removes the spool file. Plan local scratch
space for concurrent non-seekable uploads.

> [!IMPORTANT]
> S3 endpoints must use HTTPS. `AllowInsecureEndpoint` permits HTTP only for a trusted local emulator
> such as MinIO. It sends storage credentials over plaintext and is not production configuration.

`SignedURL` accepts a lifetime from one second through seven days. Signed possession is a temporary
capability, not collection authorization; issue it only after the application has made the relevant
access decision. Ordinary Ridu delivery remains private and `no-store` because collection read
rules may depend on the actor or row even when an upload collection is not marked private.

The current backend does not implement multipart, resumable, or direct browser upload. Large files
still pass through Ridu's bounded upload operation and, for non-seekable S3 bodies, the spool.

## Reconciliation and deletion {#reconciliation}

Database metadata and objects cannot share one atomic commit. Ridu narrows that boundary with
staged writes, rollback, cross-process object locks, last-reference checks, and reconciliation.

`App.ReconcileUploads` reports candidates from an ACL-independent snapshot of current, trashed, and
versioned references. The grace period must be at least five minutes. After review,
`App.CleanupUploads` re-checks candidates in bounded batches under fresh snapshots and object locks
before deletion. A candidate that gained a reference is retained.

Keep the grace period longer than the slowest admitted upload or import and quiesce imports during
a destructive cleanup pass. Failed after-commit cleanup retains bytes as reconciliation candidates;
it does not roll back a durable document deletion.

See [Uploads](/docs/uploads/) for collection metadata, image variants, remote ingestion, private
delivery, and the 65-key per-document object bound.

## Build a safe custom adapter {#custom-adapter}

Before using another provider, test these behaviours directly:

- exact declared-size enforcement, cancellation, short reads, extra bytes, and partial-write
  cleanup;
- atomic same-key replacement and idempotent missing-key deletion;
- path and key traversal rejection;
- `Open` metadata, body close behaviour, and `storage.ErrNotFound` mapping;
- prefix-restricted, fully paginated `List` results with non-zero
  modification times;
- bounded repeatable `Ping`, plus credential and dependency failure;
- concurrent `Put`, `Open`, `Delete`, reconciliation, and retry races; and
- no logging of credentials, private keys, signed URLs, or sensitive provider responses.

Custom storage used with `ridu.Execute` needs `HealthBackend` unless you configure external
dependency checks and opt into unverifiable readiness. Prefer implementing `HealthBackend` so Ridu
can report dependency failures.

## Back up both halves {#backup}

Back up the selected database's metadata, versions, and reference indexes together with the object namespace.
Restore both to the same recovery point and test document reads, private delivery, representative
checksums, and reconciliation. A database-only restore can reference missing bytes; an object-only
restore can retain stale data.

Read [Production](/docs/production/) for deployment and restore drills and
[Security](/docs/security/) for trust and capability boundaries.
