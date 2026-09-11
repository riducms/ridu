import { bindSchemaManifest } from "@riducms/protocol";
import { resolveBlockTypes } from "@riducms/protocol";
import {
	resolvePluginFields,
	validateManifestPluginPairs,
	createPluginFieldValidator,
	type ResolvedPluginField,
} from "./plugin-registry";
import { isRegisteredRowLabel, type RegisteredRowLabel } from "./local-row-label";
import { localEditorReference } from "./editor/registry";
import type { SchemaField } from "@riducms/protocol";
import type { SchemaManifest } from "@riducms/protocol";
import { en, type TranslationLanguage } from "@riducms/translations";
import type { PluginMessageCatalog } from "./i18n";
import {
	resolveAdminExtensions,
	type AdminContributions,
	type AdminPlugin,
	type ResolvedAdminExtensions,
} from "./plugin";
import {
	validateFieldEditorSelection,
	validateFieldEditorRegistrations,
	type FieldEditorConfig,
} from "./editor/registry";

/**
 * Settings for your application's admin, exported from `admin/src/admin.config.ts`.
 * Register your custom components here. Keep `generatedAdminPlugins` in `plugins`
 * to load installed plugins such as rich text. Use `fields` to replace inputs for
 * text, textarea, email, date, code, number and checkbox fields.
 */
export interface AdminConfig extends AdminContributions, FieldEditorConfig {
	/** Custom array or block row headings selected by Go `field.Admin{RowLabel: field.Component("app:name")}`. */
	rowLabels?: Readonly<Record<`app:${string}`, RegisteredRowLabel>>;
	/** Installed admin plugins, normally `generatedAdminPlugins` from Ridu's generated file. */
	plugins?: readonly AdminPlugin[];
	/** Interface messages made with `defineAdminMessages`; refer to them as `app:messageName`. */
	messages?: PluginMessageCatalog;
	/** Bundled admin UI language catalogs. These are separate from document content locales. */
	languages?: readonly TranslationLanguage[];
}

/**
 * Configure your application's admin components and installed plugins.
 *
 * Export the result as the default export of `admin/src/admin.config.ts`. Keep
 * `plugins: generatedAdminPlugins` to load installed packages such as rich text.
 * Add `fields` for inputs made with `defineFieldEditor`, `rowLabels` for headings
 * made with `defineRowLabel`, or options such as `dashboard`, `routes`, and
 * `documentActions` for other parts of the admin. All options are optional.
 *
 * Application field and row-label keys use `app:name`; select the same key in Go
 * with `field.Component`. A plugin's new field types instead belong inside its
 * `defineAdminPlugin({ fields: ... })` registration.
 *
 * Installed plugins load first, then your application components. Entries need
 * unique keys, and only one component may replace a given screen or location.
 * Run `ridu check` to also check selections, field types, and settings from Go.
 * These checks also run when building and starting the admin.
 *
 * @param config The installed plugins, custom components, and interface translations.
 * @returns The same typed configuration object. The admin
 * reads it when starting; calling this function does not mount any components.
 * @example
 * ```ts
 * import { defineAdmin } from '@riducms/plugin/admin';
 * import { generatedAdminPlugins } from './ridu.plugins.generated';
 * import WelcomePanel from './components/welcome-panel.svelte';
 *
 * export default defineAdmin({
 *   plugins: generatedAdminPlugins,
 *   dashboard: [{ key: 'welcome', component: WelcomePanel, position: 'before' }]
 * });
 * ```
 */
export function defineAdmin(config: AdminConfig): AdminConfig {
	return config;
}

/** Static configuration owned by one mounted admin. Changes require remount/HMR. */
export interface ResolvedAdminConfig {
	readonly plugins: readonly AdminPlugin[];
	readonly fields: readonly ResolvedPluginField[];
	readonly editors: NonNullable<AdminConfig["fields"]>;
	readonly rowLabels: NonNullable<AdminConfig["rowLabels"]>;
	readonly languages: readonly TranslationLanguage[];
	readonly extensions: Readonly<ResolvedAdminExtensions>;
}

/** Normalize and validate static registrations once, independently of schema refresh. */
export function resolveAdminConfig(config: AdminConfig = {}): ResolvedAdminConfig {
	if ("fieldPlugins" in config)
		throw new Error(
			"Application fieldPlugins are not supported; use a field-owned Editor component or a paired advanced field plugin."
		);
	validateFieldEditorRegistrations(config);
	for (const [reference, label] of Object.entries(config.rowLabels ?? {})) {
		if (!localEditorReference.test(reference) || !isRegisteredRowLabel(label))
			throw new Error(`Row label ${reference} must use an app:name reference and defineRowLabel.`);
	}
	const plugins = Object.freeze([...(config.plugins ?? [])]);
	return Object.freeze({
		plugins,
		fields: resolvePluginFields(plugins),
		editors: Object.freeze({ ...config.fields }),
		rowLabels: Object.freeze({ ...config.rowLabels }),
		languages: Object.freeze(config.languages?.length ? [...config.languages] : [en]),
		extensions: Object.freeze(resolveAdminExtensions(plugins, config)),
	});
}

/**
 * Check admin registrations against Ridu's resolved Go schema. Used by Ridu's build
 * tooling and admin startup; application authors normally run `ridu check` instead.
 * Set `completeManifest` only with the full schema from Go: runtime schemas can omit
 * resources the current user cannot access, so absence there does not prove a typo.
 * Throws with the affected registration or field when a selection/config is invalid.
 */
export function validateAdminManifest(
	config: ResolvedAdminConfig,
	manifest: Pick<SchemaManifest, "collections" | "globals" | "blocks"> &
		Partial<Pick<SchemaManifest, "plugins">>,
	options: { completeManifest?: boolean } = {}
): void {
	validateManifestPluginPairs(
		config.plugins,
		config.fields,
		manifest,
		options.completeManifest === true
	);
	const validatePluginField = createPluginFieldValidator(config.fields);
	const failures: string[] = [];
	bindSchemaManifest(manifest);
	const inspect = (fields: readonly SchemaField[], owner: string) => {
		for (const field of fields) {
			try {
				validateFieldEditorSelection(config.editors, field);
				validatePluginField(field);
				const selection = field.nested?.rowLabelComponent;
				if (selection?.reference !== undefined) {
					const reference = selection.reference;
					if (
						!localEditorReference.test(reference) ||
						selection.plugin !== undefined ||
						selection.component !== undefined ||
						(field.type !== "array" && field.type !== "blocks")
					)
						throw new Error("invalid local row label selection.");
					const label = config.rowLabels?.[reference as `app:${string}`];
					if (label === undefined)
						throw new Error(
							`row label ${reference} is not registered in admin/src/admin.config.ts.`
						);
					label.decode(field);
				}
			} catch (error) {
				failures.push(
					`${owner}.${field.path}: ${error instanceof Error ? error.message : String(error)}`
				);
			}
			for (const tree of field.plugin?.embeddedTrees ?? [])
				for (const item of tree.cases)
					for (const variant of resolveBlockTypes(item)) inspect(variant.fields, owner);
			if (field.nested !== undefined) inspect(field.nested.fields, owner);
			for (const block of resolveBlockTypes(field.blocks) ?? []) inspect(block.fields, owner);
		}
	};
	for (const collection of manifest.collections) inspect(collection.fields, collection.slug);
	for (const global of manifest.globals ?? []) inspect(global.fields, global.slug);
	if (failures.length) throw new Error(failures.join("\n"));
	// Runtime manifests are permission-filtered; only build checks can prove target absence.
	if (!options.completeManifest) return;
	const collections = new Map(manifest.collections.map((item) => [item.slug, item]));
	const globals = new Set((manifest.globals ?? []).map((item) => item.slug));
	const extensions = config.extensions;
	for (const cell of extensions.listCells) {
		const collection = collections.get(cell.collection);
		if (!collection?.fields.some((field) => field.name === cell.field || field.path === cell.field))
			throw new Error(
				`Admin list cell ${cell.key} selects unknown field ${cell.collection}.${cell.field}.`
			);
	}
	for (const view of extensions.views) {
		if ("collection" in view && view.collection !== undefined && !collections.has(view.collection))
			throw new Error(`Admin core view ${view.key} selects unknown collection ${view.collection}.`);
		if ("global" in view && view.global !== undefined && !globals.has(view.global))
			throw new Error(`Admin core view ${view.key} selects unknown global ${view.global}.`);
	}
	for (const action of extensions.documentActions) {
		if (action.collection !== undefined && !collections.has(action.collection))
			throw new Error(
				`Admin document action ${action.key} selects unknown collection ${action.collection}.`
			);
	}
	for (const view of extensions.documentViews) {
		if (
			view.collection !== undefined &&
			!collections.has(view.collection) &&
			!globals.has(view.collection)
		)
			throw new Error(
				`Admin document view ${view.key} selects unknown resource ${view.collection}.`
			);
	}
}

export { defineRowLabel } from "./local-row-label";
export type { RowLabelProps, RowLabelDefinition, RegisteredRowLabel } from "./local-row-label";
