import { describe, expect, test } from "bun:test";
import type { SchemaCollection, SchemaField } from "@riducms/protocol";

import { documentTitleField, documentLabel } from "../src/features/documents/document-title";

describe("document title field", () => {
	test("uses the collection admin useAsTitle field before earlier text fields", () => {
		const collection = fixtureCollection("lele");

		expect(documentTitleField(collection, () => true)?.name).toBe("lele");
	});

	test("falls back to the first readable title-compatible field when useAsTitle is unavailable", () => {
		const collection = fixtureCollection("lele");

		expect(documentTitleField(collection, (path) => path !== "lele")?.name).toBe("title");
		expect(documentTitleField(fixtureCollection(), () => true)?.name).toBe("title");
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

test("labels honor the configured field and fall back to IDs for redacted or empty titles", () => {
	const collection = fixtureCollection("lele");
	expect(
		documentLabel(collection, { id: "one", title: "Earlier field", lele: "Chosen title" })
	).toBe("Chosen title");
	for (const value of [undefined, null, ""]) {
		expect(documentLabel(collection, { id: "one", title: "Earlier field", lele: value })).toBe(
			"one"
		);
	}
});

test("uploads fall back to filenames and meaningful scalar values remain visible", () => {
	const collection = fixtureCollection();
	collection.capabilities.upload = true;
	collection.fields = [textField("filename")];
	expect(documentTitleField(collection)?.name).toBe("filename");
	expect(documentLabel(collection, { id: "one", filename: "photo.png", lele: "Caption" })).toBe(
		"photo.png"
	);
	collection.capabilities.upload = false;
	collection.admin.useAsTitle = "lele";
	collection.fields = fixtureCollection().fields;
	expect(documentLabel(collection, { id: "one", lele: 0 })).toBe("0");
	expect(documentLabel(collection, { id: "one", lele: false })).toBe("false");
});

test("unconfigured collections prefer named titles and support email-only labels", () => {
	const collection = fixtureCollection();
	collection.fields.unshift(textField("summary"));
	expect(documentTitleField(collection)?.name).toBe("title");
	collection.fields = [{ ...textField("email"), type: "email" }];
	expect(documentLabel(collection, { id: "one", email: "editor@example.test" })).toBe(
		"editor@example.test"
	);
	expect(documentTitleField(collection, () => false)).toBeUndefined();
});
