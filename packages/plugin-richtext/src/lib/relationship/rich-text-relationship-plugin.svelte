<script lang="ts">
	import type { FieldAuthoringHost } from "@riducms/plugin";
	import type { SchemaCollection, SchemaField } from "@riducms/protocol";
	import { mergeRegister } from "@lexical/utils";
	import { $getNodeByKey, COMMAND_PRIORITY_EDITOR, type NodeKey } from "lexical";
	import { useLexicalComposerContext } from "@hvniel/lexical-svelte";

	import {
		OPEN_RELATIONSHIP_BROWSER_COMMAND,
		REMOVE_RELATIONSHIP_COMMAND,
	} from "#lib/menu/rich-text-commands.js";
	import type { RichTextConfig } from "#lib/field/rich-text-config.js";
	import { richTextRelationshipCollections } from "#lib/relationship/rich-text-relationship-options.js";
	import { insertReferenceNode, removeReferenceNode } from "#lib/field/reference-node-placement.js";
	import {
		createRelationshipNode,
		isRelationshipNode,
		RelationshipNode,
	} from "#lib/relationship/rich-text-relationship-node.js";

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
	let targetCollection = $state.raw<SchemaCollection>();
	let documentID = $state<string>();
	let browserMode = $state<"edit" | "insert" | "replace">("insert");
	let replaceNodeKey: NodeKey | undefined;
	const relationshipCollections = $derived(richTextRelationshipCollections(config, authoring));
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
					const collection =
						payload.collectionSlug === undefined
							? relationshipCollections[0]
							: relationshipCollections.find(
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
					removeReferenceNode(node);
					return true;
				},
				COMMAND_PRIORITY_EDITOR
			)
		);
	});

	function commit(ids: string[], collectionSlug: string) {
		const id = ids[0];
		if (
			id === undefined ||
			!relationshipCollections.some((collection) => collection.slug === collectionSlug)
		)
			return;
		if (browserMode === "edit") {
			closeBrowser();
			return;
		}
		editor.update(() => {
			if (replaceNodeKey !== undefined) {
				const current = $getNodeByKey(replaceNodeKey);
				if (isRelationshipNode(current)) current.setReference(collectionSlug, id);
				return;
			}
			const relationship = createRelationshipNode({
				documentID: id,
				relationTo: collectionSlug,
			});
			insertReferenceNode(relationship);
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
		{...browserMode === "edit" ? {} : { collections: relationshipCollections }}
		hasMany={false}
		selectedIDs={documentID === undefined ? [] : [documentID]}
		{...browserMode === "edit" && documentID !== undefined ? { initialDocumentID: documentID } : {}}
		onCommit={commit}
		onClose={closeBrowser}
	/>
{/if}
