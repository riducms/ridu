<script lang="ts">
	import {
		useLexicalComposerContext,
		useLexicalEditable,
		useLexicalNodeSelection,
	} from "@hvniel/lexical-svelte";
	import { $getNodeByKey, type NodeKey } from "lexical";
	import { getAdminI18n } from "@riducms/plugin";
	import { Button } from "@riducms/ui";
	import {
		getRichTextField,
		getRichTextAuthoringHost,
	} from "@plugin-richtext/field/rich-text-context.svelte";
	import { blockSummary, richTextBlockTypes } from "@plugin-richtext/field/rich-text-blocks";
	import {
		OPEN_BLOCK_EDITOR_COMMAND,
		DUPLICATE_BLOCK_COMMAND,
		REMOVE_BLOCK_COMMAND,
		MOVE_BLOCK_COMMAND,
	} from "@plugin-richtext/menu/rich-text-commands";

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
	const issues = $derived(authoring?.schemaIssues?.({ treeKey: "blocks", identity }) ?? []);
	const recovery = $derived(type === undefined || !validEnvelope);
	const summary = $derived(blockSummary(fields, type) || i18n.t("plugin.richtext:block.noSummary"));
	function select(event: MouseEvent) {
		event.stopPropagation();
		if (!event.shiftKey) clearSelected();
		setSelected(true);
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
		"ridu-richtext-embedded-card my-5 rounded-md border border-control-border bg-control p-4 outline-none focus-visible:ring-2 focus-visible:ring-ring",
		selected() && "border-primary",
		issues.length > 0 && "border-destructive",
	]}
	data-block-key={identity}
	data-block-type={blockType}
	aria-label={i18n.t("plugin.richtext:block.label", { label: type?.labels.singular ?? blockType })}
	data-invalid={issues.length > 0}
	role="group"
>
	<div class="flex items-center justify-between gap-3">
		<button
			type="button"
			data-block-select
			class="min-w-0 flex-1 text-start rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
			aria-label={i18n.t("plugin.richtext:block.select", {
				label: type?.labels.singular ?? blockType,
			})}
			aria-pressed={selected()}
			onclickcapture={select}
			onkeydowncapture={keydown}
		>
			<span class="block text-xs font-medium text-foreground-muted">
				{type?.labels.singular ?? blockType}
			</span>
			<span class="block mt-1 truncate text-sm text-foreground">{summary}</span>
		</button>
		{#if editable()}
			<div class="flex shrink-0 gap-1">
				<Button size="sm" variant="ghost" onclick={edit} disabled={recovery}>
					{i18n.t("plugin.richtext:block.edit")}
				</Button>
				<Button size="sm" variant="ghost" onclick={duplicate} disabled={recovery}>
					{i18n.t("plugin.richtext:block.duplicate")}
				</Button>
				<Button size="sm" variant="ghost" onclick={remove}>
					{i18n.t("plugin.richtext:block.remove")}
				</Button>
			</div>
		{/if}
	</div>
	{#if recovery}
		<p role="alert" class="mt-2 text-sm text-destructive">
			{i18n.t("plugin.richtext:block.recovery")}
		</p>
		<Button size="sm" variant="outline" onclick={exportBlock}>
			{i18n.t("plugin.richtext:block.export")}
		</Button>
	{:else if issues.length > 0}
		<p class="mt-2 text-sm text-destructive">
			{i18n.t("plugin.richtext:block.issues", { count: issues.length })}
		</p>
	{/if}
</article>
