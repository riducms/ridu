import type { Component } from "svelte";
import type { RowLabelComponentProps } from "@riducms/plugin";
import type { AdminConfig } from "@riducms/plugin/admin";
import type { RowLabelPlugin, RowLabelSnapshot, RowLabelValue } from "@riducms/plugin";
import type { SchemaField } from "@riducms/protocol";

export class RowLabelRegistry {
	constructor(private readonly local: NonNullable<AdminConfig["rowLabels"]> = {}) {}
	#plugins = new Map<string, RowLabelPlugin>();

	register(plugin: RowLabelPlugin) {
		const identity = rowLabelIdentity(plugin.key, plugin.componentKey);
		if (this.#plugins.has(identity)) {
			throw new Error(`Admin row label component ${identity} is already registered`);
		}
		this.#plugins.set(identity, plugin);
	}

	resolve(
		field: SchemaField
	): { component: Component<RowLabelComponentProps>; config: unknown } | undefined {
		const component = field.nested?.rowLabelComponent;
		if (component === undefined) return undefined;
		if (component.reference !== undefined) {
			const local = this.local[component.reference as `app:${string}`];
			if (local === undefined)
				throw new Error(
					`No local row label ${component.reference} is registered for ${field.path}.`
				);
			return {
				component: local.component as Component<RowLabelComponentProps>,
				config: local.decode(field),
			};
		}
		const identity = rowLabelIdentity(component.plugin ?? "", component.component ?? "");
		const plugin = this.#plugins.get(identity);
		if (plugin === undefined) {
			throw new Error(
				`No admin row label component can render field ${field.path} (plugin "${component.plugin}", component "${component.component}")`
			);
		}
		return { component: plugin.component, config: component.config };
	}
}

export function createRowLabelRegistry(
	plugins: readonly RowLabelPlugin[] = [],
	local: AdminConfig["rowLabels"] = {}
): RowLabelRegistry {
	const registry = new RowLabelRegistry(local);
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
