<!-- Generated from website/src/content/docs/uploads.md by scripts/sync-agent-docs.ts. -->

# Uploads and media

Uploads in Ridu are documents with files attached. The document lives in your selected database and
contains fields such as alt text and a caption. The file bytes live in a separate object-storage
backend. Ridu joins the two into one media-library experience in the admin and generated SDK.

This guide adds a private `media` library to an existing starter project, stores files under
`.ridu/uploads` during development, and adds a `heroImage` picker to posts. When you finish, an
author can upload an image once and reuse it from any post.

> [!NOTE]
> An **upload collection** accepts file bytes and creates media documents. An **Upload field** only
> stores a reference to one of those documents. Add both when content such as a post needs a media
> picker.

## Before you start {#prerequisites}

You need a generated Ridu project and a user who can sign in to its admin. The examples use the
starter project's existing `authenticatedOnly` access function. If your project started from the
blank template, define an auth collection and equivalent access rule first.

The local backend below is appropriate for development and one application process on a durable
disk. The [production storage](#production-storage) section shows the S3-compatible replacement.

## 1. Define the media library {#upload-collection}

Create `content/media.go`:

```go title="content/media.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Media = ridu.Collection{
	Slug:   "media",
	Labels: ridu.CollectionLabels{Singular: "Asset", Plural: "Media"},
	Admin: ridu.CollectionAdmin{
		Group:      "Content",
		UseAsTitle: "alt",
	},
	Upload: true,
	UploadConfig: ridu.UploadConfig{
		MaxFileSize: 10 << 20, // 10 × 2²⁰ = 10,485,760 bytes (10 MiB)
		MimeTypes:   []string{"image/jpeg", "image/png"},
		Private:     true,
		ImageSizes: []ridu.ImageSize{
			{Name: "card", Width: 1200, Height: 630, Fit: "cover"},
			{Name: "thumb", Width: 320, Height: 320, Fit: "cover"},
		},
	},
	Fields: field.Fields{
		field.Text("alt").
			Label("Alt text").
			Required().
			Admin(field.Admin{
				Description: "Describe the image for people who cannot see it.",
			}),
		field.Textarea("caption").Label("Caption"),
	},
	Access: ridu.CollectionAccess{
		Create: authenticatedOnly,
		Read:   authenticatedOnly,
		Update: authenticatedOnly,
		Delete: authenticatedOnly,
	},
}
```

The `media` collection now accepts images up to 10 MiB. JPEG and PNG originals are retained,
and Ridu generates `card` and `thumb` JPEG variants. `Fit: "cover"` fills the requested dimensions
by cropping around the image's focal point; use `"contain"` when the complete image must remain
visible.

Ridu adds the following server-owned fields to every upload document:

| Field                         | Meaning                                                          |
| ----------------------------- | ---------------------------------------------------------------- |
| `filename`, `mimeType`        | Sanitized filename and MIME type detected from the file bytes    |
| `filesize`, `width`, `height` | Original byte size and image dimensions                          |
| `url`                         | Access-checked delivery path for the original                    |
| `sizes.card`, `sizes.thumb`   | URL, dimensions, MIME type, and byte size for each image variant |
| `focalX`, `focalY`            | Image focal point as percentages from `0` through `100`          |

Clients cannot forge these values with an ordinary JSON create. They submit the file and authored
fields such as `alt`; Ridu derives the file metadata.

## 2. Register the collection {#register-collection}

Add a stable storage namespace and `Media` to `content/config.go`. The surrounding starter config
is included so the insertion points are clear:

```go title="content/config.go" add={9,14}
package content

import "github.com/riducms/ridu"

func Config() ridu.Config {
	return ridu.Config{
		Name:             "Acme Editorial",
		Admin:            ridu.AdminConfig{User: "users"},
		StorageNamespace: "acme-editorial",
		Plugins:          installedPlugins(),
		Collections: []ridu.Collection{
			Users,
			Posts,
			Media,
		},
	}
}
```

`StorageNamespace` owns one application's objects inside the backend. Use 3–64 lowercase letters,
digits, underscores, or hyphens. Keep it unchanged if the display name or collection slug changes,
and do not share it with another application using the same storage location.

## 3. Connect local storage {#storage-ownership}

Keep the storage setup in the generated `cmd/server/main.go`. First add the local-storage and
storage-contract imports alongside the existing SQLite imports:

```go title="cmd/server/main.go" add={16-17}
package main

import (
	"context"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"example.com/acme/internal/adminassets"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/sqlite"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/storage"
	"github.com/riducms/ridu/store"
)
```

Then create the backend lazily inside `runtimeOptions`. This example uses SQLite; keep your
PostgreSQL or MongoDB `WithStore` block unchanged and add the same highlighted
`WithUploadStorage` block after it.

```go title="cmd/server/main.go" add={6-12}
func runtimeOptions(
	applicationConfig ridu.Config,
) []ridu.ExecuteOption {
	return []ridu.ExecuteOption{
		ridu.WithStore(func(ctx context.Context) (store.Store, error) {
			return sqlite.Open(ctx, sqliteDatabasePath())
		}),
		ridu.WithUploadStorage(func(
			_ context.Context,
		) (storage.Backend, error) {
			root := os.Getenv("RIDU_UPLOAD_PATH")
			if root == "" {
				root = ".ridu/uploads"
			}
			return localstorage.New(root)
		}),
		ridu.WithAddress(env("RIDU_ADDRESS", ":8080")),
		ridu.WithHandlerOptions(ridu.HandlerOptions{
			AdminAssets: adminassets.FS(),
			// Keep the remaining generated handler options here.
		}),
		// Keep the generated WithServerOptions block here.
	}
}
```

The factory runs only when the application server starts. Commands that resolve config—such as
generation and offline migration creation—do not need the upload directory or cloud credentials.
For local development the default `.ridu/uploads` directory is already disposable project state.
Set an absolute `RIDU_UPLOAD_PATH` when the files must survive a process working-directory change.

## 4. Add a media picker to posts {#reference-upload}

Add `field.Upload` to the starter `Posts` collection. The target is the `media` collection slug,
not a filesystem path or storage bucket:

```go title="content/posts.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/richtext"
)

var Posts = ridu.Collection{
	Slug: "posts",
	Access: ridu.CollectionAccess{
		Create: authenticatedOnly,
		Read:   authenticatedOnly,
		Update: authenticatedOnly,
		Delete: authenticatedOnly,
	},
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Select("status", "draft", "published").Default("draft"),
		field.Upload("heroImage", "media"),
		field.Relationship("author", "users"),
		richtext.Field("content"),
	},
}
```

This stores one media document ID in `heroImage`. Add `.Required()` when every post must have
an image, or `field.Uploads` when the field should be a gallery. Read the focused
[Upload field guide](https://riducms.com/docs/fields/upload/) for filtering and delete behavior.

## 5. Run in development {#generate-and-run}

Run the project-local development loop from the project root. `ridu dev` resolves the updated Go
config, regenerates contracts, applies the safe additive development schema change, and starts the
API and admin. These four tabs are the same command; use the package manager selected when the
project was created.

```bash title="terminal" package-manager="npm"
npm run dev
```

```bash title="terminal" package-manager="bun"
bun run dev
```

```bash title="terminal" package-manager="pnpm"
pnpm run dev
```

```bash title="terminal" package-manager="yarn"
yarn run dev
```

The server should pass upload-storage readiness and print the admin URL. If it reports
`upload storage is unavailable`, the `WithUploadStorage` option is missing or the configured
directory cannot be created.

## 6. Upload and reuse an image in the admin {#admin-workflow}

Open `/admin`, sign in, and choose **Media** under **Content**. Select **Create new**, choose an image,
enter its alt text, and save. Ridu uploads the original, detects its metadata, generates both image
sizes, and creates the media document.

Now open a post. The new **Hero image** field opens a media picker containing the document you just
created. Selecting it stores that media document's ID on the post; it does not copy the file.

![A Ridu Media document editor showing the asset preview, detected file metadata, authored alt text, image variants, and document actions.](https://raw.githubusercontent.com/riducms/ridu/main/docs/assets/ridu-admin-upload.png)

_The preview, generated sizes, and authored fields belong to one media document. The original and
variant bytes remain in the configured storage backend._

## Upload from application code {#sdk-upload}

The generated TypeScript module knows that `media` accepts uploads and which authored fields it
expects. This complete helper creates the application-bound client, uploads a browser `File`, and
returns a URL that works whether the SDK returned a relative or absolute delivery path:

```ts title="src/lib/media.ts"
import { createClient } from '../../generated/ridu.generated';

const baseURL = 'http://localhost:8080';
const ridu = createClient({ baseURL });

export async function uploadHero(file: File, alt: string) {
	const asset = await ridu.upload('media', file, {
		data: {
			alt,
			caption: 'Uploaded from the website'
		}
	});

	return {
		id: asset.id,
		originalURL: new URL(asset.url, baseURL).toString(),
		cardURL: new URL(asset.sizes.card.url, baseURL).toString(),
		width: asset.width,
		height: asset.height
	};
}
```

Pass the resulting media ID when creating or updating a post:

```ts title="src/features/posts/create-post.ts"
import { uploadHero } from '../../lib/media';
import { createClient } from '../../../generated/ridu.generated';

const ridu = createClient({ baseURL: 'http://localhost:8080' });

export async function createPost(file: File) {
	const hero = await uploadHero(
		file,
		'The Ridu team outside the studio'
	);

	return ridu.create('posts', {
		title: 'Studio notes',
		status: 'draft',
		heroImage: hero.id
	});
}
```

In a browser, the SDK sends the current session cookie by default. For another browser origin,
configure [CORS and credentialed cookies](https://riducms.com/docs/cors/). Server-side callers can supply an API-key
`Authorization` header when their application uses service credentials.

<details class="docs-disclosure">
<summary>Use the multipart REST endpoint directly</summary>
<div class="docs-disclosure-body">

The SDK call above sends `multipart/form-data` to `POST /api/collections/media`. A non-TypeScript
client can send the same request. This example assumes `cookies.txt` contains an authenticated Ridu
session:

```bash title="terminal"
curl --fail-with-body \
	--cookie cookies.txt \
	--form 'file=@./hero.jpg' \
	--form 'data={"alt":"The Ridu team outside the studio","caption":"Homepage hero"}' \
	http://localhost:8080/api/collections/media
```

The `file` part is required. `data` is an optional JSON object containing only application-authored
fields. Do not set the multipart `Content-Type` header manually; the client must add its boundary.

</div>
</details>

## Import a public remote image {#remote-url}

Use `uploadFromURL` when Ridu should fetch a public HTTP(S) image and turn it into a normal media
document:

```ts title="src/lib/import-media.ts"
import { createClient } from '../../generated/ridu.generated';

const ridu = createClient({ baseURL: 'https://cms.example.com' });

export async function importLaunchGraphic() {
	return ridu.uploadFromURL(
		'media',
		'https://images.example.com/launch.png',
		{
			data: {
				alt: 'Launch graphic',
				caption: 'Imported from the campaign image service'
			}
		}
	);
}
```

The server blocks non-HTTP schemes, private and loopback destinations, unsafe redirects, oversized
responses, and content that fails the collection's MIME rules. This is a guarded import operation,
not a general server-side proxy. Application-specific host allowlists, moderation, malware
scanning, and quarantine need a trusted pre-ingestion service or plugin.

## Change the focal point and regenerate sizes {#images}

Image uploads start at `{ focalX: 50, focalY: 50 }`, the center of the original. Authors can adjust
the focal point in the media editor. Application code can do the same and regenerate every named
size:

```ts title="src/lib/update-crop.ts"
import { createClient } from '../../generated/ridu.generated';

const ridu = createClient({ baseURL: 'https://cms.example.com' });

export async function keepSubjectInFrame(
	assetID: string,
	revision: number
) {
	return ridu.updateUploadImage(
		'media',
		assetID,
		{ focalX: 40, focalY: 35 },
		{ revision }
	);
}
```

Coordinates are percentages from `0` through `100`, not decimal fractions or source pixels. Pass
the current `_revision` so an older editor cannot overwrite a newer crop. Optional `cropX`, `cropY`,
`cropWidth`, and `cropHeight` values use the same percentage coordinate system.

## Deliver files safely {#delivery}

`asset.url` and each `asset.sizes[name].url` point at Ridu's access-checked delivery endpoint. Build
an absolute URL with `new URL(asset.url, baseURL)` when a frontend and CMS use different origins.

Because the example collection sets `Private: true`, the request must carry an authenticated actor
who can read the media document. Ridu also reapplies field redaction before delivery. Framework
delivery sends `Cache-Control: private, no-store`, including for collections where `Private` is
false, because collection read access may still depend on the actor or document.

If a public CDN is required, create an application-owned publication or copy boundary instead of
exposing Ridu's internal `objectKey`. A backend that implements `storage.URLSigner` can issue a
short-lived direct download only after the application has made the relevant access decision.

## Use S3-compatible storage in production {#production-storage}

For more than one application replica, replace the local backend with an S3-compatible backend so
every replica sees the same objects. The collection and `field.Upload` definitions do not change.
In `cmd/server/main.go`, replace the `localstorage` import with
`s3storage "github.com/riducms/ridu/adapters/storage/s3"`, then replace the local
`WithUploadStorage` option:

```go title="cmd/server/main.go"
ridu.WithStore(func(ctx context.Context) (store.Store, error) {
	return postgres.Open(ctx, os.Getenv("DATABASE_URL"))
}),
ridu.WithUploadStorage(func(
	_ context.Context,
) (storage.Backend, error) {
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
}),
ridu.WithAddress(env("RIDU_ADDRESS", ":8080")),
```

Use HTTPS for production endpoints. Size the private spool directory for concurrent uploads and
back it with ephemeral disk; S3 signing may need to spool a non-seekable request before upload. See
[Object storage](https://riducms.com/docs/storage/#s3-storage) for timeouts, local emulators, signed URLs, and the
backend contract.

## Prepare the schema change for deployment {#deploy-schema}

`ridu dev` applies additive development synchronization only. Before deploying the media
collection, create a migration, review its plan, verify the complete history, and run the project
check. Do this after the local workflow above succeeds—not before `ridu dev`.

```bash title="terminal"
ridu migrate create --name add-media
ridu migrate verify
ridu check
```

Commit the migration and changed files under `generated/`. Apply the reviewed history with
`migrate up` against the deployment database as an operator step, then require `migrate status` and
application readiness to pass. The [Migrations guide](./migrations.md) covers adapter-specific
connection and safety options.

## Delete, reconcile, and back up media {#cleanup}

Soft-deleting a media document retains its objects so it can be restored. Permanent deletion
removes objects only after the database transaction commits and only when no live, trashed, or
versioned document still owns them.

Database metadata and object storage cannot share one transaction. `App.ReconcileUploads` reports
old unreferenced objects without deleting them; after reviewing that report, `App.CleanupUploads`
performs the destructive pass. Use a grace period of at least five minutes and longer than the
slowest upload or import in your deployment. Storage listings must provide trustworthy non-zero
modification times or cleanup fails closed.

Back up the database and its upload namespace to a matched recovery point. Restoring only the
database can leave missing files; restoring only object storage can leave unreferenced objects.
After a restore, check representative media URLs and checksums before running report-only
reconciliation.

## Current limitations {#limits}

Ridu does not yet provide resumable transfers, direct browser-to-storage sessions, quarantine,
malware scanning, or a general raw-object endpoint. Each document can own at most 65 original and
derived object keys.

## Troubleshooting {#troubleshooting}

| Symptom                                         | What to check                                                                                                                                      |
| ----------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `upload storage is unavailable`                 | Register `ridu.WithUploadStorage(...)` and make sure the directory, bucket, and credentials are available to the server process.                   |
| `unknown upload collection "media"`             | Add `Media` to `Config.Collections`, keep `Upload: true`, and save while `ridu dev` is running; prepare the immutable migration before deployment. |
| MIME or size validation fails                   | Compare the actual file bytes with `MimeTypes` and `MaxFileSize`; Ridu sniffs content rather than trusting the browser's MIME header.              |
| The media picker is empty                       | Upload a media document first, confirm the Upload field targets `media`, and check that the signed-in actor can read that document.                |
| A browser upload is unauthorized                | Sign in first; for a separate frontend origin, allow the exact CORS origin and credentials.                                                        |
| A private `<img>` does not load                 | Use the Ridu delivery URL, preserve credentials where appropriate, and check the collection's read rule. Do not use `objectKey` as a URL.          |
| An image variant is missing                     | Confirm the original is a supported image, the size name exists in `ImageSizes`, and the requested variant is read from `asset.sizes.<name>.url`.  |
| A create timed out or returned an uncertain 5xx | Refresh or query the media collection before retrying so a committed upload is not duplicated.                                                     |

Continue with the [Upload field guide](https://riducms.com/docs/fields/upload/), [Object storage](https://riducms.com/docs/storage/),
[TypeScript SDK](./typescript-sdk.md#uploads), or the [`storage` API reference](https://riducms.com/reference/storage/).
