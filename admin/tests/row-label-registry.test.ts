import { describe, expect, it } from "bun:test";
import { defineRowLabelPlugin, type RowLabelComponentProps } from "@riducms/plugin";
import type { SchemaField } from "@riducms/protocol";
import type { Component } from "svelte";

import {
	createRowLabelRegistry,
	immutableRowLabelSnapshot,
} from "../src/core/plugins/row-label-registry";

const component = (() => undefined) as unknown as Component<RowLabelComponentProps>;
const registration = defineRowLabelPlugin({
	key: "curriculum",
	componentKey: "compositeSummary",
	component,
});

function repeatedField(type: "array" | "blocks", plugin = "curriculum", key = "compositeSummary") {
	return {
		id: `questions-${type}`,
		name: type,
		path: `content.${type}`,
		type,
		category: "nested",
		required: false,
		unique: false,
		admin: { label: type },
		nested: {
			fields: [],
			rowLabelComponent: { plugin, component: key, config: { key: "optionKey" } },
		},
		...(type === "blocks" ? { blocks: { types: [] } } : {}),
	} satisfies SchemaField;
}

describe("row label registry", () => {
	it("resolves exact plugin/component identities for arrays and blocks", () => {
		const registry = createRowLabelRegistry([registration]);
		expect(registry.resolve(repeatedField("array"))).toEqual({
			component: registration.component,
			config: { key: "optionKey" },
		});
		expect(registry.resolve(repeatedField("blocks"))).toEqual({
			component: registration.component,
			config: { key: "optionKey" },
		});
		expect(
			registry.resolve({
				...repeatedField("array"),
				nested: { fields: [] },
			})
		).toBeUndefined();
	});

	it("reports the field, plugin, and component for a missing or mismatched registration", () => {
		const registry = createRowLabelRegistry([registration]);
		expect(() => registry.resolve(repeatedField("array", "other", "compositeSummary"))).toThrow(
			'No admin row label component can render field content.array (plugin "other", component "compositeSummary")'
		);
		expect(() => registry.resolve(repeatedField("blocks", "curriculum", "missing"))).toThrow(
			'No admin row label component can render field content.blocks (plugin "curriculum", component "missing")'
		);
	});

	it("rejects duplicate identities and provides detached deeply frozen row snapshots", () => {
		expect(() => createRowLabelRegistry([registration, { ...registration }])).toThrow(
			"Admin row label component curriculum:compositeSummary is already registered"
		);

		const source = {
			optionKey: "A",
			nested: { label: "Alpha" },
			tags: ["one", { label: "Two" }],
		};
		const snapshot = immutableRowLabelSnapshot(source);
		source.optionKey = "mutated";
		source.nested.label = "mutated";
		source.tags[0] = "mutated";
		expect(snapshot).toEqual({
			optionKey: "A",
			nested: { label: "Alpha" },
			tags: ["one", { label: "Two" }],
		});
		expect(Object.isFrozen(snapshot)).toBe(true);
		expect(Object.isFrozen(snapshot.nested)).toBe(true);
		expect(Object.isFrozen(snapshot.tags)).toBe(true);
		expect(() => {
			(snapshot as Record<string, unknown>).optionKey = "forbidden";
		}).toThrow();
	});
});
