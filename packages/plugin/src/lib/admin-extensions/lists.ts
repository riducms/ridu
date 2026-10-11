import type { SchemaCollection, SchemaField } from "@riducms/protocol";
import type { Component } from "svelte";

import type { FieldDocument } from "#lib/authoring.js";
import type { AdminI18n, ExtensionTranslationKey } from "#lib/i18n.js";

/** Live, read-only projections and selection commands from Ridu's list controller. */
export interface AdminCollectionList {
	/**
	 * The current page. Each document holds its metadata, title, visible columns and the fields
	 * the results layout declares in `fields`; the list doesn't read anything else.
	 */
	readonly documents: readonly FieldDocument[];
	readonly selectedIDs: ReadonlySet<string>;
	readonly selectionEnabled: boolean;
	readonly allOnPageSelected: boolean;
	readonly pageSelectionIndeterminate: boolean;
	toggleDocument(id: string, selected: boolean): void;
	togglePage(selected: boolean): void;
	title(document: FieldDocument): string;
	/** Router-root destination with an encoded ID and the current content locale. */
	documentHref(document: FieldDocument): string;
}

export interface AdminListResultsRendererProps {
	collection: SchemaCollection;
	list: AdminCollectionList;
	i18n: AdminI18n;
}

/** Collection-specific results layout. Trash retains its framework restore/delete controls. */
export interface AdminListResultsRenderer {
	key: string;
	collection: string;
	/**
	 * Top-level fields the layout reads beyond the title and visible columns, for example
	 * `["cover"]`. The list returns only the fields it shows. A relationship or upload field
	 * arrives as the related document's ID and title, like a relationship column.
	 */
	fields?: readonly string[];
	component: Component<AdminListResultsRendererProps>;
}

/** Data for displaying a collection table cell. This is not an editable document-form binding. */
export interface AdminListCellRendererProps {
	collection: SchemaCollection;
	field: SchemaField;
	/** The row's metadata, title, visible columns and the fields this cell declares in `fields`. */
	document: FieldDocument;
	/** This field's saved value. Check its type before rendering; changing it does not save a document. */
	value: unknown;
	i18n: AdminI18n;
}

/** Replace the cell display for one collection/field pair; duplicate targets are rejected. */
export interface AdminListCellRenderer {
	key: string;
	/** Collection slug, for example `"posts"`. */
	collection: string;
	/** Top-level field name/path in that collection, for example `"title"`. */
	field: string;
	/**
	 * Other top-level fields the cell reads from `document`, for example `["status"]`. The list
	 * returns only the fields it shows, so name every other value the cell uses. A relationship or
	 * upload field arrives as the related document's ID and title, like a relationship column.
	 */
	fields?: readonly string[];
	/** Fallback column heading. */
	label: string;
	/** Optional translation key from this plugin's messages, or app:... for application entries. */
	labelKey?: ExtensionTranslationKey;
	component: Component<AdminListCellRendererProps>;
}
