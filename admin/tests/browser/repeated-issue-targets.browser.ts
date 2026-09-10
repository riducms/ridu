import { expect, it } from "vitest";
import { RiduError } from "@riducms/sdk";
import type { SchemaField } from "@riducms/protocol";
import { FormController } from "@admin/core/forms/form-controller.svelte";
import { indexFieldValues } from "@admin/core/forms/form-issue-correlation";
import { draftFixture, title } from "./draft-fixture";

const links: SchemaField = {
	...title,
	id: "links",
	name: "links",
	path: "links",
	type: "array",
	category: "nested",
	required: false,
	admin: { label: "Links" },
	nested: {
		fields: [{ ...title, id: "url", name: "url", path: "links.url", required: false }],
	},
};

type Row = Record<string, unknown> & { _key: string; links: Record<string, unknown>[] };

function fixture(kind: "array" | "blocks" | "embedded") {
	const original: Row[] = [
		{
			_key: "a",
			blockType: "card",
			links: [
				{ _key: "link-a", url: "invalid" },
				{ _key: "link-b", url: "valid" },
			],
		},
		{ _key: "b", blockType: "card", links: [] },
	];
	if (kind === "embedded") {
		const embedded = draftFixture([links]);
		const write = (rows: Row[]) =>
			embedded.form.setEmbedded(embedded.schema, {
				outline: rows.map((row) => ({
					kind: "widget",
					content: { schema: "card", uid: row._key, links: row.links },
				})),
			});
		write(original);
		return {
			form: embedded.form,
			field: embedded.schema,
			original,
			write,
			path: (outer: number, inner: number) => `body.outline.${outer}.content.links.${inner}.url`,
			destroy: () => embedded.binding.destroy(),
		};
	}
	const field: SchemaField = {
		...links,
		id: "sections",
		name: "sections",
		path: "sections",
		type: kind,
		...(kind === "array"
			? { nested: { fields: [links] } }
			: {
					blocks: {
						types: [
							{ slug: "card", labels: { singular: "Card", plural: "Cards" }, fields: [links] },
							{ slug: "note", labels: { singular: "Note", plural: "Notes" }, fields: [links] },
						],
					},
					nested: { fields: [] },
				}),
	};
	const form = new FormController();
	form.reset({ sections: original }, [field]);
	return {
		form,
		field,
		original,
		write: (rows: Row[]) => form.setRows("sections", rows),
		path: (outer: number, inner: number) => `sections.${outer}.links.${inner}.url`,
		destroy: () => {},
	};
}

for (const kind of ["array", "blocks", "embedded"] as const)
	for (const transition of [
		"nested and ancestor reorder",
		"nested deletion and same-key reinsertion",
		"ancestor deletion and same-key reinsertion",
		"locale change and return",
		...(kind === "blocks" ? (["Block case change and return"] as const) : []),
	] as const)
		it(`pending aggregate descendant issue follows ${kind} lifetime after ${transition}`, async () => {
			const { form, field, original, write, path, destroy } = fixture(kind);
			form.setLocalization("en");
			const target = indexFieldValues([field], form.snapshot()).find(
				(location) => location.path === path(0, 0)
			)!.token;
			let reject!: (error: unknown) => void;
			const request = form
				.submit(
					[field],
					false,
					() =>
						new Promise((_resolve, rejectRequest) => {
							reject = rejectRequest;
						})
				)
				.catch((error) => error);
			const next = structuredClone(original);
			if (transition === "nested and ancestor reorder") {
				next[0]!.links.reverse();
				write(next.toReversed());
			} else if (transition === "nested deletion and same-key reinsertion") {
				next[0]!.links = [];
				write(next);
				write(structuredClone(original));
			} else if (transition === "ancestor deletion and same-key reinsertion") {
				write([next[1]!]);
				write(structuredClone(original));
			} else if (transition === "Block case change and return") {
				next[0]!.blockType = "note";
				write(next);
				write(structuredClone(original));
			} else {
				form.setLocalization("fr");
				form.setLocalization("en");
			}
			reject(
				new RiduError({
					status: 422,
					code: "validation",
					message: "Invalid links",
					issues: [{ path: path(0, 0), target, code: "url", message: "Choose a different URL" }],
				})
			);
			expect(await request).toBeInstanceOf(RiduError);
			expect(form.submitting).toBe(false);
			expect(form.issues.map((issue) => issue.path)).toEqual(
				transition === "nested and ancestor reorder" ? [path(1, 1)] : []
			);
			destroy();
		});
