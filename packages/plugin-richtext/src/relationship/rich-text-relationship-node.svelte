<script lang="ts">
	import {
		useLexicalComposerContext,
		useLexicalEditable,
		useLexicalNodeSelection,
	} from "@hvniel/lexical-svelte";
	import { getAdminI18n } from "@riducms/plugin";
	import type { NodeKey } from "lexical";

	import { getRichTextAuthoringHost } from "@plugin-richtext/field/rich-text-context.svelte";
	import {
		OPEN_RELATIONSHIP_BROWSER_COMMAND,
		REMOVE_RELATIONSHIP_COMMAND,
	} from "@plugin-richtext/menu/rich-text-commands";

	let {
		documentID,
		nodeKey,
		relationTo,
	}: { documentID: string; nodeKey: NodeKey; relationTo: string } = $props();
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
	const titleField = $derived(
		collection?.admin.useAsTitle ??
			collection?.fields.find((field) => field.name === "title" || field.name === "name")?.name
	);
	const title = $derived(
		titleField !== undefined && typeof document?.[titleField] === "string"
			? String(document[titleField])
			: documentID
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
		editor.dispatchCommand(OPEN_RELATIONSHIP_BROWSER_COMMAND, {
			collectionSlug: relationTo,
			documentID,
			mode: "edit",
		});
	}

	function replaceDocument() {
		editor.dispatchCommand(OPEN_RELATIONSHIP_BROWSER_COMMAND, {
			collectionSlug: relationTo,
			documentID,
			mode: "replace",
			nodeKey,
		});
	}

	function removeRelationship() {
		editor.dispatchCommand(REMOVE_RELATIONSHIP_COMMAND, nodeKey);
		queueMicrotask(() => editor.focus());
	}
</script>

<article
	class={[
		"group/relationship relative my-6 flex items-center gap-3 rounded-[4px] border border-control-border bg-control px-4 py-3.5 transition-colors hover:border-primary/45",
		isSelected() && "border-primary/50",
	]}
	role="group"
	aria-label={i18n.t("plugin.richtext:relationship.label", {
		collection: collection?.labels.singular ?? relationTo,
		title,
	})}
>
	<span
		class="font-mono grid size-9 shrink-0 place-items-center rounded-full bg-primary/10 text-[11px] font-semibold text-brand-primary-soft"
		aria-hidden="true"
	>
		{(collection?.labels.singular ?? relationTo).slice(0, 2).toLocaleUpperCase(i18n.language)}
	</span>
	<button
		type="button"
		class="min-w-0 flex-1 cursor-pointer border-0 bg-transparent p-0 text-start outline-none"
		disabled={!isEditable()}
		onclick={openDocument}
	>
		<span class="block truncate text-[13.5px] font-medium text-foreground-strong">
			{loadError ? i18n.t("plugin.richtext:relationship.unavailable") : title}
		</span>
		<span class="font-mono mt-0.5 block truncate text-[10px] text-foreground-faint">
			{collection?.labels.singular ?? relationTo} · {documentID}
		</span>
	</button>
	{#if isEditable()}
		<div
			class="flex shrink-0 items-center gap-1"
			aria-label={i18n.t("plugin.richtext:relationship.actions")}
		>
			<button
				type="button"
				class="rounded-[3px] px-2 py-1.5 text-[11px] text-foreground-muted hover:bg-control-hover hover:text-foreground"
				onclick={replaceDocument}
			>
				{i18n.t("plugin.richtext:relationship.replace")}
			</button>
			<button
				type="button"
				class="rounded-[3px] px-2 py-1.5 text-[11px] text-destructive hover:bg-destructive/10"
				onclick={removeRelationship}
			>
				{i18n.t("plugin.richtext:relationship.remove")}
			</button>
		</div>
	{/if}
</article>
