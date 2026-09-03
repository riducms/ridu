<script lang="ts">
	import type { FieldAuthoringHost } from "@riducms/plugin";
	import type { SchemaCollection, SchemaField } from "@riducms/protocol";
	import { $insertNodeToNearestRoot, mergeRegister } from "@lexical/utils";
	import {
		$createParagraphNode,
		$getNodeByKey,
		$getPreviousSelection,
		$getRoot,
		$getSelection,
		$isElementNode,
		$isParagraphNode,
		$isRangeSelection,
		COMMAND_PRIORITY_EDITOR,
		type NodeKey,
	} from "lexical";
	import { useLexicalComposerContext } from "@hvniel/lexical-svelte";

	import {
		OPEN_RELATIONSHIP_BROWSER_COMMAND,
		REMOVE_RELATIONSHIP_COMMAND,
	} from "@plugin-richtext/menu/rich-text-commands";
	import type { RichTextConfig } from "@plugin-richtext/field/rich-text-config";
	import {
		createRelationshipNode,
		isRelationshipNode,
		RelationshipNode,
	} from "@plugin-richtext/relationship/rich-text-relationship-node";

	let {
		authoring,
		config,
		field,
	}: {
		authoring: FieldAuthoringHost | undefined;
		config: RichTextConfig;
		field: SchemaField;
	} = $props();
	const editor = useLexicalComposerContext()[0];
	let browserOpen = $state(false);
	let targetCollection = $state<SchemaCollection>();
	let documentID = $state<string>();
	let browserMode = $state<"edit" | "insert" | "replace">("insert");
	let replaceNodeKey: NodeKey | undefined;
	const relationshipCollections = $derived(
		(authoring?.collections ?? []).filter(
			(collection) =>
				config.relationshipCollections.length === 0 ||
				config.relationshipCollections.includes(collection.slug)
		)
	);
	const browserField = $derived(
		targetCollection === undefined ? undefined : createBrowserField(field, targetCollection)
	);

	$effect(() => {
		if (!editor.hasNodes([RelationshipNode])) {
			throw new Error("RichTextRelationshipPlugin requires RelationshipNode to be registered");
		}
		return mergeRegister(
			editor.registerCommand(
				OPEN_RELATIONSHIP_BROWSER_COMMAND,
				(payload) => {
					const collection = relationshipCollections.find(
						(candidate) => candidate.slug === payload.collectionSlug
					);
					if (collection === undefined) return false;
					browserMode =
						payload.mode ??
						(payload.nodeKey !== undefined
							? "replace"
							: payload.documentID !== undefined
								? "edit"
								: "insert");
					targetCollection = collection;
					documentID = payload.documentID;
					replaceNodeKey = browserMode === "replace" ? payload.nodeKey : undefined;
					browserOpen = true;
					return true;
				},
				COMMAND_PRIORITY_EDITOR
			),
			editor.registerCommand(
				REMOVE_RELATIONSHIP_COMMAND,
				(nodeKey) => {
					const node = $getNodeByKey(nodeKey);
					if (!isRelationshipNode(node)) return false;
					const next = node.getNextSibling();
					const previous = node.getPreviousSibling();
					node.remove();
					if ($isElementNode(next)) next.selectStart();
					else if ($isElementNode(previous)) previous.selectEnd();
					else {
						const paragraph = $createParagraphNode();
						$getRoot().append(paragraph);
						paragraph.select();
					}
					return true;
				},
				COMMAND_PRIORITY_EDITOR
			)
		);
	});

	function commit(ids: string[]) {
		const id = ids[0];
		const collection = targetCollection;
		if (id === undefined || collection === undefined) return;
		if (browserMode === "edit") {
			closeBrowser();
			return;
		}
		editor.update(() => {
			if (replaceNodeKey !== undefined) {
				const current = $getNodeByKey(replaceNodeKey);
				if (isRelationshipNode(current)) current.setReference(collection.slug, id);
				return;
			}
			const selection = $getSelection() ?? $getPreviousSelection();
			if (!$isRangeSelection(selection)) $getRoot().selectEnd();
			const focusNode = $isRangeSelection(selection) ? selection.focus.getNode() : undefined;
			const relationship = createRelationshipNode({
				documentID: id,
				relationTo: collection.slug,
			});
			$insertNodeToNearestRoot(relationship);
			if ($isParagraphNode(focusNode) && focusNode.getChildrenSize() === 0) focusNode.remove();
			const paragraph = $createParagraphNode();
			relationship.insertAfter(paragraph);
			paragraph.select();
		});
		closeBrowser();
	}

	function closeBrowser() {
		browserOpen = false;
		browserMode = "insert";
		documentID = undefined;
		replaceNodeKey = undefined;
		queueMicrotask(() => editor.focus());
	}

	function createBrowserField(field: SchemaField, collection: SchemaCollection): SchemaField {
		return {
			id: `${field.id}-richtext-relationship`,
			name: `${field.name}Relationship`,
			path: `${field.path}.__relationship`,
			type: "relationship",
			category: "relationship",
			required: false,
			unique: false,
			admin: { label: collection.labels.singular },
			relationship: {
				collectionId: collection.id,
				collectionSlug: collection.slug,
				onDelete: "nullify",
			},
		};
	}
</script>

{#if browserOpen && authoring !== undefined && targetCollection !== undefined && browserField !== undefined}
	{const ReferenceBrowser = authoring.referenceBrowser}
	<ReferenceBrowser
		open
		field={browserField}
		collection={targetCollection}
		hasMany={false}
		selectedIDs={documentID === undefined ? [] : [documentID]}
		{...browserMode === "edit" && documentID !== undefined ? { initialDocumentID: documentID } : {}}
		onCommit={commit}
		onClose={closeBrowser}
	/>
{/if}
