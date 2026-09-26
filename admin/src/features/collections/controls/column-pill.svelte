<script lang="ts">
	import { useSortable } from "@dnd-kit-svelte/svelte/sortable";
	import Plus from "@admin/components/icons/plus.svelte";
	import X from "@admin/components/icons/x.svelte";

	let {
		path,
		label,
		index,
		selected,
		onToggle,
		reorderLabel,
		disabled,
	}: {
		path: string;
		label: string;
		index: number;
		selected: boolean;
		onToggle: () => void;
		reorderLabel: string;
		disabled: boolean;
	} = $props();

	const sortable = useSortable({ id: () => path, index: () => index, group: "list-columns" });
</script>

<div
	class={[
		"ridu-list-columns__pill",
		sortable.isDragging.current && "ridu-list-columns__pill--dragging",
	]}
	data-selected={selected}
	{@attach sortable.ref}
>
	<button
		class="ridu-list-columns__toggle"
		type="button"
		{disabled}
		aria-pressed={selected}
		onclick={onToggle}
	>
		{#if selected}
			<X />
		{:else}
			<Plus />
		{/if}
		{label}
	</button>
	<button
		class="ridu-list-columns__handle"
		type="button"
		aria-label={reorderLabel}
		{@attach sortable.handleRef}
	>
		<svg width="12" height="16" viewBox="0 0 12 16" aria-hidden="true">
			<path
				d="M4 4h1v1H4zm3 0h1v1H7zM4 8h1v1H4zm3 0h1v1H7zM4 12h1v1H4zm3 0h1v1H7z"
				fill="currentColor"
			/>
		</svg>
	</button>
</div>
