<!-- Generated from website/src/content/docs/typescript-sdk/publishing.md by scripts/sync-agent-docs.ts. -->

# Drafts and publishing

These methods work with versioned collections. The examples use an `articles` collection with
drafts, configured with `Versions: true` and `VersionConfig: ridu.VersionConfig{Drafts: true}`.
[Drafts and versions](../drafts-and-versions.md) explains how drafts, the published version and
history relate.

## Save a draft {#drafts}

On a collection with drafts, `create` saves a draft, which may leave required fields empty:

```ts
const article = await ridu.create('articles', {
	title: 'Working title'
});
```

To change a published article without changing what readers see, save the edit as a draft:

```ts
const draft = await ridu.update(
	'articles',
	article.id,
	{ summary: 'A first summary' },
	{ draft: true, revision: article._revision }
);
```

## Publish {#publish}

Publish the saved draft, or send changes and publish them in one step:

```ts
await ridu.publish('articles', article.id, {
	revision: draft._revision
});

await ridu.publishChanges(
	'articles',
	article.id,
	{ summary: 'A better summary' },
	{ revision: draft._revision }
);
```

Publishing validates the whole document, including required fields a draft left empty. Pass the
revision you last read so a newer edit isn't published by accident; see
[Don't overwrite someone else's edit](../writing-data.md#revisions).

`unpublish` takes the article offline and keeps its content. `discardDraft` throws away unpublished
changes and goes back to the published version.

## Read the draft or the published version {#read}

```ts
const working = await ridu.find('articles', article.id, {
	draft: true
});
const live = await ridu.find('articles', article.id, {
	draft: false
});
```

`draft: true` needs permission to read drafts. Leaving `draft` out returns the draft to callers
who may read drafts, and the published version to everyone else. A working copy of a published
article has `_hasDraftChanges` and `_publishedRevision`.

## History {#history}

```ts
const history = await ridu.versions('articles', article.id);
const { totalDocs } = await ridu.countVersions(
	'articles',
	article.id
);

const restored = await ridu.restore(
	'articles',
	article.id,
	history[0].Revision,
	{ draft: true, revision: article._revision }
);
```

`restore` with `draft: true` puts the old content in the draft, leaving the published version
alone until you publish. `version` reads one revision.

## Schedule publication {#schedule}

```ts
const job = await ridu.schedulePublish(
	'articles',
	article.id,
	new Date('2027-01-02T09:00:00Z'),
	{ revision: article._revision, timeZone: 'Europe/London' }
);

const pending = await ridu.scheduledPublications(
	'articles',
	article.id
);
await ridu.cancelScheduledPublication('articles', article.id, job.id);
```

The date is the exact moment to publish; `timeZone` only records how to display it.
`scheduleUnpublish` takes a published article offline later. A scheduled job can still fail when it
runs, for example after a newer edit or an access change. See
[Schedule collection publication](../drafts-and-versions.md#scheduling).

## Globals {#globals}

A versioned global has the same operations under global names:

| Collection                             | Global                                                   |
| -------------------------------------- | -------------------------------------------------------- |
| `publish`, `publishChanges`            | `publishGlobal`, `publishGlobalChanges`                  |
| `unpublish`, `discardDraft`            | `unpublishGlobal`, `discardGlobalDraft`                  |
| `versions`, `version`, `countVersions` | `globalVersions`, `globalVersion`, `countGlobalVersions` |
| `restore`                              | `restoreGlobal`                                          |

Scheduled publication is available for collections only.
