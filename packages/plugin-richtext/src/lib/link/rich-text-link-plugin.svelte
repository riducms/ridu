<script lang="ts">
	import { Portal, useLexicalComposerContext, useLexicalEditable } from "@hvniel/lexical-svelte";
	import { $createLinkNode, $isLinkNode, $toggleLink, type LinkNode } from "@lexical/link";
	import { mergeRegister } from "@lexical/utils";
	import type { Attachment } from "svelte/attachments";
	import { getAdminI18n } from "@riducms/plugin";
	import {
		Button,
		Checkbox,
		Dialog,
		DialogClose,
		DialogContent,
		DialogTitle,
		FieldFrame,
		Input,
		fieldControlARIA,
	} from "@riducms/ui";
	import EditIcon from "~icons/lucide/pencil";
	import ExternalLinkIcon from "~icons/lucide/external-link";
	import TrashIcon from "~icons/lucide/trash-2";
	import XIcon from "~icons/lucide/x";
	import {
		$addUpdateTag,
		$createTextNode,
		$getNodeByKey,
		$getSelection,
		$isRangeSelection,
		$setSelection,
		COMMAND_PRIORITY_LOW,
		HISTORY_PUSH_TAG,
		KEY_DOWN_COMMAND,
		KEY_TAB_COMMAND,
		SELECTION_CHANGE_COMMAND,
		SKIP_DOM_SELECTION_TAG,
		type LexicalNode,
		type NodeKey,
		type RangeSelection,
	} from "lexical";

	import { normalizeLinkURL } from "#lib/link/link-url.js";
	import { OPEN_LINK_EDITOR_COMMAND } from "#lib/menu/rich-text-commands.js";
	import "#lib/link/rich-text-link.scss";

	interface LinkEditorSession {
		linkKey: NodeKey | undefined;
		selection: RangeSelection;
		text: string;
	}

	interface LinkPreview {
		key: NodeKey;
		url: string;
		safeURL: string | undefined;
		newTab: boolean;
	}

	const editor = useLexicalComposerContext()[0];
	const isEditable = useLexicalEditable();
	const i18n = getAdminI18n();
	const instanceID = $props.id();
	const textID = `${instanceID}-text`;
	const urlID = `${instanceID}-url`;
	const newTabID = `${instanceID}-new-tab`;

	let session: LinkEditorSession | undefined;
	let drawerOpen = $state(false);
	let textDraft = $state("");
	let urlDraft = $state("");
	let newTabDraft = $state(false);
	let textError = $state("");
	let urlError = $state("");
	let textInput: HTMLInputElement | null = null;
	let preview = $state.raw<LinkPreview>();
	let previewElement: HTMLElement | null = null;
	let previewLeft = $state(12);
	let previewTop = $state(12);
	let focusRevision = 0;
	let focusFrame: number | undefined;
	const textARIA = $derived(fieldControlARIA(textID, false, textError !== ""));
	const urlARIA = $derived(fieldControlARIA(urlID, false, urlError !== ""));

	// @lexical-scope
	function $nearestLink(node: LexicalNode): LinkNode | undefined {
		if ($isLinkNode(node)) return node;
		const parent = node.getParent();
		return $isLinkNode(parent) ? parent : undefined;
	}

	// @lexical-scope
	function $linkAtSelection(): LinkNode | undefined {
		const selection = $getSelection();
		if (!$isRangeSelection(selection)) return undefined;
		const anchorLink = $nearestLink(selection.anchor.getNode());
		if (selection.isCollapsed()) return anchorLink;
		const focusLink = $nearestLink(selection.focus.getNode());
		return anchorLink !== undefined && focusLink?.getKey() === anchorLink.getKey()
			? anchorLink
			: undefined;
	}

	// @lexical-scope
	function $beginLinkSession(linkKey?: NodeKey): boolean {
		if (!editor.isEditable()) return false;
		const selection = $getSelection();
		if (!$isRangeSelection(selection)) return false;
		const selectedLink =
			linkKey === undefined ? $linkAtSelection() : $getNodeByKey<LinkNode>(linkKey);
		const link = $isLinkNode(selectedLink) ? selectedLink : undefined;
		const text = link?.getTextContent() ?? selection.getTextContent();
		if (link === undefined && (selection.isCollapsed() || text.trim() === "")) return false;

		session = {
			linkKey: link?.getKey(),
			selection: selection.clone(),
			text,
		};
		textDraft = text;
		urlDraft = link?.getURL() ?? "";
		newTabDraft = link?.getTarget() === "_blank";
		textError = "";
		urlError = "";
		preview = undefined;
		focusRevision += 1;
		drawerOpen = true;
		return true;
	}

	function beginLinkSession(linkKey?: NodeKey): boolean {
		let opened = false;
		editor.getEditorState().read(() => {
			opened = $beginLinkSession(linkKey);
		});
		return opened;
	}

	function closeLinkEditor(restoreFocus = true) {
		if (focusFrame !== undefined) cancelAnimationFrame(focusFrame);
		focusFrame = undefined;
		const revision = ++focusRevision;
		drawerOpen = false;
		session = undefined;
		textError = "";
		urlError = "";
		if (restoreFocus)
			queueMicrotask(() => {
				if (!drawerOpen && revision === focusRevision) editor.focus();
			});
	}

	function handleDrawerOpenChange(open: boolean) {
		if (open) drawerOpen = true;
		else closeLinkEditor();
	}

	function focusTextInput(event: Event) {
		event.preventDefault();
		const revision = focusRevision;
		if (focusFrame !== undefined) cancelAnimationFrame(focusFrame);
		focusFrame = requestAnimationFrame(() => {
			focusFrame = undefined;
			if (!drawerOpen || session === undefined || revision !== focusRevision) return;
			textInput?.focus();
			textInput?.select();
		});
	}

	function setTextInput(node: HTMLInputElement | null) {
		textInput = node;
	}

	function preventAutomaticFocusRestore(event: Event) {
		event.preventDefault();
	}

	function validateDraft(): string | undefined {
		textError = textDraft.trim() === "" ? i18n.t("plugin.richtext:link.textRequired") : "";
		if (urlDraft.trim() === "") {
			urlError = i18n.t("plugin.richtext:link.urlRequired");
			return undefined;
		}
		const url = normalizeLinkURL(urlDraft);
		urlError = url === undefined ? i18n.t("plugin.richtext:link.urlInvalid") : "";
		return textError === "" ? url : undefined;
	}

	function saveLink() {
		const active = session;
		const url = validateDraft();
		const text = textDraft;
		if (active === undefined || url === undefined || text.trim() === "" || !editor.isEditable())
			return;

		editor.update(
			() => {
				$addUpdateTag(HISTORY_PUSH_TAG);
				if (active.linkKey !== undefined) {
					const node = $getNodeByKey(active.linkKey);
					if (!$isLinkNode(node)) return;
					node
						.setURL(url)
						.setTarget(newTabDraft ? "_blank" : null)
						.setRel(newTabDraft ? "noopener noreferrer" : null);
					if (node.getTextContent() !== text) {
						const [firstText, ...remainingText] = node.getAllTextNodes();
						if (firstText === undefined) {
							const replacement = $createTextNode(text);
							node.append(replacement);
							replacement.selectEnd();
						} else {
							firstText.setTextContent(text);
							for (const extraText of remainingText) extraText.remove();
							firstText.selectEnd();
						}
					} else {
						node.selectEnd();
					}
					return;
				}

				if (
					$getNodeByKey(active.selection.anchor.key) === null ||
					$getNodeByKey(active.selection.focus.key) === null
				)
					return;
				$setSelection(active.selection.clone());
				const selection = $getSelection();
				if (!$isRangeSelection(selection)) return;
				if (text === active.text) {
					$toggleLink({
						url,
						target: newTabDraft ? "_blank" : null,
						rel: newTabDraft ? "noopener noreferrer" : null,
					});
					return;
				}
				const textNode = $createTextNode(text)
					.setFormat(selection.format)
					.setStyle(selection.style);
				const link = $createLinkNode(url, {
					target: newTabDraft ? "_blank" : null,
					rel: newTabDraft ? "noopener noreferrer" : null,
				});
				link.append(textNode);
				selection.insertNodes([link]);
				link.selectEnd();
			},
			{ tag: HISTORY_PUSH_TAG, discrete: true }
		);
		closeLinkEditor();
	}

	function removePreviewLink() {
		const selected = preview;
		if (selected === undefined || !editor.isEditable()) return;
		editor.update(
			() => {
				const link = $getNodeByKey(selected.key);
				if (!$isLinkNode(link)) return;
				$addUpdateTag(HISTORY_PUSH_TAG);
				const parent = link.getParentOrThrow();
				const children = link.getChildren();
				parent.splice(link.getIndexWithinParent(), 0, children);
				link.remove();
				children.at(-1)?.selectEnd();
			},
			{ tag: HISTORY_PUSH_TAG, discrete: true }
		);
		preview = undefined;
		queueMicrotask(() => editor.focus());
	}

	function positionPreview() {
		const selected = preview;
		if (selected === undefined) return;
		const link = editor.getElementByKey(selected.key);
		if (link === null || !link.isConnected) {
			preview = undefined;
			return;
		}
		const rect = link.getBoundingClientRect();
		const width = previewElement?.offsetWidth ?? 320;
		const height = previewElement?.offsetHeight ?? 38;
		previewLeft = Math.max(12, Math.min(window.innerWidth - width - 12, rect.left));
		const preferredTop = rect.bottom + 8;
		previewTop = Math.max(12, Math.min(window.innerHeight - height - 12, preferredTop));
	}

	const attachPreviewElement: Attachment<HTMLElement> = (element) => {
		previewElement = element;
		positionPreview();
		return () => {
			if (previewElement === element) previewElement = null;
		};
	};

	function focusPreview(event: KeyboardEvent) {
		if (event.shiftKey || preview === undefined || previewElement === null) return false;
		const firstControl = previewElement.querySelector<HTMLElement>(
			"a[href], button:not([disabled]), [tabindex]:not([tabindex='-1'])"
		);
		if (firstControl === null) return false;
		event.preventDefault();
		firstControl.focus();
		return true;
	}

	// @lexical-scope
	function $updatePreview() {
		if (drawerOpen || !isEditable()) {
			preview = undefined;
			return;
		}
		const selection = $getSelection();
		const link = $linkAtSelection();
		if (!$isRangeSelection(selection) || !selection.isCollapsed() || link === undefined) {
			preview = undefined;
			return;
		}
		const root = editor.getRootElement();
		const activeElement = document.activeElement;
		if (
			root === null ||
			(!root.contains(activeElement) && !previewElement?.contains(activeElement))
		) {
			preview = undefined;
			return;
		}
		const url = link.getURL();
		preview = {
			key: link.getKey(),
			url,
			safeURL: normalizeLinkURL(url),
			newTab: link.getTarget() === "_blank",
		};
		queueMicrotask(positionPreview);
	}

	function updatePreviewFromEditor() {
		editor.getEditorState().read($updatePreview, { editor });
	}

	function preserveEditorSelection(event: MouseEvent) {
		event.preventDefault();
	}

	$effect(() => {
		const unregister = mergeRegister(
			editor.registerCommand(
				OPEN_LINK_EDITOR_COMMAND,
				() => {
					$addUpdateTag(SKIP_DOM_SELECTION_TAG);
					return $beginLinkSession();
				},
				COMMAND_PRIORITY_LOW
			),
			editor.registerCommand(
				KEY_DOWN_COMMAND,
				(event) => {
					if (
						event.key.toLowerCase() !== "k" ||
						(!event.metaKey && !event.ctrlKey) ||
						event.altKey ||
						event.shiftKey
					)
						return false;
					if (!$beginLinkSession()) return false;
					$addUpdateTag(SKIP_DOM_SELECTION_TAG);
					event.preventDefault();
					event.stopPropagation();
					return true;
				},
				COMMAND_PRIORITY_LOW
			),
			editor.registerCommand(KEY_TAB_COMMAND, focusPreview, COMMAND_PRIORITY_LOW),
			editor.registerUpdateListener(({ editorState }) => editorState.read($updatePreview)),
			editor.registerCommand(
				SELECTION_CHANGE_COMMAND,
				() => {
					$updatePreview();
					return false;
				},
				COMMAND_PRIORITY_LOW
			)
		);
		document.addEventListener("focusin", updatePreviewFromEditor);
		document.addEventListener("scroll", positionPreview, true);
		window.addEventListener("resize", positionPreview);
		return () => {
			unregister();
			document.removeEventListener("focusin", updatePreviewFromEditor);
			document.removeEventListener("scroll", positionPreview, true);
			window.removeEventListener("resize", positionPreview);
			if (focusFrame !== undefined) cancelAnimationFrame(focusFrame);
			focusFrame = undefined;
			focusRevision += 1;
			session = undefined;
			preview = undefined;
		};
	});
</script>

{#if preview}
	<Portal>
		<div
			{@attach attachPreviewElement}
			class="ridu-richtext-link-preview"
			role="dialog"
			tabindex="-1"
			aria-label={i18n.t("plugin.richtext:link.preview")}
			dir={i18n.direction}
			style={`left: ${previewLeft}px; top: ${previewTop}px;`}
			onmousedown={preserveEditorSelection}
		>
			{#if preview.safeURL}
				<a
					class="ridu-richtext-link-preview__url"
					href={preview.safeURL}
					target="_blank"
					rel="noopener noreferrer"
					title={preview.url}
				>
					{#if preview.newTab}<ExternalLinkIcon aria-hidden="true" />{/if}
					<span>{preview.url}</span>
				</a>
			{:else}
				<span class="ridu-richtext-link-preview__url" title={preview.url}>
					{#if preview.newTab}<ExternalLinkIcon aria-hidden="true" />{/if}
					<span>{preview.url}</span>
				</span>
			{/if}
			<div class="ridu-richtext-link-preview__actions">
				<Button
					variant="ghost"
					size="icon-sm"
					aria-label={i18n.t("plugin.richtext:editor.editLink")}
					tooltip={i18n.t("plugin.richtext:editor.editLink")}
					onclick={() => beginLinkSession(preview?.key)}
				>
					<EditIcon aria-hidden="true" />
				</Button>
				<Button
					variant="ghost"
					size="icon-sm"
					aria-label={i18n.t("plugin.richtext:editor.remove")}
					tooltip={i18n.t("plugin.richtext:editor.remove")}
					onclick={removePreviewLink}
				>
					<TrashIcon aria-hidden="true" />
				</Button>
			</div>
		</div>
	</Portal>
{/if}

<Dialog bind:open={() => drawerOpen, handleDrawerOpenChange}>
	<DialogContent
		variant="drawer"
		class="ridu-richtext-link-drawer"
		dir={i18n.direction}
		aria-describedby={undefined}
		onOpenAutoFocus={focusTextInput}
		onCloseAutoFocus={preventAutomaticFocusRestore}
	>
		<header class="ridu-richtext-link-drawer__header">
			<DialogTitle class="ridu-richtext-link-drawer__title">
				{i18n.t("plugin.richtext:editor.editLink")}
			</DialogTitle>
			<DialogClose
				type="button"
				class="ridu-richtext-link-drawer__close"
				aria-label={i18n.t("plugin.richtext:editor.cancel")}
			>
				<XIcon aria-hidden="true" />
			</DialogClose>
		</header>

		<form
			class="ridu-richtext-link-form"
			novalidate
			onsubmit={(event) => {
				event.preventDefault();
				saveLink();
			}}
		>
			<FieldFrame
				controlID={textID}
				label={i18n.t("plugin.richtext:link.textLabel")}
				required
				errors={textError === "" ? [] : [textError]}
			>
				<Input
					id={textID}
					bind:ref={() => textInput, setTextInput}
					bind:value={textDraft}
					required
					{...textARIA}
					oninput={() => (textError = "")}
				/>
			</FieldFrame>

			<FieldFrame
				controlID={urlID}
				label={i18n.t("plugin.richtext:editor.linkURL")}
				required
				errors={urlError === "" ? [] : [urlError]}
			>
				<Input
					id={urlID}
					bind:value={urlDraft}
					type="url"
					required
					spellcheck="false"
					{...urlARIA}
					oninput={() => (urlError = "")}
				/>
			</FieldFrame>

			<div class="ridu-richtext-link-checkbox-field">
				<Checkbox id={newTabID} bind:checked={newTabDraft} />
				<label for={newTabID}>{i18n.t("plugin.richtext:link.newTab")}</label>
			</div>

			<Button type="submit" class="ridu-richtext-link-form__submit">
				{i18n.t("plugin.richtext:link.saveChanges")}
			</Button>
		</form>
	</DialogContent>
</Dialog>
