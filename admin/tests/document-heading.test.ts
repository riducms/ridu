import { describe, expect, test } from "bun:test";
import type { SchemaCollection, SchemaField } from "@riducms/protocol";

import { documentHeadingField } from "../src/features/documents/document-heading";

describe("document heading field", () => {
	test("uses the collection admin useAsTitle field before earlier text fields", () => {
		const collection = fixtureCollection("lele");

		expect(documentHeadingField(collection, () => true)?.name).toBe("lele");
	});

	test("falls back to the first readable title-compatible field when useAsTitle is unavailable", () => {
		const collection = fixtureCollection("lele");

		expect(documentHeadingField(collection, (path) => path !== "lele")?.name).toBe("title");
		expect(documentHeadingField(fixtureCollection(), () => true)?.name).toBe("title");
	});
});

function fixtureCollection(useAsTitle?: string): SchemaCollection {
	return {
		id: "posts",
		slug: "posts",
		labels: { singular: "Post", plural: "Posts" },
		admin: useAsTitle === undefined ? {} : { useAsTitle },
		capabilities: { auth: false, upload: false, versions: false, trash: false, locking: false },
		fields: [textField("title"), textField("lele")],
	};
}

function textField(name: string): SchemaField {
	return {
		id: `posts-${name}`,
		name,
		path: name,
		type: "text",
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: name },
		text: {},
	};
}
