<script lang="ts">
	import {
		useLexicalComposerContext,
		useLexicalEditable,
		useLexicalNodeSelection,
	} from "@hvniel/lexical-svelte";
	import { getAdminI18n } from "@riducms/plugin";
	import { Button } from "@riducms/ui";
	import type { NodeKey } from "lexical";
	import ReplaceIcon from "~icons/lucide/refresh-cw";
	import RemoveIcon from "~icons/lucide/x";

	import { getRichTextAuthoringHost } from "#lib/field/rich-text-context.svelte.js";
	import {
		OPEN_RELATIONSHIP_BROWSER_COMMAND,
		REMOVE_RELATIONSHIP_COMMAND,
	} from "#lib/menu/rich-text-commands.js";
	import "#lib/relationship/rich-text-relationship-node.scss";

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
		document = undefined;
		loadError = false;
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
	class={["ridu-richtext-relationship-card", isSelected() && "is-selected"]}
	data-selected={isSelected() || undefined}
	aria-busy={authoring !== undefined && document === undefined && !loadError}
	role="group"
	aria-label={i18n.t("plugin.richtext:relationship.label", {
		collection: collection?.labels.singular ?? relationTo,
		title,
	})}
>
	<div class="ridu-richtext-relationship-card__content" aria-live="polite">
		<span class="ridu-richtext-relationship-card__label">
			{collection?.labels.singular ?? relationTo} · {documentID}
		</span>
		<button
			type="button"
			class="ridu-richtext-relationship-card__title"
			disabled={!isEditable()}
			onclick={openDocument}
		>
			{loadError ? i18n.t("plugin.richtext:relationship.unavailable") : title}
		</button>
	</div>
	{#if isEditable()}
		<div
			class="ridu-richtext-relationship-card__actions"
			role="group"
			aria-label={i18n.t("plugin.richtext:relationship.actions")}
		>
			<Button
				class="ridu-richtext-relationship-card__action"
				variant="ghost"
				size="icon-sm"
				onclick={replaceDocument}
				aria-label={i18n.t("plugin.richtext:relationship.replace")}
				tooltip={i18n.t("plugin.richtext:relationship.replace")}
			>
				<ReplaceIcon />
			</Button>
			<Button
				class="ridu-richtext-relationship-card__action"
				variant="ghost"
				size="icon-sm"
				onclick={removeRelationship}
				aria-label={i18n.t("plugin.richtext:relationship.remove")}
				tooltip={i18n.t("plugin.richtext:relationship.remove")}
			>
				<RemoveIcon />
			</Button>
		</div>
	{/if}
</article>
