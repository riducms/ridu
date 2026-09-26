<script lang="ts">
	import { Dialog as SheetPrimitive } from "bits-ui";
	import "@admin/components/ui/sheet/sheet.scss";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: SheetPrimitive.DescriptionProps = $props();
</script>

<SheetPrimitive.Description
	bind:ref
	data-slot="sheet-description"
	class={["ridu-sheet__description", className]}
	{...restProps}
/>
