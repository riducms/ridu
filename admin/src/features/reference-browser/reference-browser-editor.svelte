<script lang="ts">
	import { tick } from "svelte";
	import { Button, Input, buttonVariants } from "@riducms/ui";
	import MoreVerticalIcon from "~icons/lucide/ellipsis-vertical";
	import { Banner } from "@admin/components/ui/banner";
	import { Skeleton } from "@admin/components/ui/skeleton";
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuTrigger,
	} from "@admin/components/ui/dropdown-menu";
	import JsonViewer from "@admin/components/json-tree/json-viewer.svelte";
	import { focusFieldIssue } from "@admin/core/forms/field-issue-focus";
	import DocumentFieldSections from "@admin/features/documents/document-field-sections.svelte";
	import UploadControl from "@admin/features/uploads/upload-control.svelte";
	import type { ReferenceBrowserWorkflow } from "@admin/features/reference-browser/reference-browser-workflow.svelte";

	let { controller }: { controller: ReferenceBrowserWorkflow } = $props();
	// The drawer mounts one workflow and keeps its form and runtime for that session.
	// svelte-ignore state_referenced_locally
	const { runtime } = controller.options;
	const i18n = runtime.i18n;
	// svelte-ignore state_referenced_locally
	const { form } = controller;
	const { collection, creating, editorDocument, editorLoading, editorView, editorError, canSave } =
		$derived(controller);
	const readOnly = $derived(controller.options.readOnly);
	const fields = $derived(
		controller.editorFields.map((field) =>
			readOnly ? { ...field, admin: { ...field.admin, readOnly: true } } : field
		)
	);
	const singularLabel = $derived(
		i18n.text(collection.labels.singular, collection.labels.singularTranslations)
	);
	let formElement: HTMLFormElement | undefined;

	function timestamp(value: unknown) {
		if (typeof value !== "string") return undefined;
		const date = new Date(value);
		return Number.isNaN(date.valueOf())
			? undefined
			: i18n.formatDate(date, { dateStyle: "long", timeStyle: "short" });
	}

	async function save(event: Event) {
		event.preventDefault();
		if (await controller.saveEditor()) return;

		await tick();
		if (controller.credentialIssue !== undefined) {
			formElement?.querySelector<HTMLInputElement>("[autocomplete='new-password']")?.focus();
			return;
		}

		const issue = form.issues[0];
		if (issue && formElement) await focusFieldIssue(issue.path, formElement);
	}
</script>

<form class="ridu-reference-editor" bind:this={formElement} onsubmit={save} novalidate>
	<div class="ridu-reference-bar">
		<div class="ridu-reference-metadata">
			{#if creating}
				<span>{i18n.t("documents:creatingLabel", { label: singularLabel })}</span>
			{:else}
				{const modified = $derived(timestamp(editorDocument?.updatedAt))}
				{const created = $derived(timestamp(editorDocument?.createdAt))}
				{#if modified}
					<span>
						{i18n.t("documents:lastModifiedLabel")}:
						<strong>{modified}</strong>
					</span>
				{/if}
				{#if created}
					<span>
						{i18n.t("documents:createdLabelPlain")}:
						<strong>{created}</strong>
					</span>
				{/if}
			{/if}
		</div>
		<div class="ridu-reference-actions">
			{#if !readOnly && editorView === "edit"}
				<Button type="submit" disabled={!canSave}>
					{i18n.t(form.submitting ? "reference:saving" : "general:save")}
				</Button>
			{/if}
			<DropdownMenu>
				<DropdownMenuTrigger
					type="button"
					class={buttonVariants({ variant: "ghost", size: "icon" })}
					aria-label={i18n.t("documents:moreActions")}
				>
					<MoreVerticalIcon />
				</DropdownMenuTrigger>
				<DropdownMenuContent>
					<DropdownMenuItem
						onclick={() => (controller.editorView = editorView === "edit" ? "api" : "edit")}
					>
						{i18n.t(editorView === "edit" ? "reference:api" : "reference:edit")}
					</DropdownMenuItem>
				</DropdownMenuContent>
			</DropdownMenu>
		</div>
	</div>

	<div class="ridu-reference-fields">
		{#if editorLoading}
			<div class="ridu-reference-loading" aria-label={i18n.t("reference:loadingRelated")}>
				<Skeleton class="ridu-reference-loading__input" />
				<Skeleton class="ridu-reference-loading__input" />
				<Skeleton class="ridu-reference-loading__textarea" />
			</div>
		{:else if editorView === "api"}
			<JsonViewer value={form.values} />
		{:else}
			<fieldset class="ridu-reference-form" disabled={form.submitting}>
				{#if readOnly}
					<Banner tone="warning">{i18n.t("reference:readOnlyDescription")}</Banner>
				{/if}
				{#if editorError}
					<Banner tone="destructive">{editorError}</Banner>
				{/if}
				{#if !readOnly && form.access && !form.access.operations[creating ? "create" : "update"]}
					<Banner tone="warning">
						{i18n.t(creating ? "reference:cannotCreate" : "reference:cannotUpdate", {
							label: singularLabel.toLocaleLowerCase(i18n.language),
						})}
					</Banner>
				{/if}

				{#if collection.capabilities.upload && collection.uploadSettings}
					<UploadControl
						draft={controller.upload}
						settings={collection.uploadSettings}
						editable={controller.editableUpload}
					/>
				{/if}

				{#if controller.creatingAuthUser}
					<div class="ridu-reference-credentials">
						<label>
							<span class="ridu-field-label">{i18n.t("reference:password")}</span>
							<Input
								type="password"
								autocomplete="new-password"
								required
								disabled={!form.access?.operations.create}
								aria-invalid={controller.credentialIssue !== undefined}
								bind:value={controller.newUserPassword}
								oninput={() => (controller.credentialIssue = undefined)}
							/>
						</label>
						<label>
							<span class="ridu-field-label">{i18n.t("reference:confirmPassword")}</span>
							<Input
								type="password"
								autocomplete="new-password"
								required
								disabled={!form.access?.operations.create}
								aria-invalid={controller.credentialIssue !== undefined}
								bind:value={controller.newUserPasswordConfirmation}
								oninput={() => (controller.credentialIssue = undefined)}
							/>
						</label>
						{#if controller.credentialIssue}
							<p class="ridu-field-error" role="alert">
								{controller.credentialIssue}
							</p>
						{/if}
					</div>
				{/if}

				<DocumentFieldSections {fields} {form} />
			</fieldset>
		{/if}
	</div>
</form>
