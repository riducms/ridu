<script lang="ts" module>
	export type Side = "top" | "right" | "bottom" | "left";
</script>

<script lang="ts">
	import { Dialog as SheetPrimitive } from "bits-ui";
	import XIcon from "~icons/lucide/x";
	import { getAdminI18n } from "@riducms/plugin";
	import { type WithoutChildrenOrChild } from "@riducms/ui";

	import SheetOverlay from "@admin/components/ui/sheet/sheet-overlay.svelte";
	import "@admin/components/ui/sheet/sheet.scss";
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
		class={["ridu-sheet", className]}
		{...restProps}
	>
		{@render children?.()}
		{#if showCloseButton}
			<SheetPrimitive.Close type="button" data-slot="sheet-close" class="ridu-sheet__close">
				<XIcon />
				<span class="ridu-sheet__close-label">{i18n.t("general:close")}</span>
			</SheetPrimitive.Close>
		{/if}
	</SheetPrimitive.Content>
</SheetPrimitive.Portal>
