<script lang="ts">
	import { Dialog as DialogPrimitive } from "bits-ui";
	import XIcon from "~icons/lucide/x";
	import { type WithoutChildrenOrChild } from "#lib/utils.js";

	import DialogOverlay from "#lib/dialog/dialog-overlay.svelte";
	import type { Snippet } from "svelte";

	let {
		ref = $bindable(null),
		class: className,
		portalProps,
		children,
		closeLabel,
		variant = "default",
		...restProps
	}: WithoutChildrenOrChild<DialogPrimitive.ContentProps> & {
		portalProps?: WithoutChildrenOrChild<DialogPrimitive.PortalProps>;
		children: Snippet;
		closeLabel?: string;
		variant?: "default" | "confirmation" | "drawer";
	} = $props();
</script>

<DialogPrimitive.Portal {...portalProps}>
	<DialogOverlay
		class={{
			"ridu-dialog-overlay--confirmation": variant === "confirmation",
			"ridu-dialog-overlay--drawer": variant === "drawer",
		}}
	/>
	<DialogPrimitive.Content
		bind:ref
		data-slot="dialog-content"
		class={[
			variant === "drawer" ? "ridu-dialog-drawer" : "ridu-dialog ridu-dialog-enter",
			{ "ridu-dialog--confirmation": variant === "confirmation" },
			className,
		]}
		{...restProps}
	>
		{@render children?.()}
		{#if closeLabel}
			<DialogPrimitive.Close type="button" data-slot="dialog-close" class="ridu-dialog__close">
				<XIcon />
				<span class="ridu-dialog__close-label">{closeLabel}</span>
			</DialogPrimitive.Close>
		{/if}
	</DialogPrimitive.Content>
</DialogPrimitive.Portal>
