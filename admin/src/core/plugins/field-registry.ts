import { resolvePluginFields, type AdminPlugin, type ResolvedPluginField } from "@riducms/plugin";
import type { FieldType, SchemaField } from "@riducms/protocol";

interface FieldRenderer {
	type: FieldType;
	extension?: ResolvedPluginField;
}
export class FieldRegistry {
	#plugins = new Map<string, FieldRenderer>();
	register(plugin: ResolvedPluginField) {
		const identity =
			plugin.componentKey === undefined
				? `plugin:${plugin.key}`
				: `${plugin.registration.type}:${plugin.owner}:${plugin.key}`;
		if (this.#plugins.has(identity))
			throw new Error(`Field renderer ${identity} is already registered.`);
		this.#plugins.set(identity, { type: plugin.registration.type, extension: plugin });
	}
	resolve(field: SchemaField): FieldRenderer {
		if (field.admin.editor !== undefined)
			throw new Error(
				`No local editor ${field.admin.editor.reference} is registered for ${field.path}.`
			);
		const component = field.admin.component;
		if (component === undefined && field.type !== "plugin") return { type: field.type };
		const identity =
			component === undefined
				? `plugin:${field.plugin?.key}`
				: `${field.type}:${component.plugin}:${component.component}`;
		const renderer = this.#plugins.get(identity);
		if (!renderer)
			throw new Error(`No admin field renderer can render ${field.path} (${identity}).`);
		return renderer;
	}
}
export function createCoreFieldRegistry(plugins: readonly AdminPlugin[] = []): FieldRegistry {
	const registry = new FieldRegistry();
	for (const field of resolvePluginFields(plugins)) registry.register(field);
	return registry;
}
