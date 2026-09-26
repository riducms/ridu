<script lang="ts">
	import {
		useLexicalComposerContext,
		useLexicalEditable,
		useLexicalNodeSelection,
	} from "@hvniel/lexical-svelte";
	import { $getNodeByKey, REDO_COMMAND, UNDO_COMMAND, type NodeKey } from "lexical";
	import { getAdminI18n } from "@riducms/plugin";
	import { Button } from "@riducms/ui";
	import {
		getRichTextField,
		getRichTextAuthoringHost,
	} from "@plugin-richtext/field/rich-text-context.svelte";
	import { blockSummary, richTextBlockTypes } from "@plugin-richtext/field/rich-text-blocks";
	import { BlockNameHistorySession } from "@plugin-richtext/block/rich-text-block-name";
	import {
		OPEN_BLOCK_EDITOR_COMMAND,
		DUPLICATE_BLOCK_COMMAND,
		REMOVE_BLOCK_COMMAND,
		MOVE_BLOCK_COMMAND,
		UPDATE_BLOCK_NAME_COMMAND,
	} from "@plugin-richtext/menu/rich-text-commands";
	import "@plugin-richtext/block/rich-text-block-card.scss";

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
	const nameHistory = new BlockNameHistorySession();
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
	const summary = $derived(blockSummary(fields, type) || i18n.t("plugin.richtext:block.noSummary"));
	function select(event: MouseEvent) {
		event.stopPropagation();
		if (!event.shiftKey) clearSelected();
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
	function edit() {
		editor.dispatchCommand(OPEN_BLOCK_EDITOR_COMMAND, { nodeKey });
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
			edit();
		}
	}
</script>

<article
	class={[
		"ridu-richtext-embedded-card ridu-richtext-block-card",
		nameField !== undefined && "has-name-field",
		selected() && "is-selected",
		issues.length > 0 && "has-issues",
	]}
	data-block-key={identity}
	data-block-type={blockType}
	aria-label={i18n.t("plugin.richtext:block.label", { label: type?.labels.singular ?? blockType })}
	data-invalid={issues.length > 0}
	role="group"
>
	{#if nameField !== undefined && authoring?.schemaHeader !== undefined}
		<div
			class="ridu-richtext-block-card__name"
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
	<div class="ridu-richtext-block-card__header">
		<button
			type="button"
			data-block-select
			class="ridu-richtext-block-card__select"
			aria-label={i18n.t("plugin.richtext:block.select", {
				label: type?.labels.singular ?? blockType,
			})}
			aria-pressed={selected()}
			onclickcapture={select}
			onkeydowncapture={keydown}
		>
			<span class="ridu-richtext-block-card__label">
				{type?.labels.singular ?? blockType}
			</span>
			{#if nameField === undefined}
				<span class="ridu-richtext-block-card__summary">
					{summary}
				</span>
			{/if}
		</button>
		{#if editable()}
			<div
				class="ridu-richtext-block-card__actions"
				role="group"
				aria-label={i18n.t("plugin.richtext:editor.blockActions")}
			>
				<Button
					class="ridu-richtext-block-card__action"
					size="sm"
					variant="ghost"
					onclick={edit}
					disabled={recovery}
				>
					{i18n.t("plugin.richtext:block.edit")}
				</Button>
				<Button
					class="ridu-richtext-block-card__action"
					size="sm"
					variant="ghost"
					onclick={duplicate}
					disabled={recovery}
				>
					{i18n.t("plugin.richtext:block.duplicate")}
				</Button>
				<Button class="ridu-richtext-block-card__action" size="sm" variant="ghost" onclick={remove}>
					{i18n.t("plugin.richtext:block.remove")}
				</Button>
			</div>
		{/if}
	</div>
	{#if recovery}
		<div class="ridu-richtext-block-card__recovery">
			<p role="alert" class="ridu-richtext-block-card__status">
				{i18n.t("plugin.richtext:block.recovery")}
			</p>
			<Button size="sm" variant="outline" onclick={exportBlock}>
				{i18n.t("plugin.richtext:block.export")}
			</Button>
		</div>
	{:else if issues.length > 0}
		<p class="ridu-richtext-block-card__status">
			{i18n.t("plugin.richtext:block.issues", { count: issues.length })}
		</p>
	{/if}
</article>
