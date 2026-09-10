import { describe, expect, it } from "vitest";
import type { AccessCapabilitiesEnvelope, SchemaField } from "@riducms/protocol";

import { normalizeSlug, slugFollowsSource } from "@admin/fields/text/slug";

const { FormController } = await import("@admin/core/forms/form-controller.svelte");

describe("form controller field access", () => {
	it("preserves mixed occurrence permissions and unsaved edits through reorder", async () => {
		const form = new FormController({
			layout: [
				{ _key: "allowed", blockType: "quote", secret: "Original" },
				{ _key: "protected", blockType: "quote", secret: "Protected" },
			],
		});
		const access = accessEnvelope();
		access.fields["layout.1.secret"] = { read: true, create: false, update: false };
		form.setAccess(access, "update");
		form.set("layout.0.secret", "Edited");
		const rows = form.snapshot().layout as Record<string, unknown>[];
		form.setRows("layout", [rows[1]!, rows[0]!]);
		expect(form.canWrite("layout.1.secret", "layout.quote.secret")).toBe(true);
		expect(form.canWrite("layout.0.secret", "layout.quote.secret")).toBe(false);
		expect(form.canRead("layout.0.secret", "layout.quote.secret")).toBe(true);
		const submitted = await form.submit([blocksField()], true, async (values) => values);
		expect(submitted).toEqual({
			layout: [
				{ _key: "protected", blockType: "quote" },
				{ _key: "allowed", blockType: "quote", secret: "Edited" },
			],
		});
	});

	it("moves nested capabilities with retained rows without granting them to insertions or replacements", () => {
		const form = new FormController({
			layout: [
				{ _key: "allowed", blockType: "quote", links: [{ _key: "first" }, { _key: "second" }] },
				{ _key: "removed", blockType: "quote" },
			],
		});
		const access = accessEnvelope();
		const allowed = { read: true, create: true, update: true };
		const denied = { read: false, create: false, update: false };
		access.fields["layout.0.links.0.label"] = allowed;
		access.fields["layout.0.links.1.label"] = denied;
		access.fields["layout.1.secret"] = allowed;
		form.setAccess(access, "update");
		const retained = (form.snapshot().layout as Record<string, unknown>[])[0]!;
		form.setRows("layout", [{ _key: "new", blockType: "quote" }, retained]);
		expect(form.access?.fields["layout.0.secret"]).toBeUndefined();
		expect(form.canWrite("layout.0.secret", "layout.quote.secret")).toBe(false);
		expect(form.access?.fields["layout.1.links.0.label"]).toEqual(allowed);
		expect(form.access?.fields["layout.1.links.1.label"]).toEqual(denied);
		form.setRows("layout.1.links", [{ _key: "second" }, { _key: "first" }, { _key: "copy" }]);
		expect(form.access?.fields["layout.1.links.0.label"]).toEqual(denied);
		expect(form.access?.fields["layout.1.links.1.label"]).toEqual(allowed);
		expect(form.access?.fields["layout.1.links.2.label"]).toBeUndefined();
		form.setRows("layout", [{ _key: "allowed", blockType: "other" }]);
		expect(form.access?.fields["layout.0.secret"]).toBeUndefined();
		expect(form.access?.fields["layout.1.secret"]).toBeUndefined();
		expect(form.access?.fields["layout.quote.secret"]).toEqual(denied);
		expect(form.access?.fields.title).toEqual(allowed);
	});

	it("does not transfer indexed permissions through ambiguous keys", () => {
		for (const rows of [
			[{}, {}],
			[{ _key: "duplicate" }, { _key: "duplicate" }],
		]) {
			const form = new FormController({ layout: rows });
			form.setAccess(accessEnvelope(), "update");
			form.setRows("layout", [...rows].reverse());
			expect(form.access?.fields["layout.0.secret"]).toBeUndefined();
		}
	});

	it("does not submit changed protected values or treat a new row as an unchanged occurrence", async () => {
		const form = new FormController({
			title: "Read only title",
			layout: [{ _key: "protected", blockType: "quote", secret: "Protected" }],
		});
		const denied = { read: true, create: false, update: false };
		const access = accessEnvelope();
		access.fields["layout.0.secret"] = denied;
		access.fields["layout.quote.secret"] = denied;
		form.setAccess(access, "update");
		form.set("layout.0.secret", "Forged edit");
		let submitted = await form.submit([blocksField()], false, async (values) => values);
		expect(submitted.layout).toEqual([{ _key: "protected", blockType: "quote" }]);
		form.setRows("layout", [{ _key: "new", blockType: "quote", secret: "Forged edit" }]);
		submitted = await form.submit([blocksField()], false, async (values) => values);
		expect(submitted.layout).toEqual([{ _key: "new", blockType: "quote" }]);
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
					slug: "quote",
					labels: { singular: "Quote", plural: "Quotes" },
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

describe("missing block schema recovery", () => {
	it("preserves raw removed rows through schema reload and prevents saving their deletion", async () => {
		const previous = blocksField();
		const next = { ...previous, blocks: { types: [] } };
		const values = {
			layout: [
				{ _key: "keep", blockType: "quote", secret: "Stored content", extra: { retained: true } },
			],
		};
		const form = new FormController(values);
		form.reconcile([previous], [next]);
		expect(form.snapshot()).toEqual(values);
		expect(form.original).toEqual(values);
		form.set("layout", []);
		let called = false;
		await expect(
			form.submit([next], false, async () => {
				called = true;
			})
		).rejects.toThrow();
		expect(called).toBe(false);
		expect(form.issues[0]?.code).toBe("unknown_block_schema");
	});
});

it("keeps overlapping field registrations owned by their mounted consumers during reorder", () => {
	const form = new FormController();
	const first = form.register("layout.0.heading");
	const second = form.register("layout.1.heading");
	first();
	const movedFirst = form.register("layout.1.heading");
	second();
	const movedSecond = form.register("layout.0.heading");
	expect(form.isRegistered("layout.0.heading")).toBe(true);
	expect(form.isRegistered("layout.1.heading")).toBe(true);
	movedFirst();
	movedSecond();
	expect(form.isRegistered("layout.0.heading")).toBe(false);
	expect(form.isRegistered("layout.1.heading")).toBe(false);
});
