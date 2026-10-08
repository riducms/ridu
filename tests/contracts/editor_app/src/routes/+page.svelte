<script lang="ts">
	import { RichTextEditor } from "@riducms/plugin-richtext/editor";
	import type { RichTextDocument } from "@riducms/sdk/richtext";

	import { article } from "./article";

	let story = $state<RichTextDocument | null>({
		version: 1,
		root: {
			type: "root",
			children: [
				{ type: "paragraph", children: [{ type: "text", text: "Rendered on the server" }] },
			],
		},
	});
	// Passed one-way: the app keeps its copy current through onchange.
	let summary = $state<RichTextDocument>({
		version: 1,
		root: {
			type: "root",
			children: [{ type: "paragraph", children: [{ type: "text", text: "Passed one-way" }] }],
		},
	});
	// Stored by a field with blocks, which an editor without the admin's extensions can't open.
	const withBlock = {
		version: 1,
		root: { type: "root", children: [{ type: "block", version: 1, fields: { blockType: "cta" } }] },
	};
</script>

<main>
	<h1>Rich-text editor</h1>
	<RichTextEditor bind:value={story} label="Story" features={["links", "lists"]} />
	<pre data-testid="story">{JSON.stringify(story)}</pre>

	<RichTextEditor
		label="Note"
		lang="fr"
		toolbar="fixed"
		messages={{ "editor.placeholder": "Écrivez une note" }}
	/>
	<RichTextEditor value={summary} onchange={(next) => (summary = next)} label="Summary" />
	<pre data-testid="summary">{JSON.stringify(summary)}</pre>

	<RichTextEditor value={article} label="Article" />
	<RichTextEditor value={withBlock} label="Imported" />
</main>
