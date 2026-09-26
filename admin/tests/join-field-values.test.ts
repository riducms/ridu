import { expect, test } from "bun:test";
import type { SchemaField } from "@riducms/protocol";
import { sortJoinDocuments } from "@admin/fields/join/join-field-values";

function column(type: SchemaField["type"], format?: "date" | "date-time" | "time") {
	return {
		path: "value",
		label: "Value",
		field: { path: "value", type, date: { format } } as SchemaField,
	};
}

const referenceLabel = () => "Related";

for (const language of ["en-GB", "en-US", "fr", "ar"]) {
	test(`sorts dates chronologically independently of ${language} display formatting`, () => {
		const docs = [
			{ id: "jan", value: "2026-01-20" },
			{ id: "feb", value: "2026-02-01" },
			{ id: "absent" },
		];
		expect(
			sortJoinDocuments(docs, "value", [column("date", "date")], language, referenceLabel).map(
				(doc) => doc.id
			)
		).toEqual(["jan", "feb", "absent"]);
		expect(
			sortJoinDocuments(docs, "-value", [column("date", "date")], language, referenceLabel).map(
				(doc) => doc.id
			)
		).toEqual(["feb", "jan", "absent"]);
	});
}

test("compares timestamps as instants across timezone offsets", () => {
	const docs = [
		{ id: "later", value: "2026-01-20T08:00:00Z" },
		{ id: "earlier", value: "2026-01-20T10:00:00+03:00" },
	];
	expect(
		sortJoinDocuments(docs, "value", [column("date", "date-time")], "en", referenceLabel).map(
			(doc) => doc.id
		)
	).toEqual(["earlier", "later"]);
});

test("sorts negative and fractional numbers by numeric value", () => {
	const docs = [1.11, 1.2, -2, -10, 1000].map((value) => ({ id: String(value), value }));
	expect(
		sortJoinDocuments(docs, "value", [column("number")], "en", referenceLabel).map(
			(doc) => doc.value
		)
	).toEqual([-10, -2, 1.11, 1.2, 1000]);
});
