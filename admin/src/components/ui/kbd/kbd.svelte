<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";

	import { type WithElementRef } from "@riducms/ui";
	import "@admin/components/ui/kbd/kbd.scss";

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLElement>> = $props();
</script>

<kbd bind:this={ref} data-slot="kbd" class={["ridu-kbd", className]} {...restProps}>
	{@render children?.()}
</kbd>
