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

/** A read-only copy of an array/block row, frozen recursively. Use it to display a heading, not edit data. */
export type RowLabelSnapshot = Readonly<Record<string, RowLabelValue>>;

/** Props for a paired plugin's custom array/block row heading. */
export interface RowLabelComponentProps {
	/** The containing array/block field's resolved definition. */
	field: SchemaField;
	/** Current row data. The component receives updated snapshots when the row changes. */
	row: RowLabelSnapshot;
	/** The current visual position, starting at one. */
	rowNumber: number;
	i18n: AdminI18n;
	/** Go `field.Admin{RowLabel: field.PluginComponent(pluginKey, componentKey, config)}` settings, when supplied. This API supplies unknown data; validate it before use. */
	config?: unknown;
}

/** A named array/block heading selected by Go `field.Admin.RowLabel` using `field.PluginComponent`. */
export interface RowLabelPlugin {
	/** Owning plugin key; must match the containing `defineAdminPlugin` registration. */
	key: string;
	/** Component name selected by the Go component reference, for example `"ReviewHeading"`. */
	componentKey: string;
	component: Component<RowLabelComponentProps>;
}

/**
 * Register a paired plugin's read-only row heading in its `rowLabels` array.
 * Go selects the same `key` and `componentKey` with `field.Admin{RowLabel: field.PluginComponent(key, componentKey, config)}`.
 * Your component checks its unknown config. Application-local headings instead
 * use `defineRowLabel` from `@riducms/plugin/admin`, which accepts a config decoder.
 */
export function defineRowLabelPlugin<const Plugin extends RowLabelPlugin>(plugin: Plugin): Plugin {
	return plugin;
}
