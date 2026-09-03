<script lang="ts">
	import type { FieldComponentProps } from "@riducms/plugin";
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
	import { defineExtension, type EditorState } from "lexical";
	import { onDestroy } from "svelte";
	import { FieldFrame } from "@riducms/ui";

	import "@plugin-richtext/styles/integration.css";
	import RichTextBlockToolbar from "@plugin-richtext/menu/rich-text-block-toolbar.svelte";
	import RichTextFooter from "@plugin-richtext/field/rich-text-footer.svelte";
	import RichTextFloatingToolbar from "@plugin-richtext/toolbar/rich-text-floating-toolbar.svelte";
	import RichTextSlashMenu from "@plugin-richtext/menu/rich-text-slash-menu.svelte";
	import { parseRichTextConfig, hasRichTextFeature } from "@plugin-richtext/field/rich-text-config";
	import { setRichTextAuthoringHost } from "@plugin-richtext/field/rich-text-context.svelte";
	import { initialEditorState } from "@plugin-richtext/field/rich-text-document";
	import { UploadNode } from "@plugin-richtext/upload/rich-text-upload-node";
	import RichTextUploadPlugin from "@plugin-richtext/upload/rich-text-upload-plugin.svelte";
	import { RelationshipNode } from "@plugin-richtext/relationship/rich-text-relationship-node";
	import RichTextRelationshipPlugin from "@plugin-richtext/relationship/rich-text-relationship-plugin.svelte";

	let { field, form, authoring, i18n }: FieldComponentProps = $props();
	const issues = $derived(form.issuesFor(field.path));
	const messageID = $derived(`${field.id}-message`);
	const hasMessage = $derived(issues.length > 0 || field.admin.description !== undefined);
	const placeholderText = $derived(i18n.t("plugin.richtext:editor.placeholder"));
	let canvasElement = $state<HTMLElement | null>(null);
	// Field identity and plugin config are fixed for this mounted component.
	// svelte-ignore state_referenced_locally
	const config = parseRichTextConfig(field.plugin?.config);
	// The host is stable for this mounted field and decorator descendants inherit it through portals.
	// svelte-ignore state_referenced_locally
	setRichTextAuthoringHost(authoring);

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

	// Field identity is fixed for this mounted plugin component.
	// svelte-ignore state_referenced_locally
	const unregisterField = form.register(field.path);
	onDestroy(unregisterField);

	// The extension tree is identity-bearing and belongs to this field instance.
	// svelte-ignore state_referenced_locally
	const extension = defineExtension({
		$initialEditorState: initialEditorState(form.get(field.path)),
		dependencies: [
			RichTextExtension,
			HistoryExtension,
			...(hasRichTextFeature(config, "lists") ? [ListExtension] : []),
		],
		name: "@riducms/plugin-richtext/editor",
		namespace: `ridu-${field.id}`,
		nodes: [
			...(hasRichTextFeature(config, "links") ? [LinkNode] : []),
			...(hasRichTextFeature(config, "code") ? [CodeNode] : []),
			...(hasRichTextFeature(config, "horizontal-rule") ? [HorizontalRuleNode] : []),
			...(hasRichTextFeature(config, "uploads") ? [UploadNode] : []),
			...(hasRichTextFeature(config, "relationships") ? [RelationshipNode] : []),
		],
		editable: !field.admin.readOnly,
		theme,
		onError: (error) => {
			throw error;
		},
	});

	function changed(editorState: EditorState) {
		form.set(field.path, { version: 1, root: editorState.toJSON().root });
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
			aria-invalid={issues.length > 0}
		>
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
						ariaDescribedBy={hasMessage ? messageID : undefined}
						ariaInvalid={issues.length > 0}
						ariaRequired={field.required}
						aria-keyshortcuts="Alt+Shift+ArrowUp Alt+Shift+ArrowDown"
						aria-placeholder={placeholderText}
					>
						{#snippet placeholder(editable)}
							<span
								class={[
									"pointer-events-none absolute top-[1.35rem] start-9 z-0 text-[15.5px] leading-7 text-foreground-placeholder max-[34rem]:top-4 max-[34rem]:start-8",
									!editable && "top-0 start-0 max-[34rem]:top-0 max-[34rem]:start-0",
								]}
							>
								{editable ? placeholderText : i18n.t("plugin.richtext:editor.empty")}
							</span>
						{/snippet}
					</ContentEditable>
				</div>
				{#if hasRichTextFeature(config, "links")}<LinkPlugin />{/if}
				{#if hasRichTextFeature(config, "horizontal-rule")}<HorizontalRulePlugin />{/if}
				{#if !field.admin.readOnly}
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
				<OnChangePlugin onChange={changed} ignoreSelectionChange />
				<RichTextFooter />
			</LexicalExtensionComposer>
		</div>
	</FieldFrame>
</div>
