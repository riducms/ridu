<script lang="ts">
	import { Dialog } from "bits-ui";
	import { Link } from "@hvniel/svelte-router";
	import { getAdminI18n } from "@riducms/plugin";
	import { Button, buttonVariants } from "@riducms/ui";
	import { tick, untrack } from "svelte";
	import ChevronLeftIcon from "~icons/lucide/chevron-left";
	import ChevronRightIcon from "~icons/lucide/chevron-right";
	import XIcon from "~icons/lucide/x";

	import { Banner } from "@admin/components/ui/banner";
	import { focusFieldIssue } from "@admin/core/forms/field-issue-focus";
	import { documentPath, withContentLocale } from "@admin/core/routing/admin-paths";
	import DocumentFieldSections from "@admin/features/documents/document-field-sections.svelte";
	import UploadDocumentPreview from "@admin/features/documents/upload-document-preview-loader.svelte";
	import type { BulkUploadController } from "@admin/features/uploads/bulk-upload-controller.svelte";

	let {
		controller,
		closePath,
		contentDirection,
	}: {
		controller: BulkUploadController;
		closePath: string;
		contentDirection: "ltr" | "rtl";
	} = $props();
	const i18n = getAdminI18n();
	const formID = $props.id();
	// Only submit/retry handlers read this DOM binding.
	// svelte-ignore non_reactive_update
	let formElement: HTMLFormElement;
	const activeItem = $derived(controller.activeItem);

	$effect(() => {
		const item = activeItem;
		if (item === undefined) return;
		const blocked =
			item.status === "complete" || item.status === "uncertain" || !controller.canCreate;
		untrack(() => (item.form.writeBlocked = blocked));
		return () => {
			untrack(() => (item.form.writeBlocked = false));
		};
	});

	async function save(event: SubmitEvent) {
		event.preventDefault();
		await saveAll();
	}

	async function saveAll() {
		await controller.saveAll();
		await focusActiveIssue();
	}

	async function retry() {
		const item = controller.activeItem;
		if (item === undefined) return;
		await controller.retry(item);
		await focusActiveIssue();
	}

	async function focusActiveIssue() {
		await tick();
		const issue = controller.activeItem?.form.issues[0];
		if (issue !== undefined && formElement) await focusFieldIssue(issue.path, formElement);
	}
</script>

<main class="ridu-bulk-upload-editor">
	<header class="ridu-bulk-upload-header">
		<Dialog.Title level={1} id="bulk-upload-title" class="ridu-bulk-upload-header__title">
			{controller.collection === undefined
				? i18n.t("uploads:asset")
				: i18n.text(
						controller.collection.labels.singular,
						controller.collection.labels.singularTranslations
					)}
		</Dialog.Title>
		<Link
			class={buttonVariants({ variant: "ghost", size: "icon-sm" })}
			to={closePath}
			aria-label={i18n.t("general:close")}
		>
			<XIcon />
		</Link>
	</header>

	<div class="ridu-bulk-upload-actions">
		<div class="ridu-bulk-upload-actions__navigation">
			<p>
				<strong>{i18n.formatNumber(controller.activeIndex + 1)}</strong>
				{i18n.t("uploads:ofFiles", {
					count: controller.queue.length,
					formattedCount: i18n.formatNumber(controller.queue.length),
				})}
			</p>
			<Button
				type="button"
				variant="ghost"
				size="icon-xs"
				aria-label={i18n.t("general:previous")}
				disabled={controller.queue.length < 2 || controller.running}
				onclick={controller.previous}
			>
				<ChevronLeftIcon class="ridu-bulk-upload-actions__chevron" />
			</Button>
			<Button
				type="button"
				variant="ghost"
				size="icon-xs"
				aria-label={i18n.t("general:next")}
				disabled={controller.queue.length < 2 || controller.running}
				onclick={controller.next}
			>
				<ChevronRightIcon class="ridu-bulk-upload-actions__chevron" />
			</Button>
			<Button
				type="button"
				variant="outline"
				size="sm"
				disabled={controller.running ||
					controller.draftItems.length === 0 ||
					controller.bulkEditFields.length === 0}
				onclick={controller.openBulkEdit}
			>
				{i18n.t("uploads:editAll")}
			</Button>
		</div>
		<Button
			type="submit"
			form={formID}
			disabled={controller.running ||
				controller.preparing ||
				controller.pendingCount === 0 ||
				!controller.canCreate}
		>
			{i18n.t(controller.running ? "uploads:uploading" : "general:save")}
		</Button>
	</div>

	{#if activeItem}
		{#key activeItem.id}
			<form
				bind:this={formElement}
				id={formID}
				class="ridu-bulk-upload-editor__form"
				novalidate
				onsubmit={save}
			>
				{#if activeItem.error}
					<Banner tone={activeItem.status === "uncertain" ? "warning" : "destructive"}>
						<div class="ridu-bulk-upload-editor__error">
							<span>{activeItem.error}</span>
							{#if activeItem.status === "failed"}
								<Button type="button" size="xs" variant="outline" onclick={retry}>
									{i18n.t("general:retry")}
								</Button>
							{/if}
						</div>
					</Banner>
				{/if}

				{#if activeItem.documentID}
					<Link
						class="ridu-bulk-upload-editor__result"
						to={withContentLocale(
							documentPath(controller.collection?.slug ?? "", activeItem.documentID),
							controller.locale
						)}
					>
						{i18n.t("uploads:openAsset")}
					</Link>
				{/if}

				<fieldset
					dir={contentDirection}
					disabled={!activeItem.editable || controller.running || !controller.canCreate}
				>
					{#if controller.collection?.uploadSettings}
						<UploadDocumentPreview
							draft={activeItem.upload}
							settings={controller.collection.uploadSettings}
							editable={activeItem.editable && controller.canCreate && !controller.running}
						/>
					{/if}
					<DocumentFieldSections fields={controller.editableFields} form={activeItem.form} />
				</fieldset>
			</form>
		{/key}
	{/if}
</main>
