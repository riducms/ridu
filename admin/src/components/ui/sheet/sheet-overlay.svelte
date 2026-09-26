<script lang="ts">
	import { Dialog as SheetPrimitive } from "bits-ui";
	import "@admin/components/ui/sheet/sheet.scss";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: SheetPrimitive.OverlayProps = $props();
</script>

<SheetPrimitive.Overlay
	bind:ref
	data-slot="sheet-overlay"
	class={["ridu-sheet-overlay", className]}
	{...restProps}
/>
