<script lang="ts">
	import { type WithElementRef, type WithoutChildren } from "@ui/utils";
	import type { HTMLTextareaAttributes } from "svelte/elements";

	import "@ui/textarea/textarea.scss";

	let {
		ref = $bindable(null),
		value = $bindable(),
		class: className,
		"data-slot": dataSlot = "textarea",
		...restProps
	}: WithoutChildren<WithElementRef<HTMLTextareaAttributes>> = $props();
</script>

<textarea
	bind:this={ref}
	data-slot={dataSlot}
	class={["ridu-textarea", className]}
	bind:value
	{...restProps}></textarea>
