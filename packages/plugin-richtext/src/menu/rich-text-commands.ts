import { createCommand, type LexicalCommand, type NodeKey } from "lexical";

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
