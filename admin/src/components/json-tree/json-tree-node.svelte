<script lang="ts">
	import { isRecord } from "@riducms/protocol";
	import ChevronIcon from "@admin/components/icons/chevron.svelte";
	import JsonTreeNode from "@admin/components/json-tree/json-tree-node.svelte";

	let {
		name,
		value,
		last = true,
		root = false,
		disclosureLabel,
	}: {
		name?: string;
		value: unknown;
		last?: boolean;
		root?: boolean;
		disclosureLabel?: string;
	} = $props();

	let open = $state(true);
	const array = $derived(Array.isArray(value));
	const entries = $derived(
		array ? Object.entries(value as unknown[]) : isRecord(value) ? Object.entries(value) : []
	);
	const container = $derived(array || isRecord(value));
	const opening = $derived(array ? "[" : "{");
	const closing = $derived(array ? "]" : "}");
	const property = $derived(name === undefined ? "" : `${JSON.stringify(name)}: `);
</script>

<li class={["ridu-json-node", root && "ridu-json-node--root"]}>
	{#if container && entries.length > 0}
		<button
			type="button"
			class="ridu-json-node__toggle"
			aria-label={disclosureLabel ?? name ?? (root ? "JSON" : opening + closing)}
			aria-expanded={open}
			onclick={() => (open = !open)}
		>
			<ChevronIcon class="ridu-json-node__chevron" />
			<span>{property}{opening}</span>
		</button>

		{#if open}
			<ul class="ridu-json-node__children">
				{#each entries as [key, child], index (key)}
					<JsonTreeNode
						name={array ? undefined : key}
						disclosureLabel={array ? `[${key}]` : undefined}
						value={child}
						last={index === entries.length - 1}
					/>
				{/each}
			</ul>
		{/if}
		<div class="ridu-json-node__closing">{closing}{last ? "" : ","}</div>
	{:else}
		<div class="ridu-json-node__line">
			<span>{property}</span>
			<span
				class={{
					"ridu-json-node__string": typeof value === "string",
					"ridu-json-node__number": typeof value === "number",
				}}
			>
				{container ? `${opening}${closing}` : (JSON.stringify(value) ?? "undefined")}
			</span>
			{last ? "" : ","}
		</div>
	{/if}
</li>
