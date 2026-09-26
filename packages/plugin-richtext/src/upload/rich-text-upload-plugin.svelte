<script lang="ts">
	import type { FieldAuthoringHost } from "@riducms/plugin";
	import type { SchemaCollection, SchemaField } from "@riducms/protocol";
	import { mergeRegister } from "@lexical/utils";
	import { $getNodeByKey, COMMAND_PRIORITY_EDITOR, type NodeKey } from "lexical";
	import { useLexicalComposerContext } from "@hvniel/lexical-svelte";

	import {
		OPEN_UPLOAD_BROWSER_COMMAND,
		REMOVE_UPLOAD_COMMAND,
		UPDATE_UPLOAD_CAPTION_COMMAND,
	} from "@plugin-richtext/menu/rich-text-commands";
	import type { RichTextConfig } from "@plugin-richtext/field/rich-text-config";
	import {
		insertReferenceNode,
		removeReferenceNode,
	} from "@plugin-richtext/field/reference-node-placement";
	import {
		createUploadNode,
		isUploadNode,
		UploadNode,
	} from "@plugin-richtext/upload/rich-text-upload-node";

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
	const uploadCollections = $derived(
		(authoring?.collections ?? []).filter(
			(collection) =>
				collection.capabilities.upload &&
				(config.uploadCollections.length === 0 ||
					config.uploadCollections.includes(collection.slug))
		)
	);
	const browserField = $derived(
		targetCollection === undefined ? undefined : createBrowserField(field, targetCollection)
	);

	$effect(() => {
		if (!editor.hasNodes([UploadNode])) {
			throw new Error("RichTextUploadPlugin requires UploadNode to be registered");
		}
		return mergeRegister(
			editor.registerCommand(
				OPEN_UPLOAD_BROWSER_COMMAND,
				(payload) => {
					const collection = uploadCollections.find(
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
				UPDATE_UPLOAD_CAPTION_COMMAND,
				({ caption, nodeKey }) => {
					const node = $getNodeByKey(nodeKey);
					if (!isUploadNode(node)) return false;
					node.setCaption(caption);
					return true;
				},
				COMMAND_PRIORITY_EDITOR
			),
			editor.registerCommand(
				REMOVE_UPLOAD_COMMAND,
				(nodeKey) => {
					const node = $getNodeByKey(nodeKey);
					if (!isUploadNode(node)) return false;
					removeReferenceNode(node);
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
				if (isUploadNode(current)) current.setReference(collection.slug, id).setCaption("");
				return;
			}
			const upload = createUploadNode({ documentID: id, relationTo: collection.slug });
			insertReferenceNode(upload);
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
			id: `${field.id}-richtext-upload`,
			name: `${field.name}Upload`,
			path: `${field.path}.__upload`,
			type: "upload",
			category: "upload",
			required: false,
			unique: false,
			admin: { label: collection.labels.singular },
			upload: {
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
