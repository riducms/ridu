<script lang="ts">
	import type { RichTextDocument } from "@plugin-richtext/document";
	import { decodeRichTextDocument } from "@plugin-richtext/document-validation";
	import type { RichTextConfig } from "@plugin-richtext/field/rich-text-config";
	import type { PluginFieldProps } from "@riducms/plugin";
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
		defineExtension,
		mergeRegister,
		type EditorState,
	} from "lexical";
	import { Button, FieldFrame, fieldControlARIA } from "@riducms/ui";

	import "@plugin-richtext/styles/integration.scss";
	import RichTextBlockToolbar from "@plugin-richtext/menu/rich-text-block-toolbar.svelte";
	import RichTextEditabilityPlugin from "@plugin-richtext/field/rich-text-editability-plugin.svelte";
	import RichTextFooter from "@plugin-richtext/field/rich-text-footer.svelte";
	import RichTextToolbars from "@plugin-richtext/toolbar/rich-text-toolbars.svelte";
	import RichTextLinkPlugin from "@plugin-richtext/link/rich-text-link-plugin.svelte";
	import { registerSafeLinkTransform } from "@plugin-richtext/link/safe-link-transform";
	import { richTextMarkdownTransformers } from "@plugin-richtext/field/rich-text-markdown";
	import RichTextSlashMenu from "@plugin-richtext/menu/rich-text-slash-menu.svelte";
	import { hasRichTextFeature } from "@plugin-richtext/field/rich-text-config";
	import {
		setRichTextAuthoringHost,
		setRichTextField,
	} from "@plugin-richtext/field/rich-text-context.svelte";
	import {
		editorRecoveryIssue,
		initialEditorState,
	} from "@plugin-richtext/field/rich-text-document";
	import { UploadNode } from "@plugin-richtext/upload/rich-text-upload-node";
	import RichTextUploadPlugin from "@plugin-richtext/upload/rich-text-upload-plugin.svelte";
	import { RelationshipNode } from "@plugin-richtext/relationship/rich-text-relationship-node";
	import { BlockNode } from "@plugin-richtext/block/rich-text-block-node";
	import RichTextBlocksPlugin from "@plugin-richtext/block/rich-text-blocks-plugin.svelte";
	import RichTextRelationshipPlugin from "@plugin-richtext/relationship/rich-text-relationship-plugin.svelte";

	let {
		field: binding,
		form,
		config,
		authoring,
		i18n,
	}: PluginFieldProps<RichTextDocument<unknown>, RichTextConfig> = $props();
	const { schema: field, readOnly: editingBlocked, issues } = $derived(binding);
	const inputARIA = $derived(
		fieldControlARIA(field.id, field.admin.description !== undefined, issues.length > 0)
	);
	const visiblePlaceholder = $derived(
		editingBlocked
			? i18n.t("plugin.richtext:editor.empty")
			: i18n.t("plugin.richtext:editor.placeholder")
	);
	let canvasElement = $state<HTMLElement | null>(null);
	let fixedToolbarSlot = $state<HTMLElement | null>(null);
	// A mounted field hydrates once; the parent remounts it for schema/revision/locale changes.
	// svelte-ignore state_referenced_locally
	const initialValue = binding.rawValue;
	// Config belongs to this keyed editor mount.
	// svelte-ignore state_referenced_locally
	const recoveryIssue = editorRecoveryIssue(initialValue, config);
	// The host is stable for this mounted field and decorator descendants inherit it through portals.
	// svelte-ignore state_referenced_locally
	setRichTextAuthoringHost(authoring);
	setRichTextField({
		get field() {
			return field;
		},
		get form() {
			return form;
		},
	});

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

	// The extension tree is identity-bearing and belongs to this field instance.
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
			...(hasRichTextFeature(config, "lists") ? [ListExtension, CheckListExtension] : []),
		],
		register: (editor) =>
			mergeRegister(
				registerMarkdownShortcuts(editor, [
					...richTextMarkdownTransformers(config),
					...(hasRichTextFeature(config, "horizontal-rule")
						? DEFAULT_TRANSFORMERS.filter(
								(transformer) =>
									transformer.type === "element" &&
									transformer.dependencies.includes(HorizontalRuleNode)
							)
						: []),
				]),
				// The upstream horizontal-rule plugin treats a cross-block drag's root click
				// as a gap click and collapses the range; Lexical handles gap clicks itself.
				...(hasRichTextFeature(config, "horizontal-rule")
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
				...(hasRichTextFeature(config, "links") ? [registerSafeLinkTransform(editor)] : [])
			),
		name: "@riducms/plugin-richtext/editor",
		namespace: `ridu-${field.id}`,
		nodes: [
			BlockNode,
			...(hasRichTextFeature(config, "links") ? [LinkNode] : []),
			...(hasRichTextFeature(config, "code") ? [CodeNode] : []),
			...(hasRichTextFeature(config, "horizontal-rule") ? [HorizontalRuleNode] : []),
			...(hasRichTextFeature(config, "uploads") ? [UploadNode] : []),
			...(hasRichTextFeature(config, "relationships") ? [RelationshipNode] : []),
		],
		editable: !editingBlocked,
		theme,
		onError: (error) => {
			throw error;
		},
	});

	function changed(editorState: EditorState) {
		// Lexical can retain undefined optional properties after importing sparse server JSON.
		// Decode its serialized wire value, where those properties are absent.
		binding.set(
			decodeRichTextDocument(
				JSON.parse(JSON.stringify({ version: 1, root: editorState.toJSON().root }))
			)
		);
	}

	function exportDocument() {
		const url = URL.createObjectURL(
			new Blob([JSON.stringify(initialValue, null, 2)], { type: "application/json" })
		);
		const link = document.createElement("a");
		link.href = url;
		link.download = `${field.name}-recovery.json`;
		link.click();
		setTimeout(() => URL.revokeObjectURL(url), 0);
	}
</script>

<div data-field-path={field.path}>
	<FieldFrame
		controlID={field.id}
		label={field.admin.label}
		required={field.required}
		readOnly={field.admin.readOnly}
		description={field.admin.description}
		errors={issues.map((issue) => issue.message)}
		class="ridu-richtext-field"
	>
		<div
			class={[
				"ridu-richtext-editor",
				issues.length > 0 && "has-error",
				editingBlocked && "is-read-only",
				config.admin.hideGutter && "is-gutterless",
			]}
			aria-invalid={inputARIA["aria-invalid"]}
		>
			{#if recoveryIssue !== undefined}
				<p role="alert" class="ridu-richtext-recovery">
					{i18n.t("plugin.richtext:editor.recovery", { path: `${field.path}.${recoveryIssue}` })}
				</p>
				<Button variant="outline" onclick={exportDocument}>
					{i18n.t("plugin.richtext:editor.exportDocument")}
				</Button>
			{:else}
				<LexicalExtensionComposer {extension} contentEditable={null}>
					{#if config.admin.fixedToolbar && !editingBlocked}
						<div class="ridu-richtext-fixed-toolbar-slot" bind:this={fixedToolbarSlot}></div>
					{/if}
					<div class="ridu-richtext-canvas" bind:this={canvasElement}>
						<ContentEditable
							id={field.id}
							class="ridu-richtext-content"
							ariaLabel={field.admin.label}
							ariaDescribedBy={inputARIA["aria-describedby"]}
							ariaErrorMessage={inputARIA["aria-errormessage"]}
							ariaInvalid={inputARIA["aria-invalid"]}
							ariaRequired={field.required}
							aria-keyshortcuts={editingBlocked
								? undefined
								: hasRichTextFeature(config, "links")
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
					{#if hasRichTextFeature(config, "links")}
						<LinkPlugin />
					{/if}
					{#if !editingBlocked}
						<RichTextSlashMenu {authoring} {config} />
						<!-- Mounted after the canvas, where editor listeners have always run; the fixed
						     toolbar portals into its slot above the text. -->
						<RichTextToolbars {config} {fixedToolbarSlot} />
						{#if hasRichTextFeature(config, "links")}
							<RichTextLinkPlugin />
						{/if}
						{#if canvasElement !== null}
							<RichTextBlockToolbar anchorElement={canvasElement} {authoring} {config} />
						{/if}
						{#if hasRichTextFeature(config, "uploads")}
							<RichTextUploadPlugin {authoring} {config} {field} />
						{/if}
						{#if hasRichTextFeature(config, "relationships")}
							<RichTextRelationshipPlugin {authoring} {config} {field} />
						{/if}
					{/if}
					<RichTextEditabilityPlugin readOnly={editingBlocked} />
					<RichTextBlocksPlugin {authoring} {field} />
					<OnChangePlugin onChange={changed} ignoreSelectionChange />
					{#if !config.admin.hideInsertParagraphAtEnd}
						<RichTextFooter readOnly={editingBlocked} />
					{/if}
				</LexicalExtensionComposer>
			{/if}
		</div>
	</FieldFrame>
</div>
