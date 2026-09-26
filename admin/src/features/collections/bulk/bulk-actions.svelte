<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";

	import { getCollectionList } from "@admin/features/collections/collection-list.svelte";

	let {
		onEdit,
		onRequestPublish,
		onRequestUnpublish,
		onRequestDelete,
		onRequestRestore,
		onRequestDeletePermanent,
	}: {
		onEdit: () => void;
		onRequestPublish: () => void;
		onRequestUnpublish: () => void;
		onRequestDelete: () => void;
		onRequestRestore: () => void;
		onRequestDeletePermanent: () => void;
	} = $props();

	const list = getCollectionList();
	const i18n = getAdminI18n();
	const { controller } = list;

	const { trashOnly } = $derived(list);
	const versioned = $derived(list.collection?.capabilities.versions === true);
	const hasBulkEditableFields = $derived(list.bulkEditableFields.length > 0);

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
		selectionLimit,
		selectionPending,
		showSelectAllMatches,
	} = $derived(controller);
</script>

{#if selectedIDs.size > 0}
	<div class="ridu-list-selection" role="region" aria-label={i18n.t("collections:bulkActions")}>
		<button
			type="button"
			class="ridu-list__text-action"
			onclick={controller.clearSelection}
			aria-label={i18n.t("collections:clearSelection")}
		>
			<span role="status" aria-live="polite">
				{i18n.t("collections:selected", { count: selectedIDs.size })}
			</span>
		</button>
		{#if showSelectAllMatches}
			<button
				type="button"
				class="ridu-list__text-action"
				aria-disabled={selectionPending || selectionLimitExceeded || !canSelectAllMatches}
				title={selectionLimitExceeded
					? i18n.t("collections:selectionLimitDescription", {
							maximum: i18n.formatNumber(selectionLimit),
						})
					: undefined}
				onclick={controller.selectAllMatches}
			>
				{selectionPending
					? i18n.t("collections:selecting")
					: selectionLimitExceeded
						? i18n.t("collections:selectAllUnavailable", {
								maximum: i18n.formatNumber(selectionLimit),
							})
						: canSelectAllMatches
							? i18n.t("collections:selectAll", {
									count: i18n.formatNumber(pagination.totalDocs),
								})
							: i18n.t("collections:allSelected", {
									count: i18n.formatNumber(pagination.totalDocs),
								})}
			</button>
		{/if}
		{#if trashOnly && canBulkRestore}
			<button
				type="button"
				class="ridu-list__text-action"
				disabled={bulkPending || selectionPending}
				onclick={onRequestRestore}
			>
				{i18n.t("documents:restore")}
			</button>
		{/if}
		{#if trashOnly && canBulkDeletePermanent}
			<button
				type="button"
				class="ridu-list__text-action"
				disabled={bulkPending || selectionPending}
				onclick={onRequestDeletePermanent}
			>
				{i18n.t("general:delete")}
			</button>
		{/if}
		{#if !trashOnly && hasBulkEditableFields && canBulkUpdate}
			<button
				type="button"
				class="ridu-list__text-action"
				disabled={bulkPending || selectionPending}
				onclick={onEdit}
			>
				{i18n.t("general:edit")}
			</button>
		{/if}
		{#if !trashOnly && versioned && canBulkPublish}
			<button
				type="button"
				class="ridu-list__text-action"
				disabled={bulkPending || selectionPending}
				onclick={onRequestPublish}
			>
				{i18n.t("documents:publish")}
			</button>
		{/if}
		{#if !trashOnly && versioned && canBulkUnpublish}
			<button
				type="button"
				class="ridu-list__text-action"
				disabled={bulkPending || selectionPending}
				onclick={onRequestUnpublish}
			>
				{i18n.t("documents:unpublish")}
			</button>
		{/if}
		{#if !trashOnly && canBulkDelete}
			<button
				type="button"
				class="ridu-list__text-action"
				disabled={bulkPending || selectionPending}
				onclick={onRequestDelete}
			>
				{i18n.t("general:delete")}
			</button>
		{/if}
	</div>
{/if}
