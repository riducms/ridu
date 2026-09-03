<script lang="ts">
	import { Dialog as DialogPrimitive } from "bits-ui";
	import { cn } from "@riducms/ui";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: DialogPrimitive.OverlayProps = $props();
</script>

<DialogPrimitive.Overlay
	bind:ref
	data-slot="dialog-overlay"
	class={cn(
		"bg-[rgba(8,7,6,0.55)] supports-backdrop-filter:backdrop-blur-[4px] data-[state=open]:animate-fade-in data-[state=open]:duration-150 fixed inset-0 isolate z-50",
		className
	)}
	{...restProps}
/>
