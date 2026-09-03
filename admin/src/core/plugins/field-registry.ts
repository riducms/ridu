import type { FieldPlugin } from "@riducms/plugin";
import type { SchemaField } from "@riducms/protocol";

export class FieldRegistry {
	#plugins = new Map<string, FieldPlugin>();

	register(plugin: FieldPlugin) {
		const identity = pluginIdentity(plugin);
		if (this.#plugins.has(identity))
			throw new Error(`Field plugin ${identity} is already registered`);
		this.#plugins.set(identity, plugin);
	}

	resolve(field: SchemaField) {
		const component = field.admin.component;
		const identity =
			component !== undefined
				? `${field.type}:${component.plugin}:${component.component}`
				: field.plugin === undefined
					? field.type
					: `${field.type}:${field.plugin.key}`;
		const plugin = this.#plugins.get(identity);
		if (plugin === undefined || !plugin.canRender(field)) {
			throw new Error(`No admin field plugin can render ${field.path} (${identity})`);
		}
		return plugin;
	}
}

function pluginIdentity(plugin: FieldPlugin) {
	if (plugin.componentKey !== undefined)
		return `${plugin.type}:${plugin.key ?? "<missing>"}:${plugin.componentKey}`;
	return plugin.key === undefined ? plugin.type : `${plugin.type}:${plugin.key}`;
}

export function createCoreFieldRegistry(plugins: readonly FieldPlugin[] = []): FieldRegistry {
	const registry = new FieldRegistry();
	registry.register({ type: "text", canRender: (field) => field.text !== undefined });
	registry.register({ type: "code", canRender: (field) => field.code !== undefined });
	registry.register({ type: "select", canRender: (field) => field.select !== undefined });
	registry.register({ type: "radio", canRender: (field) => field.select !== undefined });
	registry.register({ type: "point", canRender: (field) => field.point !== undefined });
	registry.register({ type: "ui", canRender: (field) => field.ui !== undefined });
	registry.register({ type: "join", canRender: (field) => field.join !== undefined });
	registry.register({ type: "virtual", canRender: (field) => field.virtual !== undefined });
	for (const type of ["textarea", "email", "date", "number", "checkbox", "json"] as const) {
		registry.register({ type, canRender: () => true });
	}
	for (const type of ["group", "array"] as const) {
		registry.register({ type, canRender: (field) => field.nested !== undefined });
	}
	registry.register({ type: "blocks", canRender: (field) => field.blocks !== undefined });
	registry.register({
		type: "relationship",
		canRender: (field) => field.relationship !== undefined,
	});
	registry.register({ type: "upload", canRender: (field) => field.upload !== undefined });
	for (const plugin of plugins) registry.register(plugin);
	return registry;
}
