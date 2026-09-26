<script lang="ts">
	import { Dialog as DialogPrimitive } from "bits-ui";
	import "@ui/dialog/dialog.scss";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: DialogPrimitive.DescriptionProps = $props();
</script>

<DialogPrimitive.Description
	bind:ref
	data-slot="dialog-description"
	class={["ridu-dialog__description", className]}
	{...restProps}
/>
