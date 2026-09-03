<script lang="ts">
	import { createClient } from "@riducms/sdk";
	import type { PostsCreate, PostsWhere } from "../../../../testdata/generated/ridu.generated";

	let { baseURL }: { baseURL: string } = $props();
	const client = $derived(createClient({ baseURL }));
	const draft: PostsCreate = { title: "Created from Svelte" };
	const published: PostsWhere = { status: { equals: "published" } };

	async function load() {
		return client.list("posts", { where: published, select: { title: true } });
	}
</script>

<button onclick={() => client.create("posts", draft)}>Create</button>
<button onclick={load}>Load published</button>
