<script lang="ts">
	import { createClient, type Pages } from "./generated/ridu.generated";
	import Blocks from "./render/Blocks.svelte";
	const client = createClient({ baseURL: window.location.origin });
	const id = new URL(window.location.href).searchParams.get("page");
	const page: Promise<Pages> = id
		? client.find("pages", id, { populate: { "layout.media.asset": true } })
		: Promise.reject(new Error("Pass ?page=<published page ID> to render a page."));
</script>

<main>
	{#await page}
		<p>Loading page…</p>
	{:then document}
		<Blocks blocks={document.layout ?? []} />
	{:catch error}
		<p role="alert">{error instanceof Error ? error.message : "Could not load page"}</p>
	{/await}
</main>
