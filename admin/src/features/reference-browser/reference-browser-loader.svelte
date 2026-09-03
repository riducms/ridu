<script lang="ts">
	import type { FieldReferenceBrowserProps } from "@riducms/plugin";
	import type { Component } from "svelte";

	let { open = $bindable(false), ...props }: FieldReferenceBrowserProps = $props();
	let Browser = $state<Component<FieldReferenceBrowserProps> | undefined>(undefined);

	void import("@admin/features/reference-browser/reference-browser.svelte").then(
		({ default: component }) => (Browser = component)
	);
</script>

{#if Browser !== undefined}
	<Browser bind:open {...props} />
{/if}
