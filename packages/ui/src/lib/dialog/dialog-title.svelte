<script lang="ts">
	import "#lib/dialog/dialog.scss";
	import { Dialog as DialogPrimitive } from "bits-ui";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: DialogPrimitive.TitleProps = $props();
</script>

<DialogPrimitive.Title
	bind:ref
	data-slot="dialog-title"
	class={["ridu-dialog__title", className]}
	{...restProps}
/>
