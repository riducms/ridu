<script lang="ts">
	import { enhance } from "$app/forms";
	import { RichTextEditor } from "@riducms/plugin-richtext/editor";
	import { RichText } from "@riducms/plugin-richtext/svelte";

	import "./theme.scss";

	let { data, form } = $props();

	// The editor reads the document when it mounts; the saved copy below follows the server.
	// svelte-ignore state_referenced_locally
	let body = $state.raw(data.post.body ?? null);
</script>

<main>
	<h1>{data.post.title}</h1>
	<form method="POST" use:enhance>
		<RichTextEditor bind:value={body} label="Body" features={["links", "lists"]} toolbar="fixed" />
		<input type="hidden" name="body" value={JSON.stringify(body)} />
		<button>Save</button>
	</form>
	{#each form?.issues ?? [] as issue}
		<p role="alert">{issue}</p>
	{/each}

	<h2>Saved</h2>
	<article data-testid="saved">
		{#if data.post.body}
			<RichText value={data.post.body} />
		{/if}
	</article>
</main>
