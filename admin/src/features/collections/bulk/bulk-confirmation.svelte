<script module lang="ts">
	export type BulkConfirmationAction =
		| "publish-selected"
		| "unpublish-selected"
		| "delete-selected"
		| "restore-selected"
		| "delete-selected-permanently"
		| "empty-trash";
</script>

<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import { ConfirmationDialog } from "@admin/components/ui/confirmation-dialog";
	import { getCollectionList } from "@admin/features/collections/collection-list.svelte";

	let {
		open = $bindable(false),
		action,
	}: {
		open?: boolean;
		action: BulkConfirmationAction;
	} = $props();

	const list = getCollectionList();
	const { controller } = list;
	const i18n = getAdminI18n();

	const { selectedIDs, bulkPending } = $derived(controller);
	const copy = $derived(confirmationText(action));

	async function performConfirmation() {
		switch (action) {
			case "publish-selected":
				await controller.bulkPublish();
				break;

			case "unpublish-selected":
				await controller.bulkUnpublish();
				break;

			case "restore-selected":
				await controller.bulkRestore();
				break;

			case "delete-selected-permanently":
				await controller.bulkDeletePermanent();
				break;

			case "empty-trash":
				await controller.emptyTrash();
				break;

			default:
				action satisfies "delete-selected";
				await controller.bulkDelete();
		}
	}

	function confirmationText(action: BulkConfirmationAction) {
		const count = selectedIDs.size;
		switch (action) {
			case "publish-selected":
				return {
					title: i18n.t("collections:confirmPublishSelected"),
					description: i18n.t("collections:publishSelectedDescription", {
						label:
							list.collection === undefined
								? i18n.t("collections:documents")
								: i18n.text(
										list.collection.labels.plural,
										list.collection.labels.pluralTranslations
									),
					}),
					confirmLabel: i18n.t("general:confirm"),
					destructive: false,
				};

			case "unpublish-selected":
				return {
					title: i18n.t("collections:confirmUnpublishSelected"),
					description: i18n.t("collections:unpublishSelectedDescription", {
						label:
							list.collection === undefined
								? i18n.t("collections:documents")
								: i18n.text(
										list.collection.labels.plural,
										list.collection.labels.pluralTranslations
									),
					}),
					confirmLabel: i18n.t("general:confirm"),
					destructive: false,
				};

			case "restore-selected":
				return {
					title: i18n.t("collections:confirmRestoreSelected", {
						count,
						formattedCount: i18n.formatNumber(count),
					}),
					description: i18n.t("collections:restoreSelectedDescription"),
					confirmLabel: i18n.t("documents:restore"),
					destructive: false,
				};

			case "delete-selected-permanently":
				return {
					title: i18n.t("collections:confirmDeleteSelectedPermanently", {
						count,
						formattedCount: i18n.formatNumber(count),
					}),
					description: i18n.t("collections:cannotUndo"),
					confirmLabel: i18n.t("collections:deletePermanently"),
					destructive: true,
				};

			case "empty-trash": {
				return {
					title: i18n.t("collections:confirmEmptyTrash", {
						label:
							list.collection?.labels.plural.toLocaleLowerCase(i18n.language) ??
							i18n.t("collections:documents"),
					}),
					description: i18n.t("collections:emptyTrashDescription"),
					confirmLabel: i18n.t("collections:emptyTrash"),
					destructive: true,
				};
			}

			default:
				action satisfies "delete-selected";
				return {
					title: i18n.t("collections:confirmDeleteSelected", {
						count,
						formattedCount: i18n.formatNumber(count),
					}),
					description: i18n.t("collections:deleteSelectedDescription"),
					confirmLabel: i18n.t("general:delete"),
					destructive: true,
				};
		}
	}
</script>

<ConfirmationDialog
	bind:open
	title={copy.title}
	description={copy.description}
	confirmLabel={copy.confirmLabel}
	cancelLabel={i18n.t("general:cancel")}
	destructive={copy.destructive}
	disabled={bulkPending}
	onconfirm={performConfirmation}
/>
