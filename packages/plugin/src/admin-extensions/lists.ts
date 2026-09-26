import type { SchemaCollection, SchemaField } from "@riducms/protocol";
import type { Component } from "svelte";

import type { FieldDocument } from "../authoring";
import type { AdminI18n, ExtensionTranslationKey } from "../i18n";

/** Live, read-only projections and selection commands from Ridu's list controller. */
export interface AdminCollectionList {
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
	component: Component<AdminListResultsRendererProps>;
}

/** Data for displaying a collection table cell. This is not an editable document-form binding. */
export interface AdminListCellRendererProps {
	collection: SchemaCollection;
	field: SchemaField;
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
	/** Fallback column heading. */
	label: string;
	/** Optional translation key from this plugin's messages, or app:... for application entries. */
	labelKey?: ExtensionTranslationKey;
	component: Component<AdminListCellRendererProps>;
}
