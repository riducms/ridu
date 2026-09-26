<script lang="ts">
	import { useSortable } from "@dnd-kit-svelte/svelte/sortable";
	import type { Snippet } from "svelte";

	let {
		id,
		index,
		disabled = false,
		invalid = false,
		children,
	}: {
		id: string;
		index: number;
		disabled?: boolean;
		invalid?: boolean;
		children: Snippet<[ReturnType<typeof useSortable>]>;
	} = $props();
	const sortable = useSortable({
		id: () => id,
		index: () => index,
		group: "ridu-array",
		disabled: () => disabled,
	});
</script>

<div
	class="ridu-repeated-row-sortable"
	data-dragging={sortable.isDragging.current}
	{@attach sortable.ref}
>
	<section class="ridu-repeated-row" data-invalid={invalid}>
		{@render children(sortable)}
	</section>
</div>
