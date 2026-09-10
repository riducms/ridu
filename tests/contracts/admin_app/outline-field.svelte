<script lang="ts">
	import { decodeOutline, type OutlineValue } from "./outline-value";
	import type { PluginFieldProps } from "@riducms/plugin";

	let { field: binding, authoring }: PluginFieldProps<OutlineValue> = $props();
	const field = $derived(binding.schema);
	const value = $derived(binding.value);
	const nodes = $derived(readNodes(value));
	const readOnly = $derived(field.admin.readOnly === true);

	function record(value: unknown): value is Record<string, unknown> {
		return typeof value === "object" && value !== null && !Array.isArray(value);
	}
	function readNodes(value: unknown) {
		if (!record(value) || !Array.isArray(value.outline)) return [];
		return value.outline.filter(record);
	}
	function identity(node: Record<string, unknown>) {
		return record(node.content) && typeof node.content.uid === "string"
			? node.content.uid
			: undefined;
	}
	function replace(next: Record<string, unknown>[]) {
		if (readOnly) return;
		binding.set(decodeOutline({ ...(record(value) ? value : {}), outline: next }));
	}
	function add() {
		replace([
			...nodes,
			{ kind: "widget", content: { schema: "card", uid: crypto.randomUUID(), title: "New card" } },
		]);
	}
	function reverse() {
		replace([...nodes].reverse());
	}
</script>

<section data-field-path={field.path} aria-label={field.admin.label}>
	<div class="flex gap-2">
		<button type="button" onclick={add} disabled={readOnly}>Add outline card</button>
		<button type="button" onclick={reverse} disabled={readOnly || nodes.length < 2}>
			Reverse outline cards
		</button>
	</div>
	{#each nodes as node, index (identity(node) ?? index)}
		{const key = $derived(identity(node))}
		{#if key !== undefined && authoring?.schemaForm !== undefined}
			<div class="my-4 rounded border p-4" data-outline-key={key}>
				{@render authoring.schemaForm({ treeKey: "widgets", identity: key })}
			</div>
		{/if}
	{/each}
</section>
