import type { SchemaField } from "@riducms/protocol";
import type { Component } from "svelte";

import type { AdminI18n } from "./i18n";

export type RowLabelValue =
	| string
	| number
	| boolean
	| null
	| undefined
	| readonly RowLabelValue[]
	| { readonly [key: string]: RowLabelValue };

/** A detached, deeply frozen view of the current array or blocks row. */
export type RowLabelSnapshot = Readonly<Record<string, RowLabelValue>>;

export interface RowLabelComponentProps {
	field: SchemaField;
	row: RowLabelSnapshot;
	/** The current visual position, starting at one. */
	rowNumber: number;
	i18n: AdminI18n;
	/** Deterministic JSON from field.nested.rowLabelComponent.config. */
	config: unknown;
}

/** One exact plugin-owned component available to array and blocks headings. */
export interface RowLabelPlugin {
	key: string;
	componentKey: string;
	component: Component<RowLabelComponentProps>;
}

export function defineRowLabelPlugin<const Plugin extends RowLabelPlugin>(plugin: Plugin): Plugin {
	return plugin;
}
