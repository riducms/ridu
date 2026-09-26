<script lang="ts">
	import {
		useLexicalComposerContext,
		useLexicalEditable,
		useLexicalNodeSelection,
	} from "@hvniel/lexical-svelte";
	import { getAdminI18n } from "@riducms/plugin";
	import type { ElementFormatType, NodeKey } from "lexical";
	import { Button, Textarea } from "@riducms/ui";
	import ReplaceIcon from "~icons/lucide/refresh-cw";
	import RemoveIcon from "~icons/lucide/x";

	import { getRichTextAuthoringHost } from "@plugin-richtext/field/rich-text-context.svelte";
	import {
		OPEN_UPLOAD_BROWSER_COMMAND,
		REMOVE_UPLOAD_COMMAND,
		UPDATE_UPLOAD_CAPTION_COMMAND,
	} from "@plugin-richtext/menu/rich-text-commands";
	import "@plugin-richtext/upload/rich-text-upload-node.scss";

	let {
		caption,
		documentID,
		format,
		nodeKey,
		relationTo,
	}: {
		caption: string;
		documentID: string;
		format: ElementFormatType;
		nodeKey: NodeKey;
		relationTo: string;
	} = $props();
	const editor = useLexicalComposerContext()[0];
	const isEditable = useLexicalEditable();
	const i18n = getAdminI18n();
	// A decorator node key is fixed for this component's lifetime.
	// svelte-ignore state_referenced_locally
	const [isSelected, setSelected, clearSelection] = useLexicalNodeSelection(nodeKey);
	const authoring = getRichTextAuthoringHost();
	let document = $state.raw<Record<string, unknown>>();
	let loadError = $state(false);
	const collection = $derived(
		authoring?.collections.find((candidate) => candidate.slug === relationTo)
	);
	const filename = $derived(String(document?.filename ?? documentID));
	const altText = $derived(typeof document?.alt === "string" ? document.alt.trim() : "");
	const extension = $derived(fileExtension(filename));
	const imageURL = $derived(
		typeof document?.mimeType === "string" && document.mimeType.startsWith("image/")
			? typeof document.thumbnailURL === "string" && document.thumbnailURL !== ""
				? document.thumbnailURL
				: typeof document.url === "string"
					? document.url
					: undefined
			: undefined
	);
	const mediaWidth = $derived(positiveDimension(document?.width));
	const mediaHeight = $derived(positiveDimension(document?.height));
	const mediaAspect = $derived(
		mediaWidth !== undefined && mediaHeight !== undefined ? mediaWidth / mediaHeight : 1
	);
	const mediaWidthLimit = $derived(`${Math.min(450, Math.max(240, 450 * mediaAspect))}px`);

	$effect(() => {
		if (authoring === undefined) return;
		void authoring.documentRevision;
		const request = new AbortController();
		document = undefined;
		loadError = false;
		authoring
			.findDocument(relationTo, documentID, request.signal)
			.then((value) => {
				if (request.signal.aborted) return;
				document = value;
				loadError = false;
			})
			.catch(() => {
				if (request.signal.aborted) return;
				loadError = true;
			});
		return () => request.abort();
	});

	function selectNode(event?: MouseEvent) {
		if (event?.shiftKey !== true) clearSelection();
		setSelected(true);
	}

	function openDocument(event: MouseEvent) {
		selectNode(event);
		editor.dispatchCommand(OPEN_UPLOAD_BROWSER_COMMAND, {
			collectionSlug: relationTo,
			documentID,
			mode: "edit",
		});
	}

	function replaceDocument() {
		editor.dispatchCommand(OPEN_UPLOAD_BROWSER_COMMAND, {
			collectionSlug: relationTo,
			documentID,
			mode: "replace",
			nodeKey,
		});
	}

	function removeUpload() {
		editor.dispatchCommand(REMOVE_UPLOAD_COMMAND, nodeKey);
		queueMicrotask(() => editor.focus());
	}

	function updateCaption(event: Event) {
		if (!(event.currentTarget instanceof HTMLTextAreaElement)) return;
		editor.dispatchCommand(UPDATE_UPLOAD_CAPTION_COMMAND, {
			caption: event.currentTarget.value,
			nodeKey,
		});
	}

	function handleCaptionKeydown(event: KeyboardEvent) {
		event.stopPropagation();
		if (event.key !== "Escape" || !(event.currentTarget instanceof HTMLTextAreaElement)) return;
		event.preventDefault();
		event.currentTarget.blur();
		queueMicrotask(() => editor.focus());
	}

	function fileExtension(value: string) {
		const match = /\.([a-z0-9]{1,8})$/i.exec(value);
		return match?.[1]?.toLocaleUpperCase(i18n.language) ?? i18n.t("plugin.richtext:asset.file");
	}

	function positiveDimension(value: unknown) {
		return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : undefined;
	}
</script>

<figure
	class={[
		"ridu-richtext-upload-card",
		isSelected() && "is-selected",
		!isEditable() && "is-read-only",
	]}
	data-selected={isSelected() || undefined}
	data-align={format || undefined}
	aria-busy={authoring !== undefined && document === undefined && !loadError}
	style:--ridu-richtext-upload-aspect={mediaAspect}
	style:--ridu-richtext-upload-width={mediaWidthLimit}
	role="group"
	aria-label={i18n.t("plugin.richtext:asset.label", {
		collection: collection?.labels.singular ?? relationTo,
		filename,
	})}
>
	<div class="ridu-richtext-upload-card__media">
		<span
			class={[
				"ridu-richtext-upload-preview ridu-richtext-upload-card__preview",
				document === undefined && !loadError && "is-loading",
			]}
		>
			{#if imageURL !== undefined}
				<img class="ridu-richtext-upload-card__image" src={imageURL} alt={altText} />
			{:else}
				<span class="ridu-richtext-upload-card__file-preview">
					<strong class="ridu-richtext-upload-card__extension">
						{loadError ? i18n.t("plugin.richtext:asset.unavailable") : extension}
					</strong>
					<span class="ridu-richtext-upload-card__preview-name">
						{loadError ? i18n.t("plugin.richtext:asset.couldNotLoad") : filename}
					</span>
				</span>
			{/if}
		</span>

		{#if isEditable()}
			<div
				class={["ridu-richtext-upload-card__actions", isSelected() && "is-visible"]}
				role="group"
				aria-label={i18n.t("plugin.richtext:asset.actions")}
			>
				<Button
					class="ridu-richtext-upload-card__action"
					variant="ghost"
					size="icon-sm"
					onclick={replaceDocument}
					aria-label={i18n.t("plugin.richtext:editor.replaceNamed", { name: filename })}
					tooltip={i18n.t("plugin.richtext:asset.replace")}
				>
					<ReplaceIcon />
				</Button>
				<Button
					class="ridu-richtext-upload-card__action"
					variant="ghost"
					size="icon-sm"
					onclick={removeUpload}
					aria-label={i18n.t("plugin.richtext:editor.removeNamed", { name: filename })}
					tooltip={i18n.t("plugin.richtext:asset.remove")}
				>
					<RemoveIcon />
				</Button>
			</div>
		{/if}
		<div class="ridu-richtext-upload-card__metadata" aria-live="polite">
			{#if isEditable()}
				<button
					type="button"
					class="ridu-richtext-upload-card__filename"
					onclick={openDocument}
					aria-label={i18n.t("plugin.richtext:editor.editNamed", { name: filename })}
				>
					{filename}
				</button>
			{:else}
				<strong class="ridu-richtext-upload-card__filename">{filename}</strong>
			{/if}
			<span class="ridu-richtext-upload-card__collection">
				{collection?.labels.singular ?? relationTo}
			</span>
		</div>
	</div>

	{#if isEditable() || caption !== ""}
		<figcaption class="ridu-richtext-upload-card__caption">
			{#if isEditable()}
				<div class="ridu-richtext-upload-card__caption-editor">
					<svg
						class="ridu-richtext-upload-card__caption-icon"
						viewBox="0 0 24 24"
						fill="none"
						stroke="currentColor"
						stroke-width="2"
						aria-hidden="true"
					>
						<path d="M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L8 18l-4 1 1-4Z" />
					</svg>
					<Textarea
						class="ridu-richtext-upload-card__caption-input"
						rows={1}
						value={caption}
						oninput={updateCaption}
						onkeydown={handleCaptionKeydown}
						aria-label={i18n.t("plugin.richtext:asset.captionFor", { filename })}
						placeholder={i18n.t("plugin.richtext:asset.addCaption")}
					/>
				</div>
			{:else}
				<p class="ridu-richtext-upload-card__caption-text">{caption}</p>
			{/if}
		</figcaption>
	{/if}
</figure>
