import { describe, expect, test } from "bun:test";

import { resolveBlockTypes, type SchemaCollection, type SchemaField } from "@riducms/protocol";

import { cloneSchemaCollections } from "@admin/core/schema/schema-clone";

import { bindBlockFields, blockDefinition } from "./block-manifest";

describe("schema clones for extensions", () => {
	test("detached collections keep their block selections", () => {
		const title = {
			name: "title",
			path: "title",
			id: "title",
			type: "text",
			admin: { label: "Title" },
		} as SchemaField;
		const layout = {
			name: "layout",
			path: "layout",
			id: "posts-layout",
			type: "blocks",
			category: "nested",
			admin: { label: "Layout" },
			blocks: { blockReferences: ["callout"] },
		} as SchemaField;
		const fields = bindBlockFields(
			[blockDefinition("callout", [title], { singular: "Callout", plural: "Callouts" })],
			[layout],
			"posts"
		);
		const collection = {
			id: "posts",
			slug: "posts",
			fields: [...fields],
			labels: { singular: "Post", plural: "Posts" },
			admin: {},
			capabilities: { auth: false, upload: false, versions: false, trash: false, locking: false },
		} as SchemaCollection;
		const [clone] = cloneSchemaCollections([collection]);
		const [block] = resolveBlockTypes(clone!.fields[0]!.blocks);
		expect(block?.slug).toBe("callout");
		expect(block?.fields[0]).toMatchObject({
			path: "layout.callout.title",
			id: "posts-layout-callout-title",
		});
		expect(clone!.fields[0]).not.toBe(fields[0]);
		expect(clone!.labels).not.toBe(collection.labels);
	});
});
