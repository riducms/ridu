<script lang="ts">
	import { Dialog as DialogPrimitive } from "bits-ui";
	import XIcon from "~icons/lucide/x";
	import { getAdminI18n } from "@riducms/plugin";
	import { buttonVariants } from "@admin/components/ui/button/index.js";
	import { cn, type WithoutChildrenOrChild } from "@riducms/ui";
	import DialogOverlay from "@admin/components/ui/dialog/dialog-overlay.svelte";
	import type { Snippet } from "svelte";

	let {
		ref = $bindable(null),
		class: className,
		portalProps,
		children,
		showCloseButton = true,
		...restProps
	}: WithoutChildrenOrChild<DialogPrimitive.ContentProps> & {
		portalProps?: WithoutChildrenOrChild<DialogPrimitive.PortalProps>;
		children: Snippet;
		showCloseButton?: boolean;
	} = $props();
	const i18n = getAdminI18n();
</script>

<DialogPrimitive.Portal {...portalProps}>
	<DialogOverlay />
	<DialogPrimitive.Content
		bind:ref
		data-slot="dialog-content"
		class={cn(
			"ridu-dialog-enter grid max-w-[calc(100%-2rem)] gap-6 rounded-[4px] border border-control-border bg-popover p-6.5 text-[14.5px] text-popover-foreground shadow-[var(--shadow-dialog)] sm:max-w-[460px] fixed top-1/2 left-1/2 z-50 w-full -translate-x-1/2 -translate-y-1/2 outline-none",
			className
		)}
		{...restProps}
	>
		{@render children?.()}
		{#if showCloseButton}
			<DialogPrimitive.Close
				type="button"
				data-slot="dialog-close"
				class={buttonVariants({
					variant: "ghost",
					size: "icon-sm",
					class: "absolute top-4 end-4",
				})}
			>
				<XIcon />
				<span class="sr-only">{i18n.t("general:close")}</span>
			</DialogPrimitive.Close>
		{/if}
	</DialogPrimitive.Content>
</DialogPrimitive.Portal>
