<script lang="ts">
	import {
		useLexicalComposerContext,
		useLexicalEditable,
		useLexicalNodeSelection,
	} from "@hvniel/lexical-svelte";
	import { getAdminI18n } from "@riducms/plugin";
	import type { NodeKey } from "lexical";
	import { Textarea, TooltipContent, TooltipRoot, TooltipTrigger } from "@riducms/ui";

	import { getRichTextAuthoringHost } from "@plugin-richtext/field/rich-text-context.svelte";
	import {
		OPEN_UPLOAD_BROWSER_COMMAND,
		REMOVE_UPLOAD_COMMAND,
		UPDATE_UPLOAD_CAPTION_COMMAND,
	} from "@plugin-richtext/menu/rich-text-commands";

	let {
		caption,
		documentID,
		nodeKey,
		relationTo,
	}: { caption: string; documentID: string; nodeKey: NodeKey; relationTo: string } = $props();
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
		typeof document?.url === "string" &&
			typeof document.mimeType === "string" &&
			document.mimeType.startsWith("image/")
			? document.url
			: undefined
	);

	$effect(() => {
		if (authoring === undefined) return;
		void authoring.documentRevision;
		const request = new AbortController();
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
</script>

<figure
	class={[
		"group/upload relative my-[1.85rem] w-full overflow-hidden rounded-[4px] border border-control-border bg-control leading-[0] transition-colors duration-150 hover:border-primary/50 focus-within:border-primary/50",
		isSelected() && "border-primary/50",
	]}
	role="group"
	aria-label={i18n.t("plugin.richtext:asset.label", {
		collection: collection?.labels.singular ?? relationTo,
		filename,
	})}
>
	<div class="relative w-full">
		{#if isEditable()}
			<button
				type="button"
				class="block w-full cursor-pointer border-0 bg-transparent p-0 outline-none focus-visible:ring-2 focus-visible:ring-primary/65 focus-visible:ring-inset"
				aria-label={i18n.t("plugin.richtext:editor.editNamed", { name: filename })}
				onclick={openDocument}
			>
				<span
					class={[
						"ridu-richtext-upload-preview relative grid aspect-[16/7] w-full place-items-center overflow-hidden bg-background",
						document === undefined && !loadError && "is-loading",
					]}
				>
					{#if imageURL !== undefined}
						<img
							class="absolute inset-0 block size-full object-cover"
							src={imageURL}
							alt={altText}
						/>
					{:else}
						<span
							class="grid max-w-[80%] place-items-center gap-[0.45rem] font-mono text-[11px] leading-normal tracking-[0.12em] text-foreground-muted uppercase"
							aria-hidden="true"
						>
							<strong class="text-[13px] font-medium">
								{loadError ? i18n.t("plugin.richtext:asset.unavailable") : extension}
							</strong>
							<span class="max-w-full truncate text-foreground-faint">
								{loadError ? i18n.t("plugin.richtext:asset.couldNotLoad") : filename}
							</span>
						</span>
					{/if}
				</span>
			</button>
		{:else}
			<span
				class={[
					"ridu-richtext-upload-preview relative grid aspect-[16/7] w-full place-items-center overflow-hidden bg-background",
					document === undefined && !loadError && "is-loading",
				]}
			>
				{#if imageURL !== undefined}
					<img class="absolute inset-0 block size-full object-cover" src={imageURL} alt={altText} />
				{:else}
					<span
						class="font-mono text-[11px] leading-normal tracking-[0.12em] text-foreground-muted uppercase"
					>
						{loadError ? i18n.t("plugin.richtext:asset.unavailable") : extension}
					</span>
				{/if}
			</span>
		{/if}

		{#if isEditable()}
			<div
				class={[
					"pointer-events-none absolute top-3 left-1/2 z-2 flex max-w-[calc(100%-1.5rem)] -translate-x-1/2 items-center gap-1 rounded-[4px] border border-media-foreground/10 bg-media-overlay p-1 text-media-foreground opacity-0 shadow-[var(--shadow-popover)] backdrop-blur-md transition-opacity duration-150 group-hover/upload:pointer-events-auto group-hover/upload:opacity-100 group-focus-within/upload:pointer-events-auto group-focus-within/upload:opacity-100",
					isSelected() && "pointer-events-auto opacity-100",
				]}
				aria-label={i18n.t("plugin.richtext:asset.actions")}
			>
				<button
					type="button"
					class="min-w-0 cursor-pointer truncate rounded-[3px] border-0 bg-transparent px-2 py-1.5 text-start text-[11.5px] leading-[1.2] text-media-foreground/88 hover:bg-media-foreground/10 focus-visible:bg-media-foreground/10 focus-visible:outline-none"
					onclick={openDocument}
					aria-label={i18n.t("plugin.richtext:editor.editNamed", { name: filename })}
				>
					{filename}
				</button>
				<span class="mx-0.5 h-4 w-px shrink-0 bg-media-foreground/16" aria-hidden="true"></span>
				<TooltipRoot>
					<TooltipTrigger>
						{#snippet child({ props })}
							<button
								{...props}
								type="button"
								class="grid size-7 shrink-0 cursor-pointer place-items-center rounded-[3px] border-0 bg-transparent text-media-foreground/75 hover:bg-media-foreground/10 hover:text-media-foreground focus-visible:bg-media-foreground/10 focus-visible:text-media-foreground focus-visible:outline-none"
								onclick={replaceDocument}
								aria-label={i18n.t("plugin.richtext:editor.replaceNamed", { name: filename })}
							>
								<svg
									class="size-3.5"
									viewBox="0 0 24 24"
									fill="none"
									stroke="currentColor"
									stroke-width="2"
									aria-hidden="true"
								>
									<path
										d="M20 11a8.1 8.1 0 0 0-15.5-2M4 4v5h5M4 13a8.1 8.1 0 0 0 15.5 2M20 20v-5h-5"
									/>
								</svg>
							</button>
						{/snippet}
					</TooltipTrigger>
					<TooltipContent>{i18n.t("plugin.richtext:asset.replace")}</TooltipContent>
				</TooltipRoot>
				<TooltipRoot>
					<TooltipTrigger>
						{#snippet child({ props })}
							<button
								{...props}
								type="button"
								class="grid size-7 shrink-0 cursor-pointer place-items-center rounded-[3px] border-0 bg-transparent text-media-foreground/75 hover:bg-destructive/25 hover:text-media-foreground focus-visible:bg-destructive/25 focus-visible:text-media-foreground focus-visible:outline-none"
								onclick={removeUpload}
								aria-label={i18n.t("plugin.richtext:editor.removeNamed", { name: filename })}
							>
								<svg
									class="size-3.5"
									viewBox="0 0 24 24"
									fill="none"
									stroke="currentColor"
									stroke-width="2"
									aria-hidden="true"
								>
									<path d="M18 6 6 18M6 6l12 12" />
								</svg>
							</button>
						{/snippet}
					</TooltipTrigger>
					<TooltipContent>{i18n.t("plugin.richtext:asset.remove")}</TooltipContent>
				</TooltipRoot>
			</div>
		{/if}
	</div>

	{#if isEditable() || caption !== ""}
		<figcaption class="border-t border-border bg-background/45 px-[0.85rem] py-[0.55rem]">
			{#if isEditable()}
				<div class="flex items-start gap-2">
					<svg
						class="mt-[0.3rem] size-3 shrink-0 text-foreground-faint"
						viewBox="0 0 24 24"
						fill="none"
						stroke="currentColor"
						stroke-width="2"
						aria-hidden="true"
					>
						<path d="M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L8 18l-4 1 1-4Z" />
					</svg>
					<Textarea
						class="min-h-[1.45rem] w-full resize-none overflow-hidden border-0 bg-transparent p-0 text-start text-[13px] leading-[1.45] text-foreground-muted outline-none field-sizing-content placeholder:text-foreground-faint focus:text-foreground"
						rows={1}
						value={caption}
						oninput={updateCaption}
						onkeydown={handleCaptionKeydown}
						aria-label={i18n.t("plugin.richtext:asset.captionFor", { filename })}
						placeholder={i18n.t("plugin.richtext:asset.addCaption")}
					/>
				</div>
			{:else}
				<p class="text-start text-[13px] leading-[1.45] text-foreground-muted">{caption}</p>
			{/if}
		</figcaption>
	{/if}
</figure>
