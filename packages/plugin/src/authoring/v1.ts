import type { AdminPlugin } from "../plugin";
import { resolvePluginFields } from "../plugin-registry";

export { definePluginField, defineFieldComponent } from "../field";
export type {
	PluginFieldBinding,
	PluginFieldProps,
	PluginForm,
	PluginFieldRegistration,
} from "../field";

/** This literal belongs to this versioned entry point, never to the consuming runtime. */
const authoringAPIVersion = 1;

/**
 * Define the fields, pages, and other admin components supplied by a Go plugin.
 *
 * Export the result from the plugin's JavaScript package. The Go descriptor's
 * `Admin.Package` and `Admin.Export` identify that package and export. Applications
 * install both packages and load the generated plugins with `defineAdmin`.
 *
 * Required options are `key`, matching the Go plugin's key, and `pairingVersion`,
 * a positive integer matching its `Admin.PairingVersion`. Increase the latter when
 * an older Go or admin package would no longer work with the other. This import
 * supplies `apiVersion: 1`; do not pass or overwrite that property.
 *
 * Use `fields` for the default editors of new Go field types. Each map key matches
 * a Go `PluginFieldType.Key`, and each value is returned by `definePluginField`.
 * Use `components` for alternative editors made with `defineFieldComponent`,
 * selected explicitly in Go with `field.PluginComponent`. The other options add
 * pages, dashboard panels, navigation, and the UI described by `AdminContributions`.
 *
 * @param plugin The plugin key, compatibility version, and components to register.
 * @returns A frozen copy with `apiVersion: 1`, preserving inferred field value and
 * input types. Export it; do not call its components yourself or edit its maps.
 * @throws If required metadata, field registrations, or component keys are invalid.
 * Run `ridu check` in an application to also check the Go descriptor and selections.
 * @example
 * ```ts
 * import { defineAdminPlugin, definePluginField } from '@riducms/plugin/authoring/v1';
 * import ColorField from './color-field.svelte';
 * import { decodeColor } from './value';
 *
 * export const colorAdminPlugin = defineAdminPlugin({
 *   key: 'color',
 *   pairingVersion: 1,
 *   fields: {
 *     color: definePluginField({ component: ColorField, decodeValue: decodeColor })
 *   }
 * });
 * ```
 */
export function defineAdminPlugin<const Plugin extends Omit<AdminPlugin, "apiVersion">>(
	plugin: Plugin & { apiVersion?: never }
): Plugin & { readonly apiVersion: 1 } {
	if ("apiVersion" in plugin)
		throw new Error(
			"The authoring import owns apiVersion; do not restamp another plugin definition."
		);
	const result = Object.freeze({
		...plugin,
		...(plugin.fields === undefined ? {} : { fields: Object.freeze({ ...plugin.fields }) }),
		...(plugin.components === undefined
			? {}
			: { components: Object.freeze({ ...plugin.components }) }),
		apiVersion: authoringAPIVersion,
	});
	const definition: Omit<AdminPlugin, "apiVersion"> = plugin;
	resolvePluginFields([{ ...definition, apiVersion: authoringAPIVersion }]);
	return result;
}
