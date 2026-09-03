<script lang="ts">
	import ChevronRightIcon from "~icons/lucide/chevron-right";

	import JsonTreeNode from "@admin/components/json-tree/json-tree-node.svelte";

	let { name, value, last = true }: { name?: string; value: unknown; last?: boolean } = $props();
	const entries = $derived(
		Array.isArray(value)
			? value.map((item, index) => [String(index), item] as const)
			: isRecord(value)
				? Object.entries(value)
				: []
	);
	const container = $derived(Array.isArray(value) || isRecord(value));
	const opening = $derived(Array.isArray(value) ? "[" : "{");
	const closing = $derived(Array.isArray(value) ? "]" : "}");
	const countLabel = $derived(
		Array.isArray(value)
			? entries.length === 1
				? "item"
				: "items"
			: entries.length === 1
				? "key"
				: "keys"
	);

	function isRecord(candidate: unknown): candidate is Record<string, unknown> {
		return typeof candidate === "object" && candidate !== null && !Array.isArray(candidate);
	}

	function primitive(candidate: unknown) {
		if (typeof candidate === "string") return JSON.stringify(candidate);
		if (candidate === undefined) return "undefined";
		return String(candidate);
	}

	function tone(candidate: unknown) {
		if (typeof candidate === "string") return "text-[#b7c89a]";
		if (typeof candidate === "number") return "text-[#c9b38b]";
		if (typeof candidate === "boolean") return "text-[#9fb9d8]";
		return "text-foreground-faint";
	}
</script>

{#if container}
	<details class="group/json" open>
		<summary
			class="flex cursor-pointer list-none items-center gap-1 rounded px-0.5 hover:bg-control-hover focus-visible:outline-1 focus-visible:outline-primary/60"
			aria-label={name === undefined
				? `${entries.length} ${countLabel}`
				: `${name}, ${entries.length} ${countLabel}`}
		>
			<ChevronRightIcon
				class="size-3 shrink-0 text-foreground-faint transition-transform group-open/json:rotate-90"
			/>
			{#if name !== undefined}<span class="text-[#9db5cd]">{JSON.stringify(name)}</span>
				<span class="text-foreground-faint">:</span>{/if}
			<span class="text-foreground-muted">{opening}</span>
			<span class="text-[10px] text-foreground-faint">
				{entries.length}
				{countLabel}
			</span>
			<span class="text-foreground-muted">{closing}{last ? "" : ","}</span>
		</summary>
		<div class="ms-3.5 border-s border-control-border ps-3">
			{#each entries as [key, child], index (key)}
				<JsonTreeNode name={key} value={child} last={index === entries.length - 1} />
			{/each}
		</div>
	</details>
{:else}
	<div class="flex gap-1 rounded px-0.5 hover:bg-control-hover">
		{#if name !== undefined}<span class="text-[#9db5cd]">{JSON.stringify(name)}</span>
			<span class="text-foreground-faint">:</span>{/if}
		<span class={tone(value)}>{primitive(value)}{last ? "" : ","}</span>
	</div>
{/if}
