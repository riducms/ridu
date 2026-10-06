<!-- Generated from website/src/content/docs/typescript-sdk/uploads.md by scripts/sync-agent-docs.ts. -->

# Uploads

These methods work with a collection that has `Upload: true`. The examples use the `media`
collection from [Uploads](../uploads.md), which also covers storage, size limits and image sizes.

## Upload a file {#upload}

```ts
const asset = await ridu.upload('media', file, {
	data: { alt: 'Team gathered outside the studio' }
});
```

`file` is a `Blob` or `File`, such as one from an `<input type="file">`. File size, type and image
limits come from the collection's config on the server.

## Import from a URL {#from-url}

The server downloads the file itself:

```ts
const imported = await ridu.uploadFromURL(
	'media',
	'https://assets.example.com/photo.jpg',
	{ data: { alt: 'Imported photo' } }
);
```

To show the file before saving it, `previewUploadFromURL` returns its `blob` and `filename` without
creating a document.

## Edit, crop or replace {#edit}

Change the metadata, the crop and focal point, or the file itself in one save:

```ts
const edited = await ridu.updateUpload('media', asset.id, {
	data: { alt: 'The team, outside the studio' },
	image: {
		focalX: 50,
		focalY: 35,
		cropX: 10,
		cropY: 10,
		cropWidth: 80,
		cropHeight: 75
	}
});
```

Image coordinates are percentages from 0 to 100. To replace the file, add `file` and, optionally,
`filename`. Ridu regenerates the image sizes after a crop or a new file.

## Show a private file {#urls}

A private upload needs the caller's credentials. An `<img>` can't send a token header, so ask for
short-lived URLs instead:

```ts
const urls = await ridu.getUploadURLs(
	'media',
	assets.map((asset) => ({ id: asset.id, size: 'thumb' })),
	{ expiresIn: 1800 }
);
```

The URLs come back in the order you asked and last `expiresIn` seconds, up to 3,600 (600 by
default). They stop working early when the session ends or the user loses read access, and never
contain the session token. More than 100 items are split into several requests for you;
`getUploadURL` returns one URL.

`readUploadSource` downloads the original, unedited file for someone who may edit it.
