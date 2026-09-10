<script lang="ts">
	import type { RichTextDocument } from "@plugin-richtext/document";
	import { decodeRichTextDocument } from "@plugin-richtext/document-validation";
	import type { RichTextConfig } from "@plugin-richtext/field/rich-text-config";
	import type { PluginFieldProps } from "@riducms/plugin";
	import { CodeNode } from "@lexical/code";
	import { HistoryExtension } from "@lexical/history";
	import { LinkNode } from "@lexical/link";
	import { ListExtension } from "@lexical/list";
	import { RichTextExtension } from "@lexical/rich-text";
	import {
		ContentEditable,
		HorizontalRuleNode,
		HorizontalRulePlugin,
		LexicalExtensionComposer,
		LinkPlugin,
		OnChangePlugin,
	} from "@hvniel/lexical-svelte";
	import { $createParagraphNode, $getRoot, defineExtension, type EditorState } from "lexical";
	import { Button, FieldFrame, fieldControlARIA } from "@riducms/ui";

	import "@plugin-richtext/styles/integration.css";
	import RichTextBlockToolbar from "@plugin-richtext/menu/rich-text-block-toolbar.svelte";
	import RichTextEditabilityPlugin from "@plugin-richtext/field/rich-text-editability-plugin.svelte";
	import RichTextFooter from "@plugin-richtext/field/rich-text-footer.svelte";
	import RichTextFloatingToolbar from "@plugin-richtext/toolbar/rich-text-floating-toolbar.svelte";
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
	const field = $derived(binding.schema);
	const editingBlocked = $derived(binding.readOnly);
	const issues = $derived(binding.issues);
	const inputARIA = $derived(
		fieldControlARIA(field.id, field.admin.description !== undefined, issues.length > 0)
	);
	const placeholderText = $derived(i18n.t("plugin.richtext:editor.placeholder"));
	const visiblePlaceholder = $derived(
		field.admin.readOnly ? i18n.t("plugin.richtext:editor.empty") : placeholderText
	);
	let canvasElement = $state<HTMLElement | null>(null);
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
			h1: "ridu-richtext-heading ridu-richtext-h1 mt-[2.1rem] mb-[0.85rem] font-serif text-[2rem] leading-[1.12] tracking-[-0.015em] text-foreground font-normal text-balance max-[34rem]:text-[1.75rem]",
			h2: "ridu-richtext-heading ridu-richtext-h2 mt-[2.1rem] mb-[0.85rem] font-serif text-[1.75rem] leading-[1.12] text-foreground font-normal text-balance",
			h3: "ridu-richtext-heading ridu-richtext-h3 mt-[2.1rem] mb-[0.85rem] font-sans text-lg leading-[1.35] text-foreground font-semibold text-balance",
		},
		code: "ridu-richtext-code-block my-6 block overflow-x-auto rounded-[3px] border border-control-border bg-control px-[1.1rem] py-4 font-mono text-[12.5px] leading-[1.7] text-foreground-strong [tab-size:2]",
		hr: "ridu-richtext-rule my-8 block cursor-pointer border-0 border-t border-border py-[0.65rem]",
		hrSelected: "is-selected",
		link: "ridu-richtext-link cursor-pointer text-brand-primary-soft underline decoration-primary/40 decoration-1 underline-offset-[0.18em] hover:text-primary-hover hover:decoration-current",
		list: {
			checklist:
				"ridu-richtext-checklist mt-1 mb-[1.35rem] list-none ps-0 text-[15.5px] leading-7 text-foreground-body",
			listitemChecked: "ridu-richtext-list-item-checked line-through opacity-70",
			listitemUnchecked: "ridu-richtext-list-item-unchecked",
			listitem: "ridu-richtext-list-item ps-1",
			nested: { listitem: "ridu-richtext-list-item-nested" },
			ol: "ridu-richtext-list-ordered mt-1 mb-[1.35rem] list-decimal ps-[1.55rem] text-[15.5px] leading-7 text-foreground-body",
			ul: "ridu-richtext-list-unordered mt-1 mb-[1.35rem] list-disc ps-[1.55rem] text-[15.5px] leading-7 text-foreground-body",
		},
		paragraph:
			"ridu-richtext-paragraph relative mb-[0.9rem] text-[15.5px] leading-7 text-foreground-body text-pretty last:mb-0",
		quote:
			"ridu-richtext-quote my-[1.85rem] border-s-2 border-primary/55 py-[0.1rem] ps-5 font-serif text-[19px] italic leading-6 text-foreground-strong text-pretty",
		text: {
			bold: "ridu-richtext-bold font-semibold",
			code: "ridu-richtext-inline-code rounded-[3px] border border-control-border bg-control px-[0.32em] py-[0.12em] font-mono text-[0.86em] text-foreground-strong",
			italic: "ridu-richtext-italic italic",
			subscript: "ridu-richtext-subscript",
			superscript: "ridu-richtext-superscript",
			strikethrough: "ridu-richtext-strikethrough line-through",
			underline: "ridu-richtext-underline underline underline-offset-[0.15em]",
		},
		upload: "ridu-richtext-upload m-0",
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
			...(hasRichTextFeature(config, "lists") ? [ListExtension] : []),
		],
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
		class="min-w-0"
	>
		<div
			class={[
				"ridu-richtext-editor min-w-0 border-b border-transparent pb-[0.65rem] transition-colors duration-150",
				issues.length > 0 && "border-b-destructive/22",
				field.admin.readOnly && "pt-[1.6rem]",
				issues.length > 0 && "has-error",
				field.admin.readOnly && "is-read-only",
			]}
			aria-invalid={inputARIA["aria-invalid"]}
		>
			{#if recoveryIssue !== undefined}
				<p role="alert" class="my-3 text-sm text-destructive">
					{i18n.t("plugin.richtext:editor.recovery", { path: `${field.path}.${recoveryIssue}` })}
				</p>
				<Button variant="outline" onclick={exportDocument}>
					{i18n.t("plugin.richtext:editor.exportDocument")}
				</Button>
			{:else}
				<LexicalExtensionComposer {extension} contentEditable={null}>
					<div class="relative" bind:this={canvasElement}>
						<ContentEditable
							id={field.id}
							class={[
								"ridu-richtext-content relative z-1 min-h-44 pt-[1.35rem] pe-0 pb-3 ps-9 text-foreground-body caret-primary outline-none focus-visible:outline-none max-[34rem]:min-h-36 max-[34rem]:pt-4 max-[34rem]:ps-8",
								field.admin.readOnly
									? "min-h-auto pt-0 ps-0 max-[34rem]:min-h-auto max-[34rem]:pt-0 max-[34rem]:ps-0"
									: "border-s border-border transition-colors duration-150",
								issues.length > 0 && !field.admin.readOnly && "border-s-destructive/72",
							]}
							ariaLabel={field.admin.label}
							ariaDescribedBy={inputARIA["aria-describedby"]}
							ariaErrorMessage={inputARIA["aria-errormessage"]}
							ariaInvalid={inputARIA["aria-invalid"]}
							ariaRequired={field.required}
							aria-keyshortcuts="Alt+Shift+ArrowUp Alt+Shift+ArrowDown"
							aria-placeholder={visiblePlaceholder}
						>
							{#snippet placeholder()}
								<span
									class={[
										"pointer-events-none absolute top-[1.35rem] start-9 z-0 text-[15.5px] leading-7 text-foreground-placeholder max-[34rem]:top-4 max-[34rem]:start-8",
										field.admin.readOnly && "top-0 start-0 max-[34rem]:top-0 max-[34rem]:start-0",
									]}
								>
									{visiblePlaceholder}
								</span>
							{/snippet}
						</ContentEditable>
					</div>
					{#if hasRichTextFeature(config, "links")}<LinkPlugin />{/if}
					{#if hasRichTextFeature(config, "horizontal-rule")}<HorizontalRulePlugin />{/if}
					{#if !editingBlocked}
						<RichTextSlashMenu {authoring} {config} />
						<RichTextFloatingToolbar />
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
					<RichTextFooter readOnly={field.admin.readOnly === true} />
				</LexicalExtensionComposer>
			{/if}
		</div>
	</FieldFrame>
</div>
