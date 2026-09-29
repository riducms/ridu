<script lang="ts">
	import { Dialog } from "bits-ui";
	import { Button, Input, buttonVariants } from "@riducms/ui";
	import { getAdminI18n } from "@riducms/plugin";
	import { isRecord, type SchemaUploadSettings } from "@riducms/protocol";
	import XIcon from "~icons/lucide/x";
	import CopyIcon from "~icons/lucide/copy";
	import FileIcon from "~icons/lucide/file";
	import { formatFileSize } from "@admin/core/i18n/format-file-size";
	import type { UploadDraft } from "@admin/features/uploads/upload-draft.svelte";
	import RenditionPreview from "@admin/features/uploads/rendition-preview.svelte";
	import UploadThumbnail from "@admin/features/uploads/upload-thumbnail.svelte";
	import "@admin/features/uploads/upload.scss";

	let {
		draft,
		settings,
		editable = false,
	}: { draft: UploadDraft; settings: SchemaUploadSettings; editable?: boolean } = $props();
	const i18n = getAdminI18n();
	let chooser: HTMLInputElement;
	let urlMode = $state(false);
	let remoteURL = $state("");
	let dragging = $state(false);
	let dialog = $state<"edit" | "sizes">("edit");
	let dialogOpen = $state(false);
	let copied = $state(false);
	const sizes = $derived(
		isRecord(draft.document?.sizes)
			? Object.entries(draft.document.sizes).filter(
					(entry): entry is [string, Record<string, unknown>] => isRecord(entry[1])
				)
			: []
	);
	const renditions = $derived<[string, Record<string, unknown>][]>(
		draft.document ? [[i18n.t("uploads:original"), draft.document], ...sizes] : sizes
	);
	const bytes = $derived(draft.file?.size ?? Number(draft.document?.filesize ?? 0));
	const dimensions = $derived(
		draft.source
			? `${Math.round(((draft.currentImage.cropWidth || 100) / 100) * draft.source.width)} × ${Math.round(((draft.currentImage.cropHeight || 100) / 100) * draft.source.height)}`
			: `${draft.document?.width} × ${draft.document?.height}`
	);

	$effect(() => {
		const editingImage = draft.editingImage;
		if (!editable) {
			dragging = false;
			urlMode = false;
			remoteURL = "";
			if (editingImage || draft.busy) draft.endImageEdit();
			if (dialog === "edit") dialogOpen = false;
		}
		if (dialog === "edit" && dialogOpen && !editingImage) dialogOpen = false;
	});
	$effect(() => () => draft.endImageEdit());

	function closeImageEditor() {
		dialogOpen = false;
		draft.endImageEdit();
	}

	async function importURL() {
		if (!editable || draft.busy) return;
		await draft.fromURL(remoteURL, settings);
		if (draft.file) {
			urlMode = false;
			remoteURL = "";
		}
	}

	function choose(event: Event) {
		if (!(event.currentTarget instanceof HTMLInputElement)) return;
		const file = event.currentTarget.files?.[0];
		if (editable && !draft.busy && file) draft.select(file, settings);
		event.currentTarget.value = "";
	}

	function drop(event: DragEvent) {
		event.preventDefault();
		dragging = false;
		const file = event.dataTransfer?.files[0];
		if (editable && !draft.busy && file) draft.select(file, settings);
	}

	async function copyURL() {
		try {
			await navigator.clipboard.writeText(
				new URL(String(draft.document?.url ?? ""), window.location.origin).href
			);
			copied = true;
		} catch {
			copied = false;
		}
	}
</script>

<!-- File drops are an alternative to the keyboard-accessible chooser below. -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
	role="region"
	aria-label={i18n.t("uploads:assetPreview")}
	class={["ridu-upload", { "ridu-upload--dragging": dragging }]}
	ondragover={(event) => {
		event.preventDefault();
		if (editable) dragging = true;
	}}
	ondragleave={(event) => {
		if (!event.currentTarget.contains(event.relatedTarget as Node | null)) dragging = false;
	}}
	ondrop={drop}
>
	<input
		bind:this={chooser}
		hidden
		type="file"
		accept={settings.mimeTypes.join(",")}
		aria-label={i18n.t("uploads:chooseFile")}
		tabindex="-1"
		disabled={!editable || draft.busy}
		onchange={choose}
	/>

	{#if draft.present}
		<div class="ridu-upload-thumbnail">
			{#if draft.mimeType.startsWith("image/")}
				<UploadThumbnail {draft} />
			{:else}
				<FileIcon />
			{/if}
		</div>
		<div class="ridu-upload-details">
			<div class="ridu-upload-name">
				{#if draft.file}
					<Input
						aria-label={i18n.t("uploads:filename")}
						disabled={!editable}
						bind:value={() => draft.filename, draft.setFilename}
					/>
				{:else}
					<a href={String(draft.document?.url ?? "")} target="_blank" rel="noreferrer">
						{draft.filename}
					</a>
					<Button
						variant="ghost"
						size="icon-xs"
						aria-label={i18n.t(copied ? "general:copied" : "general:copy")}
						onclick={copyURL}
					>
						<CopyIcon />
					</Button>
				{/if}
			</div>
			<p>
				{formatFileSize(bytes, i18n)}
				{#if draft.mimeType.startsWith("image/")}
					· {dimensions}
				{/if}
				· {draft.mimeType}
			</p>
			<div class="ridu-upload-actions">
				{#if !draft.file && sizes.length}
					<Button
						size="xs"
						variant="secondary"
						onclick={() => {
							dialog = "sizes";
							dialogOpen = true;
						}}
					>
						{i18n.t("uploads:previewSizes")}
					</Button>
				{/if}
				{#if editable && draft.editableImage}
					<Button
						size="xs"
						variant="secondary"
						disabled={draft.busy}
						onclick={() => {
							dialog = "edit";
							dialogOpen = true;
							draft.beginImageEdit();
						}}
					>
						{i18n.t("uploads:editImage")}
					</Button>
				{/if}
			</div>
		</div>
		{#if editable}
			<Button
				class="ridu-upload-remove"
				size="icon-xs"
				variant="ghost"
				aria-label={i18n.t("uploads:removeFilename", { filename: draft.filename })}
				disabled={draft.busy}
				onclick={draft.remove}
			>
				<XIcon />
			</Button>
		{/if}
	{:else if urlMode}
		<div class="ridu-upload-url">
			<Input
				type="url"
				aria-label={i18n.t("uploads:pasteURL")}
				placeholder="https://"
				bind:value={remoteURL}
				disabled={!editable || draft.busy}
				onkeydown={(event) => {
					if (event.key === "Enter") {
						event.preventDefault();
						importURL();
					}
				}}
			/>
			<Button
				size="sm"
				variant="secondary"
				disabled={!editable || draft.busy || !remoteURL.trim()}
				onclick={importURL}
			>
				{i18n.t(draft.busy ? "uploads:importing" : "uploads:addFile")}
			</Button>
			<Button
				size="icon-sm"
				variant="ghost"
				aria-label={i18n.t("general:cancel")}
				onclick={() => {
					urlMode = false;
					draft.cancelPending();
				}}
			>
				<XIcon />
			</Button>
		</div>
	{:else}
		<div class="ridu-upload-choose">
			<Button
				size="sm"
				variant="secondary"
				disabled={!editable || draft.busy}
				onclick={() => chooser.click()}
			>
				{i18n.t("uploads:chooseFile")}
			</Button>
			<Button
				size="sm"
				variant="secondary"
				disabled={!editable || draft.busy}
				onclick={() => {
					urlMode = true;
				}}
			>
				{i18n.t("uploads:pasteURL")}
			</Button>
			<span>{i18n.t("uploads:dropFile")}</span>
		</div>
	{/if}
</div>
{#if draft.error}
	<p role="alert" class="ridu-upload-error">{draft.error}</p>
{/if}

<Dialog.Root
	open={dialogOpen}
	onOpenChange={(open) => {
		if (!open) {
			dialogOpen = false;
			if (dialog === "edit") draft.endImageEdit();
			else draft.cancelPending();
		}
	}}
>
	<Dialog.Portal>
		<Dialog.Overlay class="ridu-upload-drawer-overlay" />
		<Dialog.Content class="ridu-upload-drawer">
			<header class="ridu-upload-drawer-header">
				<Dialog.Title level={2} class="ridu-upload-drawer-title">
					{i18n.t(dialog === "edit" ? "uploads:editingFilename" : "uploads:sizesFor", {
						filename: draft.filename,
					})}
				</Dialog.Title>
				{#if dialog === "sizes"}
					<Dialog.Close type="button" class={buttonVariants({ variant: "outline", size: "sm" })}>
						{i18n.t("general:close")}
					</Dialog.Close>
				{/if}
			</header>
			<Dialog.Description class="ridu-upload-description">
				{i18n.t(
					dialog === "edit" ? "uploads:imageEditDescription" : "uploads:previewGeneratedSizes"
				)}
			</Dialog.Description>

			{#if dialog === "edit"}
				{#if draft.source}
					{#await import("@admin/features/uploads/image-editor.svelte")}
						<p class="ridu-upload-drawer-message">{i18n.t("uploads:loadingAssetPreview")}</p>
					{:then { default: Editor }}
						<Editor
							source={draft.source}
							initial={draft.currentImage}
							oncancel={closeImageEditor}
							onapply={(edit) => {
								if (!editable) {
									closeImageEditor();
									return;
								}
								draft.applyImage(edit);
								closeImageEditor();
							}}
						/>
					{:catch}
						<div class="ridu-upload-drawer-message">
							<p role="alert">{i18n.t("uploads:previewLoadFailed")}</p>
							<Dialog.Close class={buttonVariants({ variant: "outline" })}>
								{i18n.t("general:cancel")}
							</Dialog.Close>
						</div>
					{/await}
				{:else}
					<div class="ridu-upload-drawer-message">
						<p role={draft.error ? "alert" : "status"}>
							{draft.error ?? i18n.t("uploads:loadingAssetPreview")}
						</p>
						<Button variant="outline" onclick={closeImageEditor}>
							{i18n.t("general:cancel")}
						</Button>
					</div>
				{/if}
			{:else if dialog === "sizes"}
				{#if draft.document}
					<RenditionPreview {renditions} document={draft.document} />
				{/if}
			{/if}
		</Dialog.Content>
	</Dialog.Portal>
</Dialog.Root>
