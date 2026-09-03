import type { RowLabelPlugin, RowLabelSnapshot, RowLabelValue } from "@riducms/plugin";
import type { SchemaField } from "@riducms/protocol";

export class RowLabelRegistry {
	#plugins = new Map<string, RowLabelPlugin>();

	register(plugin: RowLabelPlugin) {
		const identity = rowLabelIdentity(plugin.key, plugin.componentKey);
		if (this.#plugins.has(identity)) {
			throw new Error(`Admin row label component ${identity} is already registered`);
		}
		this.#plugins.set(identity, plugin);
	}

	resolve(field: SchemaField): RowLabelPlugin | undefined {
		const component = field.nested?.rowLabelComponent;
		if (component === undefined) return undefined;
		const identity = rowLabelIdentity(component.plugin, component.component);
		const plugin = this.#plugins.get(identity);
		if (plugin === undefined) {
			throw new Error(
				`No admin row label component can render field ${field.path} (plugin "${component.plugin}", component "${component.component}")`
			);
		}
		return plugin;
	}
}

export function createRowLabelRegistry(plugins: readonly RowLabelPlugin[] = []): RowLabelRegistry {
	const registry = new RowLabelRegistry();
	for (const plugin of plugins) registry.register(plugin);
	return registry;
}

export function immutableRowLabelSnapshot(row: Record<string, unknown>): RowLabelSnapshot {
	return cloneRowLabelObject(row);
}

function rowLabelIdentity(plugin: string, component: string) {
	return `${plugin}:${component}`;
}

function cloneRowLabelObject(value: Record<string, unknown>): RowLabelSnapshot {
	const cloned: Record<string, RowLabelValue> = {};
	for (const [key, child] of Object.entries(value)) cloned[key] = cloneRowLabelValue(child);
	return Object.freeze(cloned);
}

function cloneRowLabelValue(value: unknown): RowLabelValue {
	if (
		value === undefined ||
		value === null ||
		typeof value === "string" ||
		typeof value === "number" ||
		typeof value === "boolean"
	) {
		return value;
	}
	if (Array.isArray(value)) return Object.freeze(value.map(cloneRowLabelValue));
	if (typeof value === "object") return cloneRowLabelObject(value as Record<string, unknown>);
	return undefined;
}
