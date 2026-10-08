<script lang="ts">
	import type { PluginFieldProps } from "@riducms/plugin";
	import type { RichTextDocument } from "@riducms/sdk/richtext";
	import { FieldFrame, fieldControlARIA } from "@riducms/ui";

	import "#lib/field/rich-text-field.scss";
	import RichTextEditor from "#lib/editor/rich-text-editor.svelte";
	import type {
		RichTextEditorExtension,
		RichTextEditorExtensionState,
	} from "#lib/editor/rich-text-extension.js";
	import {
		hasRichTextFeature,
		isRichTextEditorFeature,
		type RichTextConfig,
	} from "#lib/field/rich-text-config.js";
	import { richTextBlockTypes } from "#lib/field/rich-text-blocks.js";
	import {
		setRichTextAuthoringHost,
		setRichTextField,
	} from "#lib/field/rich-text-context.svelte.js";
	import { BlockNode } from "#lib/block/rich-text-block-node.js";
	import { richTextBlockOptions } from "#lib/block/rich-text-block-options.js";
	import RichTextBlocksPlugin from "#lib/block/rich-text-blocks-plugin.svelte";
	import { RelationshipNode } from "#lib/relationship/rich-text-relationship-node.js";
	import { richTextRelationshipOptions } from "#lib/relationship/rich-text-relationship-options.js";
	import RichTextRelationshipPlugin from "#lib/relationship/rich-text-relationship-plugin.svelte";
	import { UploadNode } from "#lib/upload/rich-text-upload-node.js";
	import { richTextUploadOptions } from "#lib/upload/rich-text-upload-options.js";
	import RichTextUploadPlugin from "#lib/upload/rich-text-upload-plugin.svelte";

	let {
		field: binding,
		config,
		authoring,
		i18n,
		commitEditorValue,
		acceptEmbeddedChange,
	}: Omit<PluginFieldProps<RichTextDocument<unknown>, RichTextConfig>, "form"> & {
		commitEditorValue: (value: RichTextDocument<unknown>) => void;
		acceptEmbeddedChange: () => void;
	} = $props();
	const { schema: field, readOnly: editingBlocked, issues } = $derived(binding);
	const schemaReadOnly = $derived(field.admin.readOnly === true);
	// An edit whose export the server would reject stays in the editor and is held by the
	// form as a pending edit, so it can neither be saved nor left without a warning.
	let rejectedChange = $state<string>();
	const errors = $derived([
		...new Set([
			...issues.map((issue) => issue.message),
			...(rejectedChange === undefined ? [] : [rejectedChange]),
		]),
	]);
	const inputARIA = $derived(
		fieldControlARIA(field.id, field.admin.description !== undefined, errors.length > 0)
	);
	// The host is stable for this mounted field and decorator descendants inherit it through portals.
	// svelte-ignore state_referenced_locally
	setRichTextAuthoringHost(authoring);
	setRichTextField({
		acceptEmbeddedChange: () => acceptEmbeddedChange(),
		get field() {
			return field;
		},
	});

	// The admin's uploads, relationships and blocks use its drawers and schema forms, so they
	// join the core editor as extensions. The feature set belongs to this keyed mount.
	// svelte-ignore state_referenced_locally
	const extensions: RichTextEditorExtension[] = [
		...(hasRichTextFeature(config, "uploads")
			? [
					{
						nodes: [UploadNode],
						options: (editor, i18n) => richTextUploadOptions(editor, config, i18n, authoring),
						plugin: uploadPlugin,
					} satisfies RichTextEditorExtension,
				]
			: []),
		...(hasRichTextFeature(config, "relationships")
			? [
					{
						nodes: [RelationshipNode],
						options: (editor, i18n) => richTextRelationshipOptions(editor, config, i18n, authoring),
						plugin: relationshipPlugin,
					} satisfies RichTextEditorExtension,
				]
			: []),
		// Always registered, so a document holding blocks still opens.
		{
			nodes: [BlockNode],
			options: (editor, i18n) =>
				richTextBlockOptions(editor, config, i18n, authoring, richTextBlockTypes(field)),
			plugin: blocksPlugin,
		},
	];

	function changed(value: RichTextDocument<unknown>) {
		// Lexical can finish a queued update after a save replaces the binding.
		if (binding.stale) return;
		commitEditorValue(value);
	}

	function rejected(issue: string | undefined) {
		if (binding.stale) return;
		if (issue === undefined) {
			rejectedChange = undefined;
			binding.reportPendingEdit([]);
			return;
		}
		rejectedChange = i18n.t("plugin.richtext:editor.rejectedChange", {
			path: `${field.path}.${issue}`,
		});
		binding.reportPendingEdit([
			{ code: "rejected_rich_text_edit", path: field.path, message: rejectedChange },
		]);
	}
</script>

{#snippet uploadPlugin({ locked }: RichTextEditorExtensionState)}
	{#if !locked}
		<RichTextUploadPlugin {authoring} {config} {field} />
	{/if}
{/snippet}

{#snippet relationshipPlugin({ locked }: RichTextEditorExtensionState)}
	{#if !locked}
		<RichTextRelationshipPlugin {authoring} {config} {field} />
	{/if}
{/snippet}

{#snippet blocksPlugin()}
	<RichTextBlocksPlugin {authoring} {field} />
{/snippet}

<div data-field-path={field.path}>
	<FieldFrame
		controlID={field.id}
		label={field.admin.label}
		required={field.required}
		readOnly={field.admin.readOnly}
		description={field.admin.description}
		{errors}
		class="ridu-richtext-field"
	>
		<RichTextEditor
			value={binding.rawValue}
			features={config.features.filter(isRichTextEditorFeature)}
			toolbar={config.admin.fixedToolbar ? "both" : "floating"}
			hideGutter={config.admin.hideGutter}
			hideDraggableBlockElement={config.admin.hideDraggableBlockElement}
			hideAddBlockButton={config.admin.hideAddBlockButton}
			hideInsertParagraphAtEnd={config.admin.hideInsertParagraphAtEnd}
			{extensions}
			id={field.id}
			label={field.admin.label}
			name={field.name}
			path={field.path}
			required={field.required}
			readonly={schemaReadOnly}
			disabled={editingBlocked}
			invalid={inputARIA["aria-invalid"]}
			describedby={inputARIA["aria-describedby"]}
			errormessage={inputARIA["aria-errormessage"]}
			onchange={changed}
			onrejectedchange={rejected}
		/>
	</FieldFrame>
</div>
