import { describe, expect, it } from "bun:test";
import type { SchemaField } from "@riducms/protocol";

import { fieldAccessPath, scopeRepeatedRowField } from "../src/fields/nested/scoped-field";

describe("repeating-row field scope", () => {
	it("recursively scopes descendants without dropping nested metadata", () => {
		const source = groupField();
		const scoped = scopeRepeatedRowField(source, "sections.2", "row-2", true);

		expect(scoped.path).toBe("sections.2.settings");
		expect(scoped.id).toBe("settings-row-2");
		expect(scoped.admin.readOnly).toBe(true);
		expect(scoped.admin.row?.id).toBe("settings-row-row-2");
		expect(scoped.admin.collapsible?.id).toBe("settings-collapse-row-2");
		expect(scoped.admin.tabGroup?.id).toBe("settings-tabs-row-2");

		const mode = scoped.nested?.fields[0];
		const choices = scoped.nested?.fields[1];
		const title = choices?.nested?.fields[0];
		const content = scoped.nested?.fields[2];
		const caption = content?.blocks?.types[0]?.fields[0];
		expect(mode?.path).toBe("sections.2.settings.mode");
		expect(title?.path).toBe("sections.2.settings.choices.title");
		expect(caption?.path).toBe("sections.2.settings.content.caption");
		expect(fieldAccessPath(scoped)).toBe("sections.settings");
		expect(fieldAccessPath(title!)).toBe("sections.settings.choices.title");
		expect(fieldAccessPath(caption!)).toBe("sections.settings.content.hero.caption");
		expect(fieldAccessPath({ ...caption! })).toBe("sections.settings.content.hero.caption");
		expect(title?.admin.readOnly).toBe(true);
		expect(choices?.nested?.minRows).toBe(1);
		expect(choices?.nested?.rowLabel).toBe("title");
		expect(choices?.nested?.rowLabelComponent?.component).toBe("choiceSummary");
		expect(source.path).toBe("sections.settings");
		expect(source.nested?.fields[1]?.nested?.fields[0]?.path).toBe(
			"sections.settings.choices.title"
		);
	});

	it("keeps rendered identity stable while a reordered row receives a new data path", () => {
		const source = groupField();
		const before = scopeRepeatedRowField(source, "sections.2", "stable-row-key");
		const after = scopeRepeatedRowField(source, "sections.0", "stable-row-key");

		expect(after.id).toBe(before.id);
		expect(after.admin.row?.id).toBe(before.admin.row?.id);
		expect(after.admin.collapsible?.id).toBe(before.admin.collapsible?.id);
		expect(after.admin.tabGroup?.id).toBe(before.admin.tabGroup?.id);
		expect(after.path).toBe("sections.0.settings");
		expect(fieldAccessPath(after)).toBe(fieldAccessPath(before));
		expect(after.nested?.fields[1]?.nested?.fields[0]?.path).toBe(
			"sections.0.settings.choices.title"
		);
	});
});

function groupField(): SchemaField {
	return {
		id: "settings",
		name: "settings",
		path: "sections.settings",
		type: "group",
		category: "nested",
		required: false,
		unique: false,
		admin: {
			label: "Settings",
			row: { id: "settings-row" },
			collapsible: { id: "settings-collapse", label: "Settings" },
			tabGroup: { id: "settings-tabs" },
		},
		nested: {
			fields: [
				textField("mode", "sections.settings.mode"),
				{
					id: "choices",
					name: "choices",
					path: "sections.settings.choices",
					type: "array",
					category: "nested",
					required: false,
					unique: false,
					admin: { label: "Choices" },
					nested: {
						fields: [textField("title", "sections.settings.choices.title")],
						minRows: 1,
						rowLabel: "title",
						rowLabelComponent: {
							plugin: "curriculum",
							component: "choiceSummary",
							config: { prefix: "Choice" },
						},
					},
				},
				{
					id: "content",
					name: "content",
					path: "sections.settings.content",
					type: "blocks",
					category: "nested",
					required: false,
					unique: false,
					admin: { label: "Content" },
					nested: { fields: [] },
					blocks: {
						types: [
							{
								key: "hero",
								label: "Hero",
								fields: [textField("caption", "sections.settings.content.hero.caption")],
							},
						],
					},
				},
			],
		},
	};
}

function textField(name: string, path: string): SchemaField {
	return {
		id: name,
		name,
		path,
		type: "text",
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: name },
		text: {},
	};
}
