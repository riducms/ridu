import type { SchemaCollection, SchemaField, ValidationIssue } from "@riducms/protocol";
import type { Component, Snippet } from "svelte";

/** A saved document returned by Ridu. Check custom fields before using their unknown values. */
export type FieldDocument = Record<string, unknown> & {
	id: string;
	createdAt?: string;
	updatedAt?: string;
	deletedAt?: string;
	_status?: "draft" | "published";
	_revision?: number;
	_localization?: {
		sources: Record<string, string>;
	};
};

/** A condition that narrows documents offered by the reference browser. It does not grant access. */
export interface FieldReferenceFilter {
	field: string;
	operator:
		| "equals"
		| "notEquals"
		| "like"
		| "contains"
		| "greaterThan"
		| "greaterThanEqual"
		| "lessThan"
		| "lessThanEqual";
	value: string | number | boolean;
}

/**
 * Props for Ridu's related-document picker and editor. Render the component supplied
 * as `authoring.referenceBrowser`; your `onCommit` decides how selected IDs change
 * your field. Saving a related document inside the browser is a separate server save.
 */
export interface FieldReferenceBrowserProps {
	/** Whether the browser is visible. */
	open?: boolean;
	/** The relationship/upload field whose rules the browser should use. */
	field: SchemaField;
	/** The target collection's resolved schema, available from `authoring.collections`. */
	collection: SchemaCollection;
	/** Allow multiple selections when true; otherwise select at most one document. */
	hasMany: boolean;
	/** IDs currently selected by your editor. */
	selectedIDs: readonly string[];
	/** Further restrict editing; false cannot override Ridu's access restrictions. */
	readOnly?: boolean;
	/** Open this already-loaded document for editing instead of starting at the list. */
	initialDocument?: FieldDocument;
	/** Fetch and open this document when `initialDocument` is not supplied. */
	initialDocumentID?: string;
	/** Limit selectable documents; the server still applies its own access rules. */
	optionFilter?: FieldReferenceFilter | readonly FieldReferenceFilter[];
	/** Initial values for a new related document, not changes to an existing document. */
	defaultValues?: Readonly<Record<string, unknown>>;
	/** Allow creation when access permits it. Set false to hide creation. */
	allowCreate?: boolean;
	/** Content locale for loading and editing related documents. */
	locale?: string;
	/**
	 * Called when the user confirms the selection. Update your field here.
	 * Return false (or resolve to false) to keep the browser open. Returning void or
	 * true accepts the selection and closes it while editing remains allowed. A thrown
	 * error/rejected promise keeps it open and Ridu displays an error notification.
	 */
	onCommit: (ids: string[]) => void | boolean | Promise<void | boolean>;
	/** Called when the browser closes; update your open/closed UI state here. */
	onClose: () => void;
}

/**
 * Select an existing group of ordinary fields inside a plugin value, such as the
 * fields of one rich-text card. Go must declare the embedded tree and its schemas.
 * Ridu locates the data by its stable identity, even when the card moves.
 */
export interface EmbeddedSchemaFormScope {
	/** The embedded tree key declared by the Go field, for example `"widgets"`. */
	treeKey: string;
	/** The existing item's stable ID, as declared by that tree's identity configuration. */
	identity: string;
	/** Display without editing; false does not override document or field access rules. */
	readOnly?: boolean;
}

/** A configured editorial-name control for one existing embedded item. */
export interface EmbeddedSchemaHeaderProps extends EmbeddedSchemaFormScope {
	/**
	 * Receives a permitted change to the Go-configured name field. Apply it to the
	 * latest matching item through your editor's own history/serialization path.
	 * The host does not write the parent form or save the document itself.
	 */
	onChange: (change: { field: string; value: string }) => void;
}

/** Select a Go-declared embedded variant when creating or copying an item's field data. */
export interface EmbeddedSchemaVariantScope {
	/** The embedded tree key declared by the Go field. */
	treeKey: string;
	/** The tree case's configured tag value, not the name of the property holding the tag. */
	caseTag: string;
	/** The variant key within that case, for example `"hero"`. */
	variantSlug: string;
}

/**
 * A temporary Apply/Cancel form for fields inside one plugin item.
 * Its edits stay separate from the document until your Apply handler accepts them.
 * This is not a saved document draft/version and cannot save to the server itself.
 * Ridu tracks pending edits so the outer form can ask the user to Apply or Cancel.
 */
export interface EmbeddedSchemaDraft {
	/** Unique ID for this temporary editing session. */
	readonly id: string;
	/** Stable ID of the item being edited, or the proposed ID for a new item. */
	readonly identity: string;
	/** Whether edits await Apply/Cancel. A proposed new item counts as dirty even before typing. */
	readonly dirty: boolean;
	/**
	 * Whether the draft is no longer usable. Removal, changed source data, document/
	 * locale/schema/access changes, discard, or closing the parent editor can expire it.
	 * Reopen a draft instead of applying old data. Moving the same item alone is allowed.
	 */
	readonly stale: boolean;
	/** Current validation issues shown in this draft. */
	readonly issues: readonly ValidationIssue[];
	/** Copy the draft's current field values; throws if it has expired or editing is blocked. */
	payload(): Record<string, unknown>;
	/**
	 * Check the draft against its declared field rules and update `issues`.
	 * Returns whether it passes. Go validators and hooks run later, on document save.
	 * The guarded plugin method throws if the draft has expired or editing is blocked.
	 */
	validate(): boolean;
	/** Release this draft and abandon its pending edits. Safe to call more than once. */
	discard(): void;
}

/** Options for rendering a draft in Ridu's drawer with Apply and Cancel controls. */
export interface EmbeddedSchemaDraftEditorProps {
	/** A draft created by this field's `authoring.beginSchemaDraft`. */
	draft: EmbeddedSchemaDraft;
	/** Optional heading for the drawer. */
	title?: string;
	/**
	 * Receives checked field data when Apply succeeds. Insert or update the item in
	 * your plugin value and call `field.set` (or serialize your editor's updated state).
	 * Close your drawer UI here. Ridu discards the temporary draft after this handler
	 * returns; it does not insert data into your plugin value or save the document.
	 */
	onApply: (payload: Record<string, unknown>) => void;
	/** Close your editor UI after the pending draft has been discarded. */
	onCancel: () => void;
}

/**
 * Ridu tools supplied to a plugin field for embedded forms and related documents.
 * Optional methods must be checked before use. They work within the current field's
 * lifetime: async reads/requests reject if the field expires before they complete.
 * Already-dispatched server work is not undone. Cancel requests when appropriate,
 * and prevent an older request from overwriting a newer result in your own UI.
 */
export interface FieldAuthoringHost {
	/**
	 * Render the ordinary fields of an existing embedded item using
	 * `{@render authoring.schemaForm({ treeKey, identity })}`.
	 * Edits update the parent document's unsaved form immediately. Use a draft below
	 * if the user needs Apply/Cancel. Neither approach saves the document itself.
	 */
	schemaForm?: Snippet<[EmbeddedSchemaFormScope]>;
	/**
	 * Render the configured name input and field feedback, without the card chrome.
	 * Uses the item's schema, access, visibility and stable identity. An open draft
	 * locks the card input; an item without a name field renders no control.
	 */
	schemaHeader?: Snippet<[EmbeddedSchemaHeaderProps]>;
	/**
	 * Start a temporary Apply/Cancel form. Pass `{ treeKey, identity }` to edit an
	 * existing item, or `{ treeKey, caseTag, variantSlug }` to prepare a new one using
	 * Go-declared field defaults. Creation does not insert anything into your value.
	 * Call `discard` when abandoning a draft; closing the field also cleans it up.
	 */
	beginSchemaDraft?: (
		scope: EmbeddedSchemaFormScope | EmbeddedSchemaVariantScope
	) => EmbeddedSchemaDraft;
	/** Render a draft in Ridu's drawer; your `onApply` writes the accepted data into your plugin value. */
	schemaDraftEditor?: Snippet<[EmbeddedSchemaDraftEditorProps]>;
	/** Read current parent-form issues for an existing embedded item, for example for a card badge. */
	schemaIssues?: (scope: EmbeddedSchemaFormScope) => readonly ValidationIssue[];
	/**
	 * Copy an item's ordinary field data and give its schema-declared nested rows/items
	 * fresh IDs. Use when duplicating to avoid sharing identities. This does not insert
	 * the copy or copy your outer plugin item; update that structure in your editor.
	 */
	copySchemaPayload?: (
		scope: EmbeddedSchemaVariantScope,
		payload: Readonly<Record<string, unknown>>
	) => Record<string, unknown>;
	/** Collection definitions available in the current admin schema, not fetched documents. */
	readonly collections: readonly SchemaCollection[];
	/**
	 * Admin change counter: it advances when Ridu is notified that documents changed.
	 * Useful for refreshing lookup UI; it is not a document's saved `_revision` number.
	 */
	readonly documentRevision: number;
	/** The content locale being edited, separate from the admin interface language. */
	readonly locale?: string;
	/** Related-document picker/editor component. Its `onCommit` handler updates your field. */
	referenceBrowser: Component<FieldReferenceBrowserProps>;
	/**
	 * Fetch a saved document by collection slug and ID in the current content locale.
	 * Does not include unsaved edits from the current form. Rejects on request failure
	 * or if this field expires while waiting. Returns a copy; edits to it are local.
	 */
	findDocument(collection: string, id: string, signal?: AbortSignal): Promise<FieldDocument>;
	/**
	 * POST to an endpoint declared by this renderer's paired Go plugin. Supply the
	 * plugin-relative path, not an arbitrary URL. This sends a real server request;
	 * it does not automatically update or save the containing form.
	 *
	 * Requires an editable field and rejects if the field expires while waiting.
	 * Server work already sent may still finish. The `Result` generic describes your
	 * expected response; it does not validate it. Use `unknown` and decode the result
	 * when the response shape needs checking.
	 */
	requestPlugin?<Result>(path: string, body: unknown, signal?: AbortSignal): Promise<Result>;
}
