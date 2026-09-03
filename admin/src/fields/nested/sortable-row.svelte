<script lang="ts">
	import { useSortable } from "@dnd-kit-svelte/svelte/sortable";
	import type { Snippet } from "svelte";

	let {
		id,
		index,
		disabled = false,
		children,
	}: {
		id: string;
		index: number;
		disabled?: boolean;
		children: Snippet<[ReturnType<typeof useSortable>]>;
	} = $props();
	const sortable = useSortable({
		id: () => id,
		index: () => index,
		group: "ridu-array",
		disabled: () => disabled,
	});
</script>

<section
	class={[
		"grid overflow-hidden rounded-[4px] border border-control-border bg-background transition-colors hover:border-control-border-hover",
		sortable.isDragging.current && "opacity-50",
	]}
	{@attach sortable.ref}
>
	{@render children(sortable)}
</section>
