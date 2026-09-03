<script lang="ts">
	import type { AdminI18n } from "@riducms/plugin";

	import { BulkActionBar } from "@admin/components/ui/bulk-action-bar";
	import { Button } from "@admin/components/ui/button";
	import type { CollectionListController } from "@admin/features/collections/collection-list-controller.svelte";

	let {
		i18n,
		controller,
		trashOnly,
		versioned,
		hasBulkEditableFields,
		onEdit,
		onRequestDelete,
		onRequestRestore,
		onRequestDeletePermanent,
		onOpenDocument,
	}: {
		i18n: AdminI18n;
		controller: CollectionListController;
		trashOnly: boolean;
		versioned: boolean;
		hasBulkEditableFields: boolean;
		onEdit: () => void;
		onRequestDelete: () => void;
		onRequestRestore: () => void;
		onRequestDeletePermanent: () => void;
		onOpenDocument: (id: string) => void;
	} = $props();

	const {
		bulkPending,
		canBulkDelete,
		canBulkDeletePermanent,
		canBulkPublish,
		canBulkRestore,
		canBulkUnpublish,
		canBulkUpdate,
		canSelectAllMatches,
		pagination,
		selectedIDs,
		selectionLimitExceeded,
		selectionPending,
		showSelectAllMatches,
	} = $derived(controller);
</script>

<BulkActionBar
	count={selectedIDs.size}
	onClear={controller.clearSelection}
	label={i18n.t("collections:bulkActions")}
	selectionLabel={i18n.t("collections:selected", { count: selectedIDs.size })}
	clearLabel={i18n.t("collections:clearSelection")}
>
	{#if showSelectAllMatches}
		<Button
			variant="ghost"
			size="sm"
			aria-disabled={selectionPending || selectionLimitExceeded || !canSelectAllMatches}
			title={selectionLimitExceeded
				? i18n.t("collections:selectionLimitDescription", {
						maximum: i18n.formatNumber(100),
					})
				: undefined}
			onclick={controller.selectAllMatches}
		>
			{selectionPending
				? i18n.t("collections:selecting")
				: selectionLimitExceeded
					? i18n.t("collections:selectAllUnavailable", {
							maximum: i18n.formatNumber(100),
						})
					: canSelectAllMatches
						? i18n.t("collections:selectAll", {
								count: i18n.formatNumber(pagination.totalDocs),
							})
						: i18n.t("collections:allSelected", {
								count: i18n.formatNumber(pagination.totalDocs),
							})}
		</Button>
	{/if}
	{#if trashOnly && canBulkRestore}
		<Button
			variant="ghost"
			size="sm"
			disabled={bulkPending || selectionPending}
			onclick={onRequestRestore}
		>
			{i18n.t("documents:restore")}
		</Button>
	{/if}
	{#if trashOnly && canBulkDeletePermanent}
		<Button
			variant="destructive"
			size="sm"
			disabled={bulkPending || selectionPending}
			onclick={onRequestDeletePermanent}
		>
			{i18n.t("collections:deletePermanently")}
		</Button>
	{/if}
	{#if !trashOnly && hasBulkEditableFields && canBulkUpdate}
		<Button variant="ghost" size="sm" disabled={bulkPending || selectionPending} onclick={onEdit}>
			{i18n.t("general:edit")}
		</Button>
	{/if}
	{#if !trashOnly && versioned && canBulkPublish}
		<Button
			variant="ghost"
			size="sm"
			disabled={bulkPending || selectionPending}
			onclick={controller.bulkPublish}
		>
			{i18n.t("documents:publish")}
		</Button>
	{/if}
	{#if !trashOnly && versioned && canBulkUnpublish}
		<Button
			variant="ghost"
			size="sm"
			disabled={bulkPending || selectionPending}
			onclick={controller.bulkUnpublish}
		>
			{i18n.t("documents:unpublish")}
		</Button>
	{/if}
	{#if !trashOnly && canBulkDelete}
		<Button
			variant="ghost"
			size="sm"
			disabled={bulkPending || selectionPending}
			onclick={onRequestDelete}
		>
			{i18n.t("general:delete")}
		</Button>
	{/if}
	{#if !trashOnly && selectedIDs.size === 1}
		<Button variant="ghost" size="sm" onclick={() => onOpenDocument([...selectedIDs][0] ?? "")}>
			{i18n.t("collections:openDocument")}
		</Button>
	{/if}
</BulkActionBar>
