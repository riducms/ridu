<script lang="ts">
	import { Collapsible } from "bits-ui";
	import type { Snippet } from "svelte";
	import "@admin/components/ui/collapsible-panel/collapsible-panel.scss";

	let { id, open, children }: { id: string; open: boolean; children: Snippet } = $props();
</script>

<Collapsible.Root {open}>
	<Collapsible.Content {id} class="ridu-collapsible-panel" inert={!open} aria-hidden={!open}>
		{@render children()}
	</Collapsible.Content>
</Collapsible.Root>
