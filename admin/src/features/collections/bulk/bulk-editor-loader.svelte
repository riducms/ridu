<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import { Button } from "@riducms/ui";
	import { Dialog } from "bits-ui";
	import type { SchemaCollection, SchemaField } from "@riducms/protocol";

	import { getCollectionList } from "@admin/features/collections/collection-list.svelte";

	let { open = $bindable(false) }: { open?: boolean } = $props();

	const i18n = getAdminI18n();
	const list = getCollectionList();
	let editor = $state.raw<typeof import("@admin/features/collections/bulk/bulk-editor.svelte")>();
	let status = $state<"idle" | "loading" | "failed">("idle");
	let owner:
		| {
				slug: string;
				locale: string | undefined;
				collection: SchemaCollection | undefined;
				fields: readonly SchemaField[];
		  }
		| undefined;

	$effect(() => {
		const slug = list.slug;
		const locale = list.contentLocale;
		const collection = list.collection;
		const fields = list.bulkEditableFields;
		if (!open) {
			owner = undefined;
			return;
		}
		if (owner === undefined) owner = { slug, locale, collection, fields };
		else if (
			owner.slug !== slug ||
			owner.locale !== locale ||
			owner.collection !== collection ||
			owner.fields !== fields
		) {
			open = false;
			owner = undefined;
			return;
		}

		if (!open || editor !== undefined || status !== "idle") return;

		status = "loading";
		import("@admin/features/collections/bulk/bulk-editor.svelte")
			.then((loaded) => {
				editor = loaded;
				status = "idle";
			})
			.catch(() => {
				status = "failed";
			});
	});

	function changeOpen(next: boolean) {
		open = next;
	}
</script>

{#if editor !== undefined}
	<editor.default bind:open />
{:else if open}
	<Dialog.Root {open} onOpenChange={changeOpen}>
		<Dialog.Portal>
			<Dialog.Overlay class="ridu-list-bulk-module-overlay" />
			<Dialog.Content class="ridu-list-bulk-module" dir={i18n.direction}>
				<header class="ridu-list-bulk-module__header">
					<Dialog.Title class="ridu-list-bulk-module__title">
						{i18n.t(status === "failed" ? "collections:workspaceLoadFailed" : "general:loading")}
					</Dialog.Title>
					<Dialog.Description class="ridu-list-bulk-module__description">
						{i18n.t(
							status === "failed"
								? "collections:workspaceLoadFailed"
								: "collections:bulkEditDescription"
						)}
					</Dialog.Description>
				</header>
				<footer class="ridu-list-bulk-module__actions">
					<Button variant="outline" onclick={() => (open = false)}>
						{i18n.t("general:close")}
					</Button>
					{#if status === "failed"}
						<Button onclick={() => window.location.reload()}>
							{i18n.t("general:reloadPage")}
						</Button>
					{/if}
				</footer>
			</Dialog.Content>
		</Dialog.Portal>
	</Dialog.Root>
{/if}
