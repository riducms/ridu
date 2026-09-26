<script lang="ts">
	import { type WithElementRef, type WithoutChildren } from "@riducms/ui";
	import type { HTMLAttributes } from "svelte/elements";
	import "@admin/components/ui/skeleton/skeleton.scss";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: WithoutChildren<WithElementRef<HTMLAttributes<HTMLDivElement>>> = $props();
</script>

<div
	bind:this={ref}
	data-slot="skeleton"
	class={["ridu-skeleton", className]}
	{...restProps}
	aria-hidden="true"
></div>
