# Homepage hero capture

The homepage hero is a screenshot of the real Ridu admin editing a journal post, with a designed
frontend in its live-preview panel. Nothing in the admin is mocked.

```sh
bun scripts/capture-homepage-hero.ts
```

The script generates a blank SQLite application below the ignored `playground/.ridu/homepage-hero/`,
copies this directory's content model and seed command into it, builds the production binary, and
seeds an editor, a generated banner image, and a post with several published revisions. It serves
the journal frontend on port 18090: the page reads the saved draft with the preview capability and
updates from the admin's live-preview messages. The result is written to
`website/src/assets/home/ridu-live-preview.png`.

Pass `--reuse-project` to skip regenerating the application after the first run.
