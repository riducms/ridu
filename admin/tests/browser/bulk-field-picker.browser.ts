import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import type { SchemaField } from "@riducms/protocol";
import { expect, it, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { render } from "vitest-browser-svelte";

import type { AdminClient } from "@admin/core/api/admin-client";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

const Harness = svelte`
 <script>
  import { setAdminI18n } from "@riducms/plugin";
  import FieldPicker from "../../src/features/bulk-edit/field-picker.svelte";

  let { i18n, fields, disabled = false, changed } = $props();
  let value = $state([]);

  setAdminI18n(i18n);

  function select(next) {
   value = next;
   changed(next);
  }
 </script>

 <FieldPicker {fields} {value} {disabled} onValueChange={select} />
`;

const fields: SchemaField[] = ["Title", "Summary", "Featured"].map((label) => ({
	id: label.toLowerCase(),
	name: label.toLowerCase(),
	path: label.toLowerCase(),
	type: "text",
	category: "scalar",
	required: false,
	unique: false,
	admin: { label },
}));

it.each(["pointer", "keyboard"])(
	"picks multiple fields by %s without duplicating their labels in the input",
	async (method) => {
		const runtime = new AdminRuntime({} as AdminClient);
		const changed = vi.fn();
		const screen = await render(Harness, { i18n: runtime.i18n, fields, changed });
		const input = screen.getByRole("combobox", { name: "Select fields to edit", exact: true });

		await input.click();
		await expect.element(page.getByRole("option", { name: "Title", exact: true })).toBeVisible();

		if (method === "keyboard") {
			await input.fill("sum");
			await expect
				.element(page.getByRole("option", { name: "Title", exact: true }))
				.not.toBeInTheDocument();
			await expect.element(input).toHaveFocus();
			await userEvent.keyboard("{ArrowDown}");
			await expect
				.element(input)
				.toHaveAttribute(
					"aria-activedescendant",
					page.getByRole("option", { name: "Summary", exact: true }).element().id
				);
			await userEvent.keyboard("{Enter}");
		} else {
			await page.getByRole("option", { name: "Summary", exact: true }).click();
		}

		await expect.poll(() => changed.mock.lastCall?.[0]).toEqual(["summary"]);
		await expect.element(input).toHaveValue("");
		await expect.element(input).toHaveFocus();
		await expect.element(screen.getByRole("button", { name: "Remove Summary" })).toBeVisible();

		await input.click();
		await expect
			.element(page.getByRole("option", { name: "Summary", exact: true }))
			.not.toBeInTheDocument();
		await page.getByRole("option", { name: "Featured", exact: true }).click();
		await expect.poll(() => changed.mock.lastCall?.[0]).toEqual(["summary", "featured"]);
		await expect.element(input).toHaveValue("");

		await screen.getByRole("button", { name: "Remove Summary" }).click();
		await expect.poll(() => changed.mock.lastCall?.[0]).toEqual(["featured"]);
		await expect.element(input).toHaveFocus();
		await input.click();
		await expect.element(page.getByRole("option", { name: "Summary", exact: true })).toBeVisible();
		await userEvent.keyboard("{Escape}");

		await screen.getByRole("button", { name: "Clear selection", exact: true }).click();
		await expect.poll(() => changed.mock.lastCall?.[0]).toEqual([]);
		await expect.element(input).toHaveFocus();
		await expect.element(input).toHaveAttribute("placeholder", "Select a value");
		await screen.unmount();
	}
);

it("prevents changes while disabled and exposes a searchable empty result", async () => {
	const runtime = new AdminRuntime({} as AdminClient);
	const changed = vi.fn();
	const props = { i18n: runtime.i18n, fields, changed };
	const screen = await render(Harness, props);
	const input = screen.getByRole("combobox", { name: "Select fields to edit", exact: true });

	await input.fill("absent");
	await expect.element(page.getByText("No fields found", { exact: true })).toBeVisible();
	await userEvent.keyboard("{Escape}");
	await input.click();
	await page.getByRole("option", { name: "Title", exact: true }).click();
	await screen.rerender({ ...props, disabled: true });

	await expect.element(input).toBeDisabled();
	await expect.element(screen.getByRole("button", { name: "Remove Title" })).toBeDisabled();
	await expect.element(screen.getByRole("button", { name: "Clear selection" })).toBeDisabled();
	expect(changed).toHaveBeenCalledTimes(1);
	await screen.unmount();
});
