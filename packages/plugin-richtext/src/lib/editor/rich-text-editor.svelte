<script module lang="ts">
	import type { RichTextEditorFeature } from "#lib/field/rich-text-config.js";

	const defaultFeatures: readonly RichTextEditorFeature[] = [
		"links",
		"lists",
		"code",
		"horizontal-rule",
	];

	const theme = {
		heading: {
			h1: "ridu-richtext-heading ridu-richtext-h1",
			h2: "ridu-richtext-heading ridu-richtext-h2",
			h3: "ridu-richtext-heading ridu-richtext-h3",
			h4: "ridu-richtext-heading ridu-richtext-h4",
			h5: "ridu-richtext-heading ridu-richtext-h5",
			h6: "ridu-richtext-heading ridu-richtext-h6",
		},
		code: "ridu-richtext-code-block",
		hr: "ridu-richtext-rule",
		hrSelected: "is-selected",
		link: "ridu-richtext-link",
		list: {
			checklist: "ridu-richtext-checklist",
			listitemChecked: "ridu-richtext-list-item-checked",
			listitemUnchecked: "ridu-richtext-list-item-unchecked",
			listitem: "ridu-richtext-list-item",
			olDepth: [
				"ridu-richtext-list-ordered",
				"ridu-richtext-list-alpha-upper",
				"ridu-richtext-list-alpha-lower",
				"ridu-richtext-list-roman-upper",
				"ridu-richtext-list-roman-lower",
			],
			nested: { listitem: "ridu-richtext-list-item-nested" },
			ol: "ridu-richtext-list-ordered",
			ul: "ridu-richtext-list-unordered",
		},
		paragraph: "ridu-richtext-paragraph",
		quote: "ridu-richtext-quote",
		text: {
			bold: "ridu-richtext-bold",
			code: "ridu-richtext-inline-code",
			italic: "ridu-richtext-italic",
			subscript: "ridu-richtext-subscript",
			superscript: "ridu-richtext-superscript",
			strikethrough: "ridu-richtext-strikethrough",
			underline: "ridu-richtext-underline",
		},
		upload: "ridu-richtext-upload",
	};
</script>

<script lang="ts">
	import { documentRecoveryIssue, type RichTextDocument } from "@riducms/sdk/richtext";
	import { getAdminI18n } from "@riducms/plugin";
	import { CodeNode } from "@lexical/code";
	import { HistoryExtension } from "@lexical/history";
	import { LinkNode } from "@lexical/link";
	import { CheckListExtension, ListExtension } from "@lexical/list";
	import { registerMarkdownShortcuts } from "@lexical/markdown";
	import { RichTextExtension } from "@lexical/rich-text";
	import { $insertNodeToNearestRoot } from "@lexical/utils";
	import {
		ContentEditable,
		DEFAULT_TRANSFORMERS,
		$createHorizontalRuleNode,
		HorizontalRuleNode,
		INSERT_HORIZONTAL_RULE_COMMAND,
		LexicalExtensionComposer,
		LinkPlugin,
		OnChangePlugin,
	} from "@hvniel/lexical-svelte";
	import {
		$createParagraphNode,
		$getRoot,
		$getSelection,
		$isRangeSelection,
		COMMAND_PRIORITY_EDITOR,
		HISTORY_MERGE_TAG,
		defineExtension,
		mergeRegister,
		type EditorState,
	} from "lexical";
	import { Button } from "@riducms/ui";
	import { BROWSER } from "esm-env";
	import type { ClassValue } from "svelte/elements";

	import "#lib/styles/integration.scss";
	import RichTextBlockToolbar from "#lib/menu/rich-text-block-toolbar.svelte";
	import RichTextEditabilityPlugin from "#lib/field/rich-text-editability-plugin.svelte";
	import RichTextFooter from "#lib/field/rich-text-footer.svelte";
	import RichTextToolbars from "#lib/toolbar/rich-text-toolbars.svelte";
	import { followVisualViewport } from "#lib/toolbar/visual-viewport.js";
	import RichTextLinkPlugin from "#lib/link/rich-text-link-plugin.svelte";
	import { registerSafeLinkTransform } from "#lib/link/safe-link-transform.js";
	import { richTextMarkdownTransformers } from "#lib/field/rich-text-markdown.js";
	import RichTextSlashMenu from "#lib/menu/rich-text-slash-menu.svelte";
	import type { RichTextToolbar } from "#lib/field/rich-text-config.js";
	import {
		editorRecoveryIssue,
		initialEditorState,
		isBlocklessDocument,
		isEmptyDocument,
	} from "#lib/field/rich-text-document.js";
	import { BLOCK_FIELD_CHANGE_TAG } from "#lib/block/rich-text-block-history.js";
	import { equalRichTextValues } from "#lib/field/rich-text-value.js";
	import type { RichTextEditorExtension } from "#lib/editor/rich-text-extension.js";
	import RichText from "#lib/render/rich-text.svelte";

	const generatedID = $props.id();
	let {
		value = $bindable(),
		features = defaultFeatures,
		toolbar = "floating",
		hideGutter = false,
		hideDraggableBlockElement = false,
		hideAddBlockButton = false,
		hideInsertParagraphAtEnd = false,
		lang,
		dir,
		onchange,
		onrejectedchange,
		extensions = [],
		id = generatedID,
		label,
		name = "rich-text",
		path = name,
		required = false,
		readonly = false,
		disabled = false,
		invalid = false,
		describedby,
		errormessage,
		placeholder,
		class: className,
	}: {
		/**
		 * The stored document, read when the editor mounts. The editor writes each accepted edit
		 * back, but a value set from outside doesn't replace its content: remount it for that.
		 */
		value?: unknown;
		/** Pass the Go field's features, so the editor offers only what the field accepts. */
		features?: readonly RichTextEditorFeature[];
		toolbar?: RichTextToolbar;
		// The Go field's `richtext.Admin` options.
		hideGutter?: boolean;
		hideDraggableBlockElement?: boolean;
		hideAddBlockButton?: boolean;
		hideInsertParagraphAtEnd?: boolean;
		/** The language of the editor's content, such as "fr". */
		lang?: string | undefined;
		/** The direction of the editor's content. */
		dir?: "ltr" | "rtl" | undefined;
		/** A changed document, already checked to be one the rich-text field accepts. */
		onchange?: (value: RichTextDocument<unknown>) => void;
		/** The path of a node the field would reject, or undefined once the document is valid again. */
		onrejectedchange?: (issue: string | undefined) => void;
		/** Features the core editor doesn't own, such as the admin's uploads and blocks. */
		extensions?: readonly RichTextEditorExtension[];
		id?: string;
		label?: string | undefined;
		/** Names the downloaded recovery file. */
		name?: string;
		/** Locates problems in recovery messages. */
		path?: string;
		required?: boolean;
		/** Shows the document without editing controls. */
		readonly?: boolean;
		/** Blocks editing for now, for example while the document saves. */
		disabled?: boolean;
		invalid?: boolean;
		describedby?: string | undefined;
		errormessage?: string | undefined;
		placeholder?: string | undefined;
		class?: ClassValue | undefined;
	} = $props();

	// Lexical is set up once, with the document, features and extensions the editor mounts with;
	// its host remounts it to replace them.
	const initialValue = value;
	// svelte-ignore state_referenced_locally
	const recoveryIssue = editorRecoveryIssue(initialValue, features, extensions);
	// The admin provides its translations, and the app editor its own catalogue.
	const i18n = getAdminI18n();

	const editingBlocked = $derived(readonly || disabled);
	const fixedToolbar = $derived(toolbar !== "floating" && !readonly);
	const visiblePlaceholder = $derived(
		placeholder ??
			(readonly
				? i18n.t("plugin.richtext:editor.empty")
				: i18n.t("plugin.richtext:editor.placeholder"))
	);

	// The block toolbar and the fixed toolbar render into these once they exist.
	let canvasElement = $state.raw<HTMLElement | null>(null);
	let fixedToolbarSlot = $state.raw<HTMLElement | null>(null);

	// The extension tree is identity-bearing and belongs to this editor instance.
	// svelte-ignore state_referenced_locally
	const extension = defineExtension({
		$initialEditorState:
			(recoveryIssue === undefined ? initialEditorState(initialValue) : null) ??
			(() => {
				// Initialize Lexical only: its first real edit must not look like initial hydration.
				$getRoot().append($createParagraphNode());
			}),
		dependencies: [
			RichTextExtension,
			HistoryExtension,
			...(features.includes("lists") ? [ListExtension, CheckListExtension] : []),
		],
		register: (editor) =>
			mergeRegister(
				registerMarkdownShortcuts(editor, [
					...richTextMarkdownTransformers(features),
					...(features.includes("horizontal-rule")
						? DEFAULT_TRANSFORMERS.filter(
								(transformer) =>
									transformer.type === "element" &&
									transformer.dependencies.includes(HorizontalRuleNode)
							)
						: []),
				]),
				// The upstream horizontal-rule plugin treats a cross-block drag's root click
				// as a gap click and collapses the range; Lexical handles gap clicks itself.
				...(features.includes("horizontal-rule")
					? [
							editor.registerCommand(
								INSERT_HORIZONTAL_RULE_COMMAND,
								() => {
									const selection = $getSelection();
									if (!$isRangeSelection(selection)) return false;
									$insertNodeToNearestRoot($createHorizontalRuleNode());
									return true;
								},
								COMMAND_PRIORITY_EDITOR
							),
						]
					: []),
				...(features.includes("links") ? [registerSafeLinkTransform(editor)] : [])
			),
		name: "@riducms/plugin-richtext/editor",
		namespace: `ridu-${id}`,
		nodes: [
			...extensions.flatMap((extension) => extension.nodes ?? []),
			...(features.includes("links") ? [LinkNode] : []),
			...(features.includes("code") ? [CodeNode] : []),
			...(features.includes("horizontal-rule") ? [HorizontalRuleNode] : []),
		],
		editable: !editingBlocked,
		theme,
		onError: (error) => {
			throw error;
		},
	});

	function changed(editorState: EditorState, _editor: unknown, tags: Set<string>) {
		// Lexical can finish a queued update while a save locks the editor.
		if (editingBlocked) return;
		// Hydration merges belong to Lexical. Focused inline edits also merge their undo
		// history, but carry an explicit tag because every keystroke must reach the host.
		if (tags.has(HISTORY_MERGE_TAG) && !tags.has(BLOCK_FIELD_CHANGE_TAG)) return;

		// Lexical can retain undefined optional properties after importing sparse server JSON.
		// Validate its serialized wire value, where those properties are absent.
		const next: unknown = JSON.parse(
			JSON.stringify({ version: 1, root: editorState.toJSON().root })
		);
		const issue = documentRecoveryIssue(next);
		// An edit the server would reject stays in the editor and is reported, not emitted.
		onrejectedchange?.(issue);
		if (issue !== undefined) return;
		// Reconciliation and history updates can dirty nodes without changing the document.
		if (equalRichTextValues(next, value)) return;
		value = next;
		onchange?.(next as RichTextDocument<unknown>);
	}

	function exportDocument() {
		const url = URL.createObjectURL(
			new Blob([JSON.stringify(initialValue, null, 2)], { type: "application/json" })
		);
		const link = document.createElement("a");
		link.href = url;
		link.download = `${name}-recovery.json`;
		link.click();
		setTimeout(() => URL.revokeObjectURL(url), 0);
	}
</script>

<div
	class={[
		"ridu-richtext-editor",
		invalid && "has-error",
		readonly && "is-read-only",
		hideGutter && "is-gutterless",
		className,
	]}
	{lang}
	{dir}
	aria-invalid={invalid}
>
	{#if recoveryIssue !== undefined}
		<p role="alert" class="ridu-richtext-recovery">
			{i18n.t("plugin.richtext:editor.recovery", { path: `${path}.${recoveryIssue}` })}
		</p>
		<Button variant="outline" onclick={exportDocument}>
			{i18n.t("plugin.richtext:editor.exportDocument")}
		</Button>
	{:else if !BROWSER}
		<!-- The fixed toolbar's slot and the footer hold their space until the editor mounts. -->
		{#if fixedToolbar}
			<div class="ridu-richtext-fixed-toolbar-slot"></div>
		{/if}
		<div class="ridu-richtext-canvas">
			<div class="ridu-richtext-content" data-ridu-richtext-static>
				{#if isBlocklessDocument(initialValue) && !isEmptyDocument(initialValue)}
					<RichText value={initialValue} />
				{:else}
					<!-- Lexical opens an empty document as one empty paragraph and its placeholder. -->
					<p></p>
					<span class="ridu-richtext-placeholder">{visiblePlaceholder}</span>
				{/if}
			</div>
		</div>
		{#if !hideInsertParagraphAtEnd && !readonly}
			<div class="ridu-richtext-footer"></div>
		{/if}
	{:else}
		<LexicalExtensionComposer {extension} contentEditable={null}>
			{#if fixedToolbar}
				<div
					class="ridu-richtext-fixed-toolbar-slot"
					bind:this={fixedToolbarSlot}
					inert={editingBlocked}
					{@attach followVisualViewport}
				></div>
			{/if}
			<div class="ridu-richtext-canvas" bind:this={canvasElement}>
				<ContentEditable
					{id}
					class="ridu-richtext-content"
					ariaLabel={label}
					ariaDescribedBy={describedby}
					ariaErrorMessage={errormessage}
					ariaInvalid={invalid}
					ariaRequired={required}
					aria-keyshortcuts={editingBlocked
						? undefined
						: features.includes("links")
							? "Alt+Shift+ArrowUp Alt+Shift+ArrowDown Control+K Meta+K"
							: "Alt+Shift+ArrowUp Alt+Shift+ArrowDown"}
					aria-placeholder={visiblePlaceholder}
				>
					{#snippet placeholder()}
						<span class="ridu-richtext-placeholder">
							{visiblePlaceholder}
						</span>
					{/snippet}
				</ContentEditable>
			</div>
			{#if features.includes("links")}
				<LinkPlugin />
			{/if}
			{#if !editingBlocked}
				<RichTextSlashMenu {features} {extensions} />
				{#if features.includes("links")}
					<RichTextLinkPlugin />
				{/if}
				{#if canvasElement !== null}
					<RichTextBlockToolbar
						anchorElement={canvasElement}
						{features}
						{extensions}
						{hideDraggableBlockElement}
						{hideAddBlockButton}
					/>
				{/if}
			{/if}
			<!-- Keep the fixed toolbar's height while a save temporarily locks the editor. -->
			{#if !readonly}
				<RichTextToolbars {features} {fixedToolbarSlot} floating={toolbar !== "fixed"} />
			{/if}
			<RichTextEditabilityPlugin readOnly={editingBlocked} />
			{#each extensions as { plugin }}
				{@render plugin?.({ locked: editingBlocked })}
			{/each}
			<OnChangePlugin
				onChange={changed}
				ignoreSelectionChange
				ignoreHistoryMergeTagChange={false}
			/>
			{#if !hideInsertParagraphAtEnd}
				<RichTextFooter readOnly={readonly} />
			{/if}
		</LexicalExtensionComposer>
	{/if}
</div>
