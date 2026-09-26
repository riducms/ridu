import { expect, it } from "vitest";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import type { SchemaField, SchemaFieldTabGroup } from "@riducms/protocol";

import { createAdminClient } from "@admin/core/api/admin-client";
import { FormController } from "@admin/core/forms/form-controller.svelte";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

const Harness = svelte`
	<script>
		import { setAdminI18n } from "@riducms/plugin";
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import FieldTabLayout from "../../src/fields/field-tab-layout.svelte";

		let { fields, form, runtime, tabGroup } = $props();
		setAdminRuntime(runtime);
		setAdminI18n(runtime.i18n);
	</script>

	<button onclick={() => form.issues = [{ code: "invalid", path: "summary", message: "Required" }]}>Show issue</button>
	<button onclick={() => form.issues = []}>Clear issues</button>
	<FieldTabLayout {fields} {form} {tabGroup} />
`;

const tabGroup: SchemaFieldTabGroup = { id: "content-tabs" };
const fields: SchemaField[] = [
	{
		id: "title",
		name: "title",
		path: "title",
		type: "text",
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: "Title", tab: "Main", tabGroup },
	},
	{
		id: "summary",
		name: "summary",
		path: "summary",
		type: "text",
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: "Summary", tab: "Details", tabGroup },
	},
];

it("reveals the same invalid tab again after its issues clear", async () => {
	const runtime = new AdminRuntime(createAdminClient());
	const form = new FormController({}, runtime.i18n);
	form.reset({ title: "", summary: "" }, fields);
	const screen = await render(Harness, { fields, form, runtime, tabGroup });
	const main = screen.getByRole("tab", { name: "Main" });
	const details = screen.getByRole("tab", { name: /Details/ });

	await expect.element(main).toHaveAttribute("aria-selected", "true");
	await screen.getByRole("button", { name: "Show issue" }).click();
	await expect.element(details).toHaveAttribute("aria-selected", "true");

	await screen.getByRole("button", { name: "Clear issues" }).click();
	await main.click();
	await expect.element(main).toHaveAttribute("aria-selected", "true");
	await screen.getByRole("button", { name: "Show issue" }).click();
	await expect.element(details).toHaveAttribute("aria-selected", "true");

	await screen.unmount();
});
