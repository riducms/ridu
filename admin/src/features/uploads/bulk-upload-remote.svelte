<script lang="ts">
	import { Link } from "@hvniel/svelte-router";
	import { getAdminI18n } from "@riducms/plugin";
	import { Button, Input } from "@riducms/ui";
	import { tick, untrack } from "svelte";

	import { Banner } from "@admin/components/ui/banner";
	import { focusFieldIssue } from "@admin/core/forms/field-issue-focus";
	import { documentPath, withContentLocale } from "@admin/core/routing/admin-paths";
	import { bulkUploadRenderFields } from "@admin/features/uploads/bulk-upload";
	import type { BulkUploadController } from "@admin/features/uploads/bulk-upload-controller.svelte";
	import DocumentFieldSections from "@admin/features/documents/document-field-sections.svelte";

	let {
		controller,
		contentDirection,
	}: { controller: BulkUploadController; contentDirection: "ltr" | "rtl" } = $props();
	const i18n = getAdminI18n();
	const renderedFields = $derived(
		bulkUploadRenderFields(controller.editableFields, "bulk-upload-remote")
	);

	$effect(() => {
		const blocked =
			controller.remoteOutcomeUncertain || !controller.uploadEnabled || !controller.canCreate;
		untrack(() => (controller.remoteForm.writeBlocked = blocked));
		return () => {
			untrack(() => (controller.remoteForm.writeBlocked = false));
		};
	});

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		const form = event.currentTarget;
		if (!(form instanceof HTMLFormElement)) return;
		const urlInput = form.elements.namedItem("remoteURL");
		if (!(urlInput instanceof HTMLInputElement) || !urlInput.reportValidity()) return;

		await controller.uploadRemote();
		const issue = controller.remoteForm.issues[0];
		if (issue !== undefined) {
			await tick();
			await focusFieldIssue(issue.path, form);
		}
	}
</script>

<details class="ridu-bulk-upload-remote">
	<summary>{i18n.t("uploads:pasteURL")}</summary>
	<form novalidate onsubmit={submit}>
		<p>{i18n.t("uploads:remoteImportDescription")}</p>
		<div class="ridu-bulk-upload-remote__url">
			<Input
				type="url"
				name="remoteURL"
				required
				aria-label={i18n.t("uploads:assetURL")}
				placeholder="https://"
				value={controller.remoteURL}
				disabled={controller.remotePending || !controller.uploadEnabled || !controller.canCreate}
				oninput={(event) => controller.setRemoteURL(event.currentTarget.value)}
				onchange={controller.prepareRemoteDefaults}
			/>
		</div>

		{#if controller.remoteError}
			<Banner tone="destructive">{controller.remoteError}</Banner>
		{/if}

		{#if controller.remoteDocumentID && !controller.remoteForm.dirty}
			<Link
				class="ridu-bulk-upload-remote__result"
				to={withContentLocale(
					documentPath(controller.collection?.slug ?? "", controller.remoteDocumentID),
					controller.locale
				)}
			>
				{i18n.t("uploads:openImportedAsset")}
			</Link>
		{/if}

		<fieldset
			dir={contentDirection}
			disabled={controller.remotePending || !controller.uploadEnabled || !controller.canCreate}
		>
			<DocumentFieldSections fields={renderedFields} form={controller.remoteForm} stacked />
		</fieldset>

		<Button
			type="submit"
			disabled={controller.remotePending ||
				controller.remoteOutcomeUncertain ||
				!controller.uploadEnabled ||
				!controller.canCreate ||
				controller.remoteURL.trim() === ""}
		>
			{i18n.t(controller.remotePending ? "uploads:importing" : "uploads:importFromURL")}
		</Button>
	</form>
</details>
