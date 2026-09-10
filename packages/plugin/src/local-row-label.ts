import type { Component } from "svelte";
import type { SchemaField } from "@riducms/protocol";
import type { RowLabelComponentProps } from "./row-label";
import { decodeComponentConfig } from "./component-config";

/** Props for an application-local array/block heading; row data is read-only and config is decoded. */
export type RowLabelProps<Config = undefined> = Omit<RowLabelComponentProps, "config"> &
	([Config] extends [undefined] ? { config?: never } : { config: Config });
/** Heading component plus a synchronous settings decoder, required when Config is not undefined. */
export type RowLabelDefinition<Config = undefined> = {
	component: Component<RowLabelProps<NoInfer<Config>>>;
} & (0 extends 1 & Config
	? { decodeConfig: (value: unknown) => Config }
	: [Config] extends [never]
		? { decodeConfig: (value: unknown) => Config }
		: [Config] extends [undefined]
			? { decodeConfig?: (value: unknown) => Config }
			: { decodeConfig: (value: unknown) => Config });
const registration = Symbol("ridu-row-label");
const registered = new WeakSet<RegisteredRowLabel>();
/** Helper result stored in `defineAdmin({ rowLabels: ... })`; do not construct it manually. */
export interface RegisteredRowLabel {
	readonly [registration]: true;
	readonly component: Component<never>;
	decode(field: SchemaField): unknown;
}

/**
 * Register a custom heading for an array or block row in your application.
 *
 * Put the result under an `app:name` key in `defineAdmin`'s `rowLabels`. Select
 * that key in Go with `field.Admin{RowLabel: field.Component("app:name")}`.
 * Keep `RowLabelPath` for the plain-text name used by row actions and screen readers.
 *
 * `component` is required and receives `RowLabelProps`: `row` contains current,
 * unsaved values, and `rowNumber` starts at one. Display a summary here; use a
 * field component for controls that edit values.
 *
 * Add `decodeConfig` when the Go `field.Component` call supplies settings. Return
 * checked settings synchronously, or throw a useful error. A decoder requires a
 * Go configuration object, even for empty settings; without one, omit Go settings.
 *
 * @param definition The heading component and optional settings decoder.
 * @returns A frozen registration for `defineAdmin`'s `rowLabels` map. It does not
 * choose a Go field or mount the component until the admin renders that field.
 * @throws If the component is invalid. `ridu check` also checks Go selections and settings.
 * @example
 * ```ts
 * import { defineAdmin, defineRowLabel } from '@riducms/plugin/admin';
 * import { generatedAdminPlugins } from './ridu.plugins.generated';
 * import LinkRowLabel from './components/link-row-label.svelte';
 *
 * export default defineAdmin({
 *   plugins: generatedAdminPlugins,
 *   rowLabels: {
 *     'app:linkSummary': defineRowLabel({ component: LinkRowLabel })
 *   }
 * });
 * ```
 */
export function defineRowLabel<Config = undefined>(
	definition: RowLabelDefinition<Config>
): RegisteredRowLabel {
	if (typeof definition.component !== "function")
		throw new Error("A row label requires a Svelte component.");
	const { component, decodeConfig } = definition;
	const label = Object.freeze({
		[registration]: true as const,
		component,
		decode(field: SchemaField): unknown {
			const selection = field.nested?.rowLabelComponent;
			try {
				return decodeComponentConfig(selection, decodeConfig);
			} catch (error) {
				throw new Error(
					`Row label ${selection?.reference} config for ${field.path} is invalid: ${error instanceof Error ? error.message : String(error)}`
				);
			}
		},
	});
	registered.add(label);
	return label;
}

export function isRegisteredRowLabel(value: RegisteredRowLabel): boolean {
	return registered.has(value);
}
