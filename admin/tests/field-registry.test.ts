import { resolveAdminConfig } from "@riducms/plugin/admin";
import { describe, expect, it } from "bun:test";
import {
	defineAdminPlugin,
	definePluginField,
	defineFieldComponent,
} from "@riducms/plugin/authoring/v1";
import type { SchemaField } from "@riducms/protocol";
import type { Component } from "svelte";

import { createCoreFieldRegistry } from "../src/core/plugins/field-registry";

describe("core field registry", () => {
	it("resolves upload references with the built-in renderer", () => {
		const field: SchemaField = {
			id: "blog-hero",
			name: "heroImage",
			path: "heroImage",
			type: "upload",
			category: "upload",
			required: false,
			unique: false,
			admin: { label: "Hero image" },
			upload: {
				collectionId: "media",
				collectionSlug: "media",
				onDelete: "nullify",
			},
		};

		expect(createCoreFieldRegistry().resolve(field).type).toBe("upload");
	});

	it("registers output-only join and virtual renderers", () => {
		const registry = createCoreFieldRegistry();
		const base = {
			category: "presentation" as const,
			required: false,
			unique: false,
			admin: { label: "Output" },
		};
		const join: SchemaField = {
			...base,
			id: "categories-posts",
			name: "posts",
			path: "posts",
			type: "join",
			join: { collectionId: "posts", collectionSlug: "posts", on: "category", limit: 10 },
		};
		const virtual: SchemaField = {
			...base,
			id: "categories-label",
			name: "label",
			path: "label",
			type: "virtual",
			virtual: { valueType: "string" },
		};

		expect(registry.resolve(join).type).toBe("join");
		expect(registry.resolve(virtual).type).toBe("virtual");
	});

	it("resolves a custom plugin field to its statically registered component", () => {
		const component = (() => undefined) as unknown as Component<
			import("@riducms/plugin").PluginFieldProps<string>
		>;
		const plugin = defineAdminPlugin({
			key: "palette",
			pairingVersion: 1,
			fields: {
				color: definePluginField({
					component,
					decodeValue: (value: unknown): string => String(value),
				}),
			},
		});
		const field: SchemaField = {
			id: "brand-accent",
			name: "accent",
			path: "accent",
			type: "plugin",
			category: "plugin",
			required: false,
			unique: false,
			admin: { label: "Accent" },
			plugin: { key: "color", config: { palette: ["#663399"] } },
		};

		expect(
			createCoreFieldRegistry(resolveAdminConfig({ plugins: [plugin] }).fields).resolve(field)
				.extension?.registration.component
		).toBe(component);
		expect(() => createCoreFieldRegistry().resolve(field)).toThrow(
			"No admin field renderer can render accent (plugin:color)"
		);
	});

	it("resolves exact plugin renderers without replacing built-in storage semantics", () => {
		const overview = (() => undefined) as unknown as Component<
			import("@riducms/plugin").PluginFieldProps<undefined, undefined, "ui">
		>;
		const preview = (() => undefined) as unknown as Component<
			import("@riducms/plugin").PluginFieldProps<undefined, undefined, "ui">
		>;
		const registry = createCoreFieldRegistry(
			resolveAdminConfig({
				plugins: [
					defineAdminPlugin({
						key: "seo",
						pairingVersion: 1,
						components: {
							overview: defineFieldComponent({
								type: "ui",
								component: overview,
								decodeValue: (): undefined => undefined,
							}),
							preview: defineFieldComponent({
								type: "ui",
								component: preview,
								decodeValue: (): undefined => undefined,
							}),
						},
					}),
				],
			}).fields
		);
		const base: SchemaField = {
			id: "posts-meta-overview",
			name: "overview",
			path: "meta.overview",
			type: "ui",
			category: "presentation",
			required: false,
			unique: false,
			admin: { label: "Overview", component: { plugin: "seo", component: "overview" } },
			ui: {},
		};

		expect(registry.resolve(base).extension?.registration.component).toBe(overview);
		expect(
			registry.resolve({
				...base,
				name: "preview",
				path: "meta.preview",
				admin: { label: "Preview", component: { plugin: "seo", component: "preview" } },
			}).extension?.registration.component
		).toBe(preview);
		expect(() => createCoreFieldRegistry().resolve(base)).toThrow("ui:seo:overview");
	});
});
