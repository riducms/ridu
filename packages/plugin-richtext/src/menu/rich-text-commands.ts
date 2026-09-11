import {
	createCommand,
	HISTORY_MERGE_TAG,
	HISTORY_PUSH_TAG,
	type LexicalCommand,
	type NodeKey,
} from "lexical";

export interface OpenUploadBrowserPayload {
	collectionSlug: string;
	documentID?: string;
	mode?: "edit" | "insert" | "replace";
	nodeKey?: NodeKey;
}

export type OpenRelationshipBrowserPayload = OpenUploadBrowserPayload;

export interface UpdateUploadCaptionPayload {
	caption: string;
	nodeKey: NodeKey;
}

export const OPEN_UPLOAD_BROWSER_COMMAND: LexicalCommand<OpenUploadBrowserPayload> = createCommand(
	"OPEN_UPLOAD_BROWSER_COMMAND"
);

export const OPEN_RELATIONSHIP_BROWSER_COMMAND: LexicalCommand<OpenRelationshipBrowserPayload> =
	createCommand("OPEN_RELATIONSHIP_BROWSER_COMMAND");

export const REMOVE_UPLOAD_COMMAND: LexicalCommand<NodeKey> =
	createCommand("REMOVE_UPLOAD_COMMAND");

export const REMOVE_RELATIONSHIP_COMMAND: LexicalCommand<NodeKey> = createCommand(
	"REMOVE_RELATIONSHIP_COMMAND"
);

export const UPDATE_UPLOAD_CAPTION_COMMAND: LexicalCommand<UpdateUploadCaptionPayload> =
	createCommand("UPDATE_UPLOAD_CAPTION_COMMAND");

export const OPEN_BLOCK_EDITOR_COMMAND: LexicalCommand<{
	blockType?: string;
	nodeKey?: NodeKey;
	position?: { targetNodeKey: NodeKey; insertBefore: boolean };
}> = createCommand("OPEN_BLOCK_EDITOR_COMMAND");
export const DUPLICATE_BLOCK_COMMAND: LexicalCommand<NodeKey> =
	createCommand("DUPLICATE_BLOCK_COMMAND");
export const REMOVE_BLOCK_COMMAND: LexicalCommand<NodeKey> = createCommand("REMOVE_BLOCK_COMMAND");
export const MOVE_BLOCK_COMMAND: LexicalCommand<{ nodeKey: NodeKey; direction: -1 | 1 }> =
	createCommand("MOVE_BLOCK_COMMAND");
export const UPDATE_BLOCK_NAME_COMMAND: LexicalCommand<{
	nodeKey: NodeKey;
	identity: string;
	nameField: string;
	change: { field: string; value: string };
	historyTag: typeof HISTORY_PUSH_TAG | typeof HISTORY_MERGE_TAG;
}> = createCommand("UPDATE_BLOCK_NAME_COMMAND");
