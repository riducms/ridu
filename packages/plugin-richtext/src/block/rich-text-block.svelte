<script lang="ts">
	import {
		useLexicalComposerContext,
		useLexicalEditable,
		useLexicalNodeSelection,
	} from "@hvniel/lexical-svelte";
	import {
		$getNodeByKey,
		REDO_COMMAND,
		UNDO_COMMAND,
		SKIP_DOM_SELECTION_TAG,
		type NodeKey,
	} from "lexical";
	import { getAdminI18n } from "@riducms/plugin";
	import { Button } from "@riducms/ui";
	import {
		getRichTextField,
		getRichTextAuthoringHost,
	} from "@plugin-richtext/field/rich-text-context.svelte";
	import { blockSummary, richTextBlockTypes } from "@plugin-richtext/field/rich-text-blocks";
	import { equalRichTextValues } from "@plugin-richtext/field/rich-text-value";
	import {
		BLOCK_FIELD_CHANGE_TAG,
		BlockFieldHistorySession,
	} from "@plugin-richtext/block/rich-text-block-history";
	import { isBlockNode } from "@plugin-richtext/block/rich-text-block-node";
	import ChevronDown from "~icons/lucide/chevron-down";
	import Copy from "~icons/lucide/copy";
	import X from "~icons/lucide/x";
	import {
		DUPLICATE_BLOCK_COMMAND,
		REMOVE_BLOCK_COMMAND,
		MOVE_BLOCK_COMMAND,
		UPDATE_BLOCK_NAME_COMMAND,
	} from "@plugin-richtext/menu/rich-text-commands";
	import "@plugin-richtext/block/rich-text-block.scss";

	let {
		nodeKey,
		fields,
		validEnvelope,
	}: { nodeKey: NodeKey; fields: Record<string, unknown>; validEnvelope: boolean } = $props();
	const editor = useLexicalComposerContext()[0];
	const i18n = getAdminI18n();
	const editable = useLexicalEditable();
	const context = getRichTextField();
	const authoring = getRichTextAuthoringHost();
	const nameHistory = new BlockFieldHistorySession();
	const fieldHistory = new BlockFieldHistorySession();
	let collapsed = $state(false);
	// The decorator instance owns this fixed Lexical node key.
	// svelte-ignore state_referenced_locally
	const [selected, setSelected, clearSelected] = useLexicalNodeSelection(nodeKey);
	const identity = $derived(typeof fields._key === "string" ? fields._key : "");
	const blockType = $derived(
		typeof fields.blockType === "string"
			? fields.blockType
			: i18n.t("plugin.richtext:block.unknown")
	);
	const type = $derived(
		richTextBlockTypes(context.field).find((candidate) => candidate.slug === blockType)
	);
	const nameField = $derived(type?.admin?.nameField);
	const issues = $derived(authoring?.schemaIssues?.({ treeKey: "blocks", identity }) ?? []);
	const recovery = $derived(type === undefined || !validEnvelope);
	const summary = $derived(blockSummary(fields, type) || i18n.t("plugin.richtext:block.untitled"));
	const label = $derived(type?.labels.singular ?? blockType);
	const errorCount = $derived(new Set(issues.map((issue) => issue.path)).size);

	// Submission feedback opens the disclosure before the form focuses its invalid child.
	$effect(() => {
		if (issues.length > 0) collapsed = false;
	});

	function select(event: MouseEvent) {
		event.stopPropagation();
		if (!event.shiftKey && !event.metaKey && !event.ctrlKey) clearSelected();
		setSelected(true);
	}

	function changeName(change: { field: string; value: string }) {
		if (nameField === undefined || recovery || !editable()) return;
		editor.dispatchCommand(UPDATE_BLOCK_NAME_COMMAND, {
			nodeKey,
			identity,
			nameField,
			change,
			historyTag: nameHistory.nextTag(),
		});
	}

	function nameKeydown(event: KeyboardEvent) {
		if (!(event.target instanceof HTMLInputElement)) return;
		event.stopPropagation();
		if (event.isComposing) return;
		if (event.key === "Enter") {
			event.preventDefault();
			return;
		}
		if (event.target.readOnly || event.target.disabled) return;
		const key = event.key.toLowerCase();
		const modifier = event.metaKey || event.ctrlKey;
		const undo = modifier && key === "z" && !event.shiftKey;
		const redo = modifier && ((key === "z" && event.shiftKey) || (key === "y" && !event.metaKey));
		if (!undo && !redo) return;
		event.preventDefault();
		nameHistory.reset();
		editor.dispatchCommand(undo ? UNDO_COMMAND : REDO_COMMAND, undefined);
	}

	function changeFields(payload: Record<string, unknown>) {
		if (recovery || !editable()) return;
		editor.update(
			() => {
				const node = $getNodeByKey(nodeKey);
				if (!isBlockNode(node) || node.getFields()._key !== identity) return;
				if (equalRichTextValues(node.getFields(), payload)) return;
				context.acceptEmbeddedChange();
				node.setFields(payload);
			},
			{
				tag: [BLOCK_FIELD_CHANGE_TAG, fieldHistory.nextTag(), SKIP_DOM_SELECTION_TAG],
				discrete: true,
			}
		);
	}

	function stopFieldEvent(event: Event) {
		event.stopPropagation();
	}

	function fieldKeydown(event: KeyboardEvent) {
		event.stopPropagation();
		if (event.defaultPrevented) return;
		const target = event.target;
		if (!(target instanceof HTMLElement) || !editable()) return;
		// Nested rich-text controls belong to their own editor, including its toolbar.
		const fieldEditor = target.closest(".ridu-richtext-editor");
		if (fieldEditor !== null && !fieldEditor.contains(editor.getRootElement())) return;
		if (event.isComposing) return;
		if (event.key === "Enter" && target instanceof HTMLInputElement) event.preventDefault();
		if (
			(target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement) &&
			(target.readOnly || target.disabled)
		)
			return;
		const key = event.key.toLowerCase();
		const modifier = event.metaKey || event.ctrlKey;
		const undo = modifier && key === "z" && !event.shiftKey;
		const redo = modifier && ((key === "z" && event.shiftKey) || (key === "y" && !event.metaKey));
		if (!undo && !redo) return;
		event.preventDefault();
		fieldHistory.reset();
		editor.dispatchCommand(undo ? UNDO_COMMAND : REDO_COMMAND, undefined);
	}

	function toggle() {
		collapsed = !collapsed;
	}

	function exportBlock() {
		const serialized = editor.read(() => $getNodeByKey(nodeKey)?.exportJSON());
		if (serialized === undefined) return;
		const url = URL.createObjectURL(
			new Blob([JSON.stringify(serialized, null, 2)], { type: "application/json" })
		);
		const link = document.createElement("a");
		link.href = url;
		link.download = `${blockType}-${identity || "historical"}.json`;
		link.click();
		setTimeout(() => URL.revokeObjectURL(url), 0);
	}

	function duplicate() {
		editor.dispatchCommand(DUPLICATE_BLOCK_COMMAND, nodeKey);
	}

	function remove() {
		editor.dispatchCommand(REMOVE_BLOCK_COMMAND, nodeKey);
	}

	function keydown(event: KeyboardEvent) {
		if (
			(event.key === "Delete" || event.key === "Backspace") &&
			event.target === event.currentTarget &&
			editable()
		) {
			event.preventDefault();
			event.stopPropagation();
			remove();
		} else if (
			editable() &&
			event.altKey &&
			event.shiftKey &&
			(event.key === "ArrowUp" || event.key === "ArrowDown")
		) {
			event.preventDefault();
			event.stopPropagation();
			editor.dispatchCommand(MOVE_BLOCK_COMMAND, {
				nodeKey,
				direction: event.key === "ArrowUp" ? -1 : 1,
			});
		} else if (event.key === "Enter" && event.target === event.currentTarget && editable()) {
			event.preventDefault();
			event.stopPropagation();
			toggle();
		}
	}
</script>

<article
	class={[
		"ridu-richtext-block",
		nameField !== undefined && "has-name-field",
		selected() && "is-selected",
		issues.length > 0 && "has-issues",
	]}
	data-block-key={identity}
	data-block-type={blockType}
	aria-label={i18n.t("plugin.richtext:block.label", { label: type?.labels.singular ?? blockType })}
	data-invalid={issues.length > 0}
	data-collapsed={collapsed}
	role="group"
>
	<div class="ridu-richtext-block__header">
		<button
			type="button"
			data-block-select
			class="ridu-richtext-block__select"
			aria-label={i18n.t("plugin.richtext:block.select", {
				label: type?.labels.singular ?? blockType,
			})}
			aria-pressed={selected()}
			onclickcapture={select}
			onkeydowncapture={keydown}
		>
			<span class="ridu-richtext-block__label">
				{type?.labels.singular ?? blockType}
			</span>
			{#if nameField === undefined}
				<span class="ridu-richtext-block__summary">
					{summary}
				</span>
			{/if}
		</button>
		{#if nameField !== undefined && authoring?.schemaHeader !== undefined}
			<div
				class="ridu-richtext-block__name"
				onfocuscapture={nameHistory.reset.bind(nameHistory)}
				onblurcapture={nameHistory.reset.bind(nameHistory)}
				onkeydowncapture={nameKeydown}
			>
				{@render authoring.schemaHeader({
					treeKey: "blocks",
					identity,
					readOnly: !editable() || recovery,
					onChange: changeName,
				})}
			</div>
		{/if}
		{#if errorCount > 0}
			<span class="ridu-richtext-block__errors">
				{i18n.t(errorCount === 1 ? "plugin.richtext:block.error" : "plugin.richtext:block.errors", {
					count: errorCount,
				})}
			</span>
		{/if}
		{#if editable()}
			<div
				class="ridu-richtext-block__actions"
				role="group"
				aria-label={i18n.t("plugin.richtext:editor.blockActions")}
			>
				<Button
					class="ridu-richtext-block__action"
					size="sm"
					variant="ghost"
					onclick={duplicate}
					disabled={recovery}
					aria-label={i18n.t("plugin.richtext:block.duplicate")}
				>
					<Copy aria-hidden="true" />
				</Button>
				<Button
					class="ridu-richtext-block__action"
					size="sm"
					variant="ghost"
					onclick={remove}
					aria-label={i18n.t("plugin.richtext:block.remove")}
				>
					<X aria-hidden="true" />
				</Button>
			</div>
		{/if}
		{#if !recovery}
			<button
				type="button"
				class="ridu-richtext-block__toggle"
				onclick={toggle}
				aria-label={i18n.t(
					collapsed ? "plugin.richtext:block.expand" : "plugin.richtext:block.collapse",
					{ label }
				)}
				aria-expanded={!collapsed}
			>
				<ChevronDown aria-hidden="true" />
			</button>
		{/if}
	</div>
	{#if recovery}
		<div class="ridu-richtext-block__recovery">
			<p role="alert" class="ridu-richtext-block__status">
				{i18n.t("plugin.richtext:block.recovery")}
			</p>
			<Button size="sm" variant="outline" onclick={exportBlock}>
				{i18n.t("plugin.richtext:block.export")}
			</Button>
		</div>
	{:else if authoring?.schemaForm !== undefined}
		<div
			class="ridu-richtext-block__fields"
			role="presentation"
			hidden={collapsed}
			onfocuscapture={fieldHistory.reset.bind(fieldHistory)}
			onblurcapture={fieldHistory.reset.bind(fieldHistory)}
			onkeydown={fieldKeydown}
			onclick={stopFieldEvent}
			onpointerdown={stopFieldEvent}
			oncopy={stopFieldEvent}
			oncut={stopFieldEvent}
			onpaste={stopFieldEvent}
		>
			{@render authoring.schemaForm({
				treeKey: "blocks",
				identity,
				readOnly: !editable(),
				onChange: changeFields,
			})}
		</div>
	{/if}
</article>
