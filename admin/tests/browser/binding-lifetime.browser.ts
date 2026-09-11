import { expect, test } from "vitest";
import type { SchemaField } from "@riducms/protocol";
import { FormController } from "@admin/core/forms/form-controller.svelte";
import { FieldEditorBinding } from "@admin/core/forms/field-editor-binding";
import { PluginFieldBinding } from "@admin/core/forms/plugin-field-binding";

for (const kind of ["local", "plugin"] as const) {
	test(`${kind} destruction permanently releases registration without evaluating destroyed getters`, () => {
		const schema: SchemaField = {
			id: "title",
			name: "title",
			path: "title",
			type: "text",
			category: "scalar",
			required: false,
			unique: false,
			admin: { label: "Title" },
		};
		const form = new FormController();
		form.reset({ title: "Before" }, [schema]);
		let destroyed = false;
		const getSchema = () => {
			if (destroyed) throw new Error("destroyed component schema");
			return schema;
		};
		const version = () => {
			if (destroyed) throw new Error("destroyed component version");
			return 1;
		};
		const binding =
			kind === "local"
				? new FieldEditorBinding(form, getSchema, "text", version)
				: new PluginFieldBinding(form, getSchema, { decodeValue: (value) => value }, version);
		expect(form.isRegistered("title")).toBe(true);
		binding.destroy();
		destroyed = true;
		binding.destroy();
		form.set("title", "After");
		expect(binding.stale).toBe(true);
		expect(form.isRegistered("title")).toBe(false);
		if (kind === "local") {
			expect(binding.value).toBeUndefined();
			expect(binding.schema.admin.label).toBe("Title");
		} else {
			expect(() => binding.value).toThrow("stale");
			expect(() => binding.schema).toThrow("stale");
		}
		expect(() => binding.set("late")).toThrow("stale");
		expect(form.get("title")).toBe("After");
	});
}
