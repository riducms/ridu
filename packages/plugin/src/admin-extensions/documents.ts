import type { OperationCapabilities, SchemaCollection } from "@riducms/protocol";
import type { Component } from "svelte";

import type { FieldDocument } from "../authoring";
import type { AdminI18n, ExtensionTranslationKey } from "../i18n";
import type { AdminExtensionNotificationTone } from "./shared";

/** Tools for a document action or extra document view after it completes its own operation. */
export interface AdminDocumentExtensionHost {
	/** Reload the current document from the server. This is not a save of unsaved form edits. */
	refresh: () => Promise<void>;
	/** Show a temporary success or error message. */
	notify: (tone: AdminExtensionNotificationTone, title: string, message?: string) => void;
}

/** Saved document data for actions/views; use the generated SDK for application-specific operations. */
export interface AdminDocumentExtensionProps {
	collection: SchemaCollection;
	document: FieldDocument;
	host: AdminDocumentExtensionHost;
	i18n: AdminI18n;
}

/** Add a component alongside the document's standard actions. Implement the operation in that component. */
export interface AdminDocumentAction {
	key: string;
	/** Omit to show the action for every non-global collection. */
	collection?: string;
	/** Hide unless this document operation is allowed. Visibility does not replace server authorization. */
	requires?: keyof OperationCapabilities;
	component: Component<AdminDocumentExtensionProps>;
}

/** Add a tab beside Edit and API. Keys `edit` and `api` are reserved for Ridu. */
export interface AdminDocumentView {
	key: string;
	label: string;
	labelKey?: ExtensionTranslationKey;
	/** Collection or global slug; omit to show for every collection and global. */
	collection?: string;
	component: Component<AdminDocumentExtensionProps>;
}
