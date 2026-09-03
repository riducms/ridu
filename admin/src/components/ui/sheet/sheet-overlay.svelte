<script lang="ts">
	import { Dialog as SheetPrimitive } from "bits-ui";
	import { cn } from "@riducms/ui";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: SheetPrimitive.OverlayProps = $props();
</script>

<SheetPrimitive.Overlay
	bind:ref
	data-slot="sheet-overlay"
	class={cn(
		"fixed inset-0 z-50 bg-backdrop supports-backdrop-filter:backdrop-blur-sm data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=closed]:animate-out data-[state=closed]:fade-out-0",
		className
	)}
	{...restProps}
/>

<style>
	:global([data-slot="sheet-overlay"][data-state="open"]) {
		animation-duration: 240ms;
	}

	:global([data-slot="sheet-overlay"][data-state="closed"]) {
		animation-duration: 180ms;
	}

	@media (prefers-reduced-motion: reduce) {
		:global([data-slot="sheet-overlay"]) {
			animation-duration: 1ms;
		}
	}
</style>
