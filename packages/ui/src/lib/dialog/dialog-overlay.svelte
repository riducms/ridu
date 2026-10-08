<script lang="ts">
	import { Dialog as DialogPrimitive } from "bits-ui";
	import "#lib/dialog/dialog.scss";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: DialogPrimitive.OverlayProps = $props();
</script>

<DialogPrimitive.Overlay
	bind:ref
	data-slot="dialog-overlay"
	class={["ridu-dialog-overlay", className]}
	{...restProps}
/>
