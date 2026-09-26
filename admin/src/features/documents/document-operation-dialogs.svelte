<script lang="ts">
	import { Link } from "@hvniel/svelte-router";
	import {
		Button,
		buttonVariants,
		Dialog,
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle,
	} from "@riducms/ui";
	import { collectionPath } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import type { DocumentController } from "@admin/features/documents/document-controller.svelte";
	const runtime = getAdminRuntime();
	let { controller }: { controller: DocumentController } = $props();
	let dismissedLock = $state<string>();
	const lockKey = $derived(
		controller.lock.lockedByAnotherEditor
			? `${controller.collectionSlug}:${controller.documentID ?? ""}:${controller.lock.ownerLabel ?? ""}:${controller.lock.updatedAt ?? ""}`
			: undefined
	);
</script>

{#if !controller.globalResource && controller.canDelete}
	{const deleteConfirmation = $derived(
		controller.collection?.capabilities.trash === true
			? {
					title: runtime.i18n.t("documents:moveToTrashQuestion"),
					description: runtime.i18n.t("documents:moveToTrashDescription", {
						label: controller.documentHeading,
					}),
					action: runtime.i18n.t("documents:moveToTrash"),
				}
			: {
					title: runtime.i18n.t("documents:deleteQuestion"),
					description: runtime.i18n.t("documents:deleteDescription", {
						label: controller.documentHeading,
					}),
					action: runtime.i18n.t("documents:deleteDocument"),
				}
	)}

	<Dialog bind:open={controller.deleteDialogOpen}>
		<DialogContent variant="confirmation">
			<DialogHeader>
				<DialogTitle>
					{deleteConfirmation.title}
				</DialogTitle>
				<DialogDescription>
					{deleteConfirmation.description}
				</DialogDescription>
			</DialogHeader>
			<DialogFooter>
				<Button variant="outline" onclick={() => (controller.deleteDialogOpen = false)}>
					{runtime.i18n.t("documents:cancel")}
				</Button>
				<Button onclick={controller.remove}>
					{deleteConfirmation.action}
				</Button>
			</DialogFooter>
		</DialogContent>
	</Dialog>
{/if}

<Dialog
	open={lockKey !== undefined && dismissedLock !== lockKey}
	onOpenChange={(open) => {
		if (!open) dismissedLock = lockKey;
	}}
>
	<DialogContent variant="confirmation">
		<DialogHeader>
			<DialogTitle>
				{runtime.i18n.t("documents:currentlyEditing", {
					owner: controller.lock.ownerLabel ?? runtime.i18n.t("documents:anotherEditor"),
				})}
			</DialogTitle>
			<DialogDescription>
				{controller.lock.updatedAt === undefined
					? runtime.i18n.t("documents:editingLeaseActive")
					: runtime.i18n.t("documents:editedSince", {
							date: controller.documentDate(controller.lock.updatedAt),
						})}
				{controller.lock.canTakeOver
					? runtime.i18n.t("documents:lockOptionsWithTakeover")
					: runtime.i18n.t("documents:lockOptionsReadOnly")}
			</DialogDescription>
		</DialogHeader>
		<DialogFooter>
			<Link
				class={buttonVariants({ variant: "ghost" })}
				to={collectionPath(controller.collectionSlug)}
			>
				{runtime.i18n.t("documents:goBack")}
			</Link>
			<Button variant="outline" onclick={() => (dismissedLock = lockKey)}>
				{runtime.i18n.t("documents:viewReadOnly")}
			</Button>
			{#if controller.lock.canTakeOver}
				<Button disabled={controller.lock.operation} onclick={controller.lock.takeOver}>
					{controller.lock.operation
						? runtime.i18n.t("documents:takingOver")
						: runtime.i18n.t("documents:takeOver")}
				</Button>
			{/if}
		</DialogFooter>
	</DialogContent>
</Dialog>
