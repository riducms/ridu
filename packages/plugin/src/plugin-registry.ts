import { ADMIN_PLUGIN_API_VERSION, type SchemaField, type SchemaManifest } from "@riducms/protocol";
import { assertPluginFieldRegistration, type RegisteredPluginField } from "./field";
import { assertAdminPluginPairs, type AdminPlugin } from "./plugin";

/** A registered field editor with the names Ridu uses to select it. Returned by registry resolution. */
export interface ResolvedPluginField {
	/** Owning Go/admin plugin key. */
	readonly owner: string;
	/** Field-type key for a default editor, or component name for an explicitly selected editor. */
	readonly key: string;
	/** Present only for editors registered in `components` and selected by Go `AdminComponent`. */
	readonly componentKey?: string;
	readonly registration: RegisteredPluginField;
}

/**
 * Collect field registrations, checking plugin IDs, field-type ownership, named
 * components and API versions. Throws on malformed/duplicate registrations.
 * Used by Ridu's runtime/check tooling; authors normally call the registration
 * helpers and retain their generated plugin list instead of invoking this directly.
 */
export function resolvePluginFields(
	plugins: readonly AdminPlugin[]
): readonly ResolvedPluginField[] {
	const owners = new Set<string>();
	const identities = new Set<string>();
	const resolved: ResolvedPluginField[] = [];
	for (const plugin of plugins) {
		if (!/^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/.test(plugin.key) || owners.has(plugin.key))
			throw new Error(`Invalid or duplicate admin plugin key ${plugin.key}.`);
		if (plugin.apiVersion !== ADMIN_PLUGIN_API_VERSION)
			throw new Error(
				`Admin plugin ${plugin.key} uses API ${plugin.apiVersion}; this admin supports ${ADMIN_PLUGIN_API_VERSION}.`
			);
		if (!Number.isSafeInteger(plugin.pairingVersion) || plugin.pairingVersion < 1)
			throw new Error(`Admin plugin ${plugin.key} requires a positive pairingVersion.`);
		owners.add(plugin.key);
		for (const [kind, entries] of [
			["fields", plugin.fields],
			["components", plugin.components],
		] as const) {
			if (entries === undefined) continue;
			if (
				typeof entries !== "object" ||
				entries === null ||
				Array.isArray(entries) ||
				(Object.getPrototypeOf(entries) !== Object.prototype &&
					Object.getPrototypeOf(entries) !== null)
			)
				throw new Error(`Plugin ${plugin.key} ${kind} must be a keyed registration object.`);
			for (const [key, registration] of Object.entries(entries)) {
				if (
					!(
						kind === "fields" ? /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/ : /^[a-zA-Z_$][a-zA-Z0-9_$]*$/
					).test(key)
				)
					throw new Error(`Invalid plugin ${kind} key ${key}.`);
				assertPluginFieldRegistration(registration);
				if (
					kind === "fields"
						? registration.type !== "plugin" || registration.fieldType !== undefined
						: registration.type === "plugin" && registration.fieldType === undefined
				)
					throw new Error(`Plugin ${plugin.key} ${kind}.${key} uses the wrong registration kind.`);
				const identity = kind === "fields" ? `field:${key}` : `component:${plugin.key}:${key}`;
				if (identities.has(identity))
					throw new Error(`Duplicate admin field renderer ${identity}.`);
				identities.add(identity);
				resolved.push(
					Object.freeze({
						owner: plugin.key,
						key,
						registration,
						...(kind === "components" ? { componentKey: key } : {}),
					})
				);
			}
		}
	}
	return Object.freeze(resolved);
}

/** Canonical manifest, actual executable registrations, and the same decoders as the host. */
export function validateManifestPluginPairs(
	plugins: readonly AdminPlugin[],
	registrations: readonly ResolvedPluginField[],
	manifest: Pick<SchemaManifest, "collections" | "globals" | "blocks"> &
		Partial<Pick<SchemaManifest, "plugins">>,
	complete: boolean
): void {
	// Startup may receive only public or access-filtered schemas. Build/check own
	// completeness; selected fields below still validate their exact renderer.
	if (complete && manifest.plugins !== undefined)
		for (const item of registrations) {
			if (
				item.registration.fieldType !== undefined &&
				!manifest.plugins.some((plugin) =>
					plugin.fieldTypes?.some((field) => field.key === item.registration.fieldType)
				)
			)
				throw new Error(
					`Named renderer ${item.owner}:${item.key} selects undeclared field type ${item.registration.fieldType}.`
				);
		}
	if (manifest.plugins !== undefined) {
		for (const backend of manifest.plugins) {
			if (backend.admin === undefined) continue;
			const plugin = plugins.find((candidate) => candidate.key === backend.key);
			if (plugin === undefined)
				throw new Error(`Missing admin registration for backend plugin ${backend.key}.`);
			assertAdminPluginPairs([
				{
					admin: plugin,
					backend: {
						...backend.admin,
						key: backend.key,
						fieldTypes: (backend.fieldTypes ?? []).map((field) => field.key),
					},
				},
			]);
		}
		if (complete)
			for (const plugin of plugins) {
				if (
					!manifest.plugins.some(
						(backend) => backend.key === plugin.key && backend.admin !== undefined
					)
				)
					throw new Error(`Admin plugin ${plugin.key} has no paired Go admin descriptor.`);
			}
	}
}

/** Resolve a selection against static registrations; the caller owns schema traversal. */
export function createPluginFieldValidator(registrations: readonly ResolvedPluginField[]) {
	const fields = new Map(
		registrations.filter((item) => item.componentKey === undefined).map((item) => [item.key, item])
	);
	const components = new Map(
		registrations
			.filter((item) => item.componentKey !== undefined)
			.map((item) => [`${item.owner}:${item.key}`, item])
	);
	return (field: SchemaField): void => {
		const selection = field.admin.component;
		const selected =
			selection !== undefined
				? components.get(`${selection.plugin}:${selection.component}`)
				: field.type === "plugin"
					? fields.get(field.plugin?.key ?? "")
					: undefined;
		if (selection !== undefined || field.type === "plugin") {
			if (selected === undefined)
				throw new Error(
					`Missing renderer for ${selection !== undefined ? `${selection.plugin}:${selection.component}` : field.plugin?.key}.`
				);
			selected.registration.decodeConfig(field);
		}
	};
}
