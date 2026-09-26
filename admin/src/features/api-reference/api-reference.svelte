<script lang="ts">
	import { Dialog } from "bits-ui";
	import { Button, buttonVariants } from "@riducms/ui";
	import type { SchemaCollection } from "@riducms/protocol";
	import XIcon from "~icons/lucide/x";

	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import "@admin/features/api-reference/api-reference-drawer.scss";

	let { collection, documentID }: { collection: SchemaCollection; documentID?: string } = $props();

	const runtime = getAdminRuntime();
	const i18n = runtime.i18n;
	let open = $state(false);
	let content =
		$state.raw<
			Promise<typeof import("@admin/features/api-reference/api-reference-content.svelte")>
		>();

	function load() {
		content ??= import("@admin/features/api-reference/api-reference-content.svelte");
	}

	function changeOpen(value: boolean) {
		open = value;
		if (value) load();
	}
</script>

<Dialog.Root {open} onOpenChange={changeOpen}>
	<Dialog.Trigger class={buttonVariants({ variant: "outline", size: "sm" })}>
		{i18n.t("apiReference:title")}
	</Dialog.Trigger>

	<Dialog.Portal>
		<Dialog.Overlay class="ridu-api-reference-overlay" />
		<Dialog.Content class="ridu-api-reference-drawer" dir={i18n.direction}>
			<header class="ridu-api-reference-drawer__header">
				<div>
					<Dialog.Title class="ridu-api-reference-drawer__title">
						{i18n.t("apiReference:title")}
						<code>{collection.slug}</code>
					</Dialog.Title>
					<Dialog.Description class="ridu-api-reference-drawer__description">
						{i18n.t("apiReference:description")}
					</Dialog.Description>
				</div>
				<Dialog.Close
					class={buttonVariants({ variant: "ghost", size: "icon" })}
					aria-label={i18n.t("general:close")}
				>
					<XIcon />
				</Dialog.Close>
			</header>

			{#await content}
				<p class="ridu-api-reference-drawer__message" role="status">
					{i18n.t("apiReference:loading")}
				</p>
			{:then module}
				{#if module && runtime.manifest}
					<module.default {collection} {documentID} manifest={runtime.manifest} />
				{/if}
			{:catch}
				<div class="ridu-api-reference-drawer__message" role="alert">
					<p>{i18n.t("apiReference:loadFailed")}</p>
					<Button variant="outline" onclick={() => window.location.reload()}>
						{i18n.t("general:reloadPage")}
					</Button>
				</div>
			{/await}
		</Dialog.Content>
	</Dialog.Portal>
</Dialog.Root>
