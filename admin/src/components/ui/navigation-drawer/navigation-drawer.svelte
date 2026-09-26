<script lang="ts">
	import { Dialog } from "bits-ui";
	import type { Snippet } from "svelte";
	import { getAdminI18n } from "@riducms/plugin";
	import MenuIcon from "~icons/lucide/menu";
	import XIcon from "~icons/lucide/x";
	import "@admin/components/ui/navigation-drawer/navigation-drawer.scss";

	let { open = $bindable(false), children }: { open?: boolean; children: Snippet } = $props();

	const i18n = getAdminI18n();
</script>

<Dialog.Root bind:open>
	<Dialog.Trigger type="button" class="ridu-nav-toggle" aria-label={i18n.t("navigation:openMenu")}>
		<MenuIcon aria-hidden="true" />
	</Dialog.Trigger>
	<Dialog.Portal>
		<Dialog.Content class="ridu-navigation-drawer" dir={i18n.direction}>
			<Dialog.Title class="ridu-navigation-drawer__label">
				{i18n.t("navigation:adminNavigation")}
			</Dialog.Title>
			<Dialog.Description class="ridu-navigation-drawer__label">
				{i18n.t("navigation:adminNavigationDescription")}
			</Dialog.Description>
			<div class="ridu-navigation-drawer__header">
				<Dialog.Close
					type="button"
					class="ridu-nav-toggle"
					aria-label={i18n.t("navigation:closeMenu")}
				>
					<XIcon aria-hidden="true" />
				</Dialog.Close>
			</div>
			{@render children()}
		</Dialog.Content>
	</Dialog.Portal>
</Dialog.Root>
