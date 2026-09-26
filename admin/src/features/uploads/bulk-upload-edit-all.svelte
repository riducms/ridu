<script lang="ts">
	import { Dialog } from "bits-ui";
	import { getAdminI18n } from "@riducms/plugin";
	import { Button } from "@riducms/ui";
	import { untrack } from "svelte";
	import XIcon from "~icons/lucide/x";

	import FieldPicker from "@admin/features/bulk-edit/field-picker.svelte";
	import DocumentFieldSections from "@admin/features/documents/document-field-sections.svelte";
	import { bulkUploadRenderFields } from "@admin/features/uploads/bulk-upload";
	import type { BulkUploadController } from "@admin/features/uploads/bulk-upload-controller.svelte";

	let {
		controller,
		contentDirection,
	}: { controller: BulkUploadController; contentDirection: "ltr" | "rtl" } = $props();
	const i18n = getAdminI18n();
	const renderedFields = $derived(
		bulkUploadRenderFields(controller.selectedBulkEditFields, "bulk-upload-edit")
	);

	$effect(() => {
		const blocked = !controller.bulkEditOpen;
		untrack(() => (controller.bulkEditForm.writeBlocked = blocked));
		return () => {
			untrack(() => (controller.bulkEditForm.writeBlocked = false));
		};
	});
</script>

<Dialog.Root
	open={controller.bulkEditOpen}
	onOpenChange={(open) => {
		controller.bulkEditOpen = open;
	}}
>
	{#if controller.bulkEditOpen}
		<Dialog.Portal>
			<Dialog.Overlay class="ridu-bulk-edit-overlay" />
			<Dialog.Content class="ridu-bulk-edit-drawer ridu-bulk-edit-drawer--uploads">
				<div class="ridu-bulk-edit-main">
					<header class="ridu-bulk-edit-header">
						<Dialog.Title level={2} class="ridu-bulk-edit-title">
							{i18n.t("uploads:editingAssets", {
								count: controller.draftItems.length,
								formattedCount: i18n.formatNumber(controller.draftItems.length),
							})}
						</Dialog.Title>
						<Dialog.Close
							type="button"
							class="ridu-bulk-edit-close"
							aria-label={i18n.t("general:close")}
						>
							<XIcon />
						</Dialog.Close>
					</header>
					<Dialog.Description class="ridu-bulk-edit-description">
						{i18n.t("uploads:bulkEditDescription")}
					</Dialog.Description>

					<FieldPicker
						fields={controller.bulkEditFields}
						value={controller.bulkEditPaths}
						onValueChange={controller.setBulkEditPaths}
					/>

					{#if renderedFields.length > 0}
						<div class="ridu-bulk-edit-fields" dir={contentDirection}>
							<DocumentFieldSections
								fields={renderedFields}
								form={controller.bulkEditForm}
								stacked
							/>
						</div>
					{/if}
				</div>

				<aside class="ridu-bulk-edit-sidebar">
					<Button variant="outline" onclick={() => (controller.bulkEditOpen = false)}>
						{i18n.t("general:cancel")}
					</Button>
					<Button
						disabled={controller.selectedBulkEditFields.length === 0}
						onclick={controller.applyBulkEdit}
					>
						{i18n.t("uploads:applyToFiles", { count: controller.draftItems.length })}
					</Button>
				</aside>
			</Dialog.Content>
		</Dialog.Portal>
	{/if}
</Dialog.Root>
