import { expect, it, vi } from "vitest";
import { userEvent } from "vitest/browser";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import type { SchemaField } from "@riducms/protocol";
import { createAdminClient } from "@admin/core/api/admin-client";
import { FormController, FormValidationError } from "@admin/core/forms/form-controller.svelte";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

const Harness = svelte`
 <script>
  import { setAdminI18n } from "@riducms/plugin";
  import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
  import FieldLayout from "../../src/fields/field-layout.svelte";
  let { form, fields, runtime } = $props();
  setAdminRuntime(runtime);
  setAdminI18n(runtime.i18n);
 </script>
 <FieldLayout {form} {fields} />
`;
const json: SchemaField = {
	id: "metadata",
	name: "metadata",
	path: "metadata",
	type: "json",
	category: "scalar",
	required: false,
	unique: false,
	admin: { label: "Metadata", tab: "Data", tabGroup: { id: "tabs" } },
};
const title: SchemaField = {
	...json,
	id: "title",
	name: "title",
	path: "title",
	type: "text",
	admin: { label: "Title", tab: "Main", tabGroup: { id: "tabs" } },
};

it("retains invalid JSON across tabs, blocks Save, and submits the corrected object", async () => {
	const runtime = new AdminRuntime(createAdminClient());
	const form = new FormController({}, runtime.i18n);
	const fields = [json, title];
	form.reset({ metadata: { original: true }, title: "Post" }, fields);
	const screen = await render(Harness, { runtime, form, fields });
	const editor = screen.getByRole("textbox", { name: "Metadata", exact: true });
	await editor.fill('{"unfinished":');
	expect(form.get("metadata")).toEqual({ original: true });
	expect(form.dirty).toBe(true);
	await screen.getByRole("tab", { name: "Main", exact: true }).click();
	const save = vi.fn(async (value) => value);
	await expect(form.submit(fields, false, save)).rejects.toBeInstanceOf(FormValidationError);
	expect(save).not.toHaveBeenCalled();
	await screen.getByRole("tab", { name: /^Data/ }).click();
	await expect.element(editor).toHaveTextContent('{"unfinished":');
	await editor.fill('{"finished": 2}');
	await expect.poll(() => form.get("metadata")).toEqual({ finished: 2 });
	await expect(form.submit(fields, false, save)).resolves.toMatchObject({
		metadata: { finished: 2 },
	});
	await screen.unmount();
});

it("external resets discard JSON drafts and their undo history", async () => {
	const runtime = new AdminRuntime(createAdminClient());
	const form = new FormController({}, runtime.i18n);
	form.reset({ metadata: { old: true } }, [json]);
	const screen = await render(Harness, { runtime, form, fields: [json] });
	const editor = screen.getByRole("textbox", { name: "Metadata", exact: true });
	await editor.fill('{"broken":');
	form.reset({ metadata: { fresh: true } }, [json]);
	await expect.poll(() => editor.element().textContent).toContain('"fresh": true');
	await editor.click();
	await userEvent.keyboard("{ControlOrMeta>}z{/ControlOrMeta}");
	expect(form.get("metadata")).toEqual({ fresh: true });
	expect(form.dirty).toBe(false);
	await screen.unmount();
});

it("invalid JSON survives closing and reopening a disclosure", async () => {
	const runtime = new AdminRuntime(createAdminClient());
	const form = new FormController({}, runtime.i18n);
	const field = {
		...json,
		admin: {
			label: "Metadata",
			collapsible: { id: "advanced", label: "Advanced", initiallyCollapsed: false },
		},
	};
	form.reset({ metadata: {} }, [field]);
	const screen = await render(Harness, { runtime, form, fields: [field] });
	const editor = screen.getByRole("textbox", { name: "Metadata", exact: true });
	await editor.fill('{"incomplete":');
	await screen.getByText("Advanced", { exact: true }).click();
	expect(form.pendingEditIssues()).toHaveLength(1);
	await screen.getByText("Advanced", { exact: true }).click();
	await expect.element(editor).toHaveTextContent('{"incomplete":');
	await screen.unmount();
});

it("starts a fresh undo history when the next document has identical text", async () => {
	const runtime = new AdminRuntime(createAdminClient());
	const form = new FormController({}, runtime.i18n);
	form.reset({ metadata: { old: true } }, [json]);
	const screen = await render(Harness, { runtime, form, fields: [json] });
	const editor = screen.getByRole("textbox", { name: "Metadata", exact: true });
	await editor.fill(JSON.stringify({ fresh: true }, null, 2));
	await expect.poll(() => form.get("metadata")).toEqual({ fresh: true });

	form.reset({ metadata: { fresh: true } }, [json]);
	await expect.poll(() => editor.element().textContent).toContain('"fresh": true');
	await editor.click();
	await userEvent.keyboard("{ControlOrMeta>}z{/ControlOrMeta}");
	expect(form.get("metadata")).toEqual({ fresh: true });
	expect(form.dirty).toBe(false);
	await screen.unmount();
});
