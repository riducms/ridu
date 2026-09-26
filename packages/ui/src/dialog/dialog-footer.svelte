<script lang="ts">
	import "@ui/dialog/dialog.scss";
	import { type WithElementRef } from "@ui/utils";
	import type { HTMLAttributes } from "svelte/elements";

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLDivElement>> = $props();
</script>

<div
	bind:this={ref}
	data-slot="dialog-footer"
	class={["ridu-dialog__footer", className]}
	{...restProps}
>
	{@render children?.()}
</div>
