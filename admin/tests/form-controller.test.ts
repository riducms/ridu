import { describe, expect, it } from "bun:test";
import type { AccessCapabilitiesEnvelope, SchemaField } from "@riducms/protocol";

import { normalizeSlug, slugFollowsSource } from "../src/fields/text/slug";

Object.assign(globalThis, {
	$state: Object.assign(<Value>(value: Value) => value, {
		raw: <Value>(value?: Value) => value,
	}),
});

const { FormController } = await import("../src/core/forms/form-controller.svelte");

describe("form controller field access", () => {
	it("uses block-aware canonical access and drops stale indexed capabilities", () => {
		const form = new FormController();
		form.setAccess(accessEnvelope(), "create");

		expect(form.canWrite("layout.0.secret", "layout.quote.secret")).toBe(true);
		expect(form.canWrite("layout.2.secret", "layout.quote.secret")).toBe(false);

		form.invalidateIndexedFieldCapabilities("layout");

		expect(form.access?.fields["layout.0.secret"]).toBeUndefined();
		expect(form.access?.fields["layout.quote.secret"]).toBeDefined();
		expect(form.access?.fields.title).toBeDefined();
		expect(form.canWrite("layout.0.secret", "layout.quote.secret")).toBe(false);
	});

	it("uses canonical block paths while validating and serializing", async () => {
		const form = new FormController({
			layout: [{ _key: "quote-1", blockType: "quote", secret: "classified" }],
		});
		form.setAccess(accessEnvelope(), "create");
		form.invalidateIndexedFieldCapabilities("layout");

		const submitted = await form.submit([blocksField()], true, async (values) => values);

		expect(submitted).toEqual({
			layout: [{ _key: "quote-1", blockType: "quote" }],
		});
	});
});

describe("form controller derived text bindings", () => {
	it("keeps generated slugs following while the field has no mounted consumer", () => {
		const form = new FormController({ title: "Hello World", slug: "hello-world" });
		const binding = form.bindDerivedText(
			"slug",
			"title",
			(source) => normalizeSlug(String(source ?? "")),
			(current, source) => String(current ?? "") === "" || slugFollowsSource(current, source)
		);

		form.set("title", "Hidden Field Update");
		expect(form.get("slug")).toBe("hidden-field-update");

		binding.setManual("hand-authored");
		form.set("title", "Ignored Update");
		expect(form.get("slug")).toBe("hand-authored");

		binding.follow();
		form.set("title", "Following Again");
		expect(form.get("slug")).toBe("following-again");
	});

	it("clears keep-alive observers when the form resets", () => {
		const form = new FormController({ title: "One", slug: "one" });
		form.bindDerivedText(
			"slug",
			"title",
			(source) => normalizeSlug(String(source ?? "")),
			(current, source) => slugFollowsSource(current, source)
		);

		form.reset({ title: "Fresh", slug: "" });
		form.set("title", "No Stale Observer");
		expect(form.get("slug")).toBe("");
	});
});

function accessEnvelope(): AccessCapabilitiesEnvelope {
	return {
		operations: {
			admin: true,
			create: true,
			read: true,
			readVersions: true,
			update: true,
			delete: true,
			duplicate: true,
			publish: true,
			unpublish: true,
			restoreDeleted: true,
			deletePermanent: true,
			selectAll: true,
		},
		fields: {
			"layout.quote.secret": { read: false, create: false, update: false },
			"layout.0.secret": { read: true, create: true, update: true },
			title: { read: true, create: true, update: true },
		},
	};
}

function blocksField(): SchemaField {
	return {
		id: "layout",
		name: "layout",
		path: "layout",
		type: "blocks",
		category: "nested",
		required: false,
		unique: false,
		admin: { label: "Layout" },
		blocks: {
			types: [
				{
					key: "quote",
					label: "Quote",
					fields: [
						{
							id: "layout-quote-secret",
							name: "secret",
							path: "layout.quote.secret",
							type: "text",
							category: "scalar",
							required: true,
							unique: false,
							admin: { label: "Secret" },
							text: {},
						},
					],
				},
			],
		},
	};
}
