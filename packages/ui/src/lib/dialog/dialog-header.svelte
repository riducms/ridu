<script lang="ts">
	import "#lib/dialog/dialog.scss";
	import { type WithElementRef } from "#lib/utils.js";
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
	data-slot="dialog-header"
	class={["ridu-dialog__header", className]}
	{...restProps}
>
	{@render children?.()}
</div>
