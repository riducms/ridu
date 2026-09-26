<script lang="ts">
	import { Dialog as SheetPrimitive } from "bits-ui";
	import "@admin/components/ui/sheet/sheet.scss";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: SheetPrimitive.TitleProps = $props();
</script>

<SheetPrimitive.Title
	bind:ref
	data-slot="sheet-title"
	class={["ridu-sheet__title", className]}
	{...restProps}
/>
