<script lang="ts" module>
	export type Side = "top" | "right" | "bottom" | "left";
</script>

<script lang="ts">
	import { Dialog as SheetPrimitive } from "bits-ui";
	import XIcon from "~icons/lucide/x";
	import { getAdminI18n } from "@riducms/plugin";
	import { buttonVariants } from "@admin/components/ui/button/index.js";
	import { cn, type WithoutChildrenOrChild } from "@riducms/ui";
	import SheetOverlay from "@admin/components/ui/sheet/sheet-overlay.svelte";
	import type { Snippet } from "svelte";

	let {
		ref = $bindable(null),
		class: className,
		side = "right",
		showCloseButton = true,
		portalProps,
		children,
		...restProps
	}: WithoutChildrenOrChild<SheetPrimitive.ContentProps> & {
		portalProps?: WithoutChildrenOrChild<SheetPrimitive.PortalProps>;
		side?: Side;
		showCloseButton?: boolean;
		children: Snippet;
	} = $props();
	const i18n = getAdminI18n();
</script>

<SheetPrimitive.Portal {...portalProps}>
	<SheetOverlay />
	<SheetPrimitive.Content
		bind:ref
		data-slot="sheet-content"
		data-side={side}
		class={cn(
			"fixed z-50 flex flex-col bg-popover bg-clip-padding text-sm text-popover-foreground shadow-xl data-[side=bottom]:inset-x-0 data-[side=bottom]:bottom-0 data-[side=bottom]:h-auto data-[side=bottom]:border-t data-[side=left]:inset-y-0 data-[side=left]:left-0 data-[side=left]:h-full data-[side=left]:w-3/4 data-[side=left]:border-r data-[side=right]:inset-y-0 data-[side=right]:right-0 data-[side=right]:h-full data-[side=right]:w-3/4 data-[side=right]:border-l data-[side=top]:inset-x-0 data-[side=top]:top-0 data-[side=top]:h-auto data-[side=top]:border-b data-[side=left]:sm:max-w-sm data-[side=right]:sm:max-w-sm data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[side=bottom]:data-[state=open]:slide-in-from-bottom data-[side=left]:data-[state=open]:slide-in-from-left data-[side=right]:data-[state=open]:slide-in-from-right data-[side=top]:data-[state=open]:slide-in-from-top data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[side=bottom]:data-[state=closed]:slide-out-to-bottom data-[side=left]:data-[state=closed]:slide-out-to-left data-[side=right]:data-[state=closed]:slide-out-to-right data-[side=top]:data-[state=closed]:slide-out-to-top",
			className
		)}
		{...restProps}
	>
		{@render children?.()}
		{#if showCloseButton}
			<SheetPrimitive.Close
				type="button"
				data-slot="sheet-close"
				class={buttonVariants({
					variant: "ghost",
					size: "icon-sm",
					class: "absolute top-4 end-4 bg-secondary",
				})}
			>
				<XIcon />
				<span class="sr-only">{i18n.t("general:close")}</span>
			</SheetPrimitive.Close>
		{/if}
	</SheetPrimitive.Content>
</SheetPrimitive.Portal>

<style>
	:global([data-slot="sheet-content"][data-state="open"]) {
		animation-duration: 300ms;
		animation-timing-function: cubic-bezier(0, 0, 0.2, 1);
	}

	:global([data-slot="sheet-content"][data-state="closed"]) {
		animation-duration: 220ms;
		animation-timing-function: cubic-bezier(0.4, 0, 1, 1);
	}

	@media (prefers-reduced-motion: reduce) {
		:global([data-slot="sheet-content"]) {
			animation-duration: 1ms;
		}
	}
</style>
