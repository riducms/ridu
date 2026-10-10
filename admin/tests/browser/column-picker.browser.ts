import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import type { SchemaField } from "@riducms/protocol";
import { expect, it, vi } from "vitest";
import { userEvent } from "vitest/browser";
import { render } from "vitest-browser-svelte";

const Harness = svelte`
	<script>
		import { setAdminI18n } from "@riducms/plugin";
		import { createAdminI18n } from "@riducms/translations";
		import ColumnPicker from "../../src/features/collections/controls/column-picker.svelte";
		let { columns, availableColumns, onChange } = $props();
		setAdminI18n(createAdminI18n());
	</script>
	<ColumnPicker {columns} {availableColumns} canReadField={(path) => path !== "secret"} {onChange} />
`;

const NavigatingHarness = svelte`
	<script>
		import { setAdminI18n } from "@riducms/plugin";
		import { createAdminI18n } from "@riducms/translations";
		import ColumnPicker from "../../src/features/collections/controls/column-picker.svelte";
		setAdminI18n(createAdminI18n());
		const availableColumns = [{ path: "a", label: "A" }, { path: "b", label: "B" }];
		let ready = $state(true);
		let columns = $state([{ path: "a", active: true }, { path: "b", active: true }]);
		let pending = [];
		function changeColumns(next) {
			pending = next;
			ready = false;
		}
	</script>
	<button onclick={() => { columns = pending; ready = true; }}>Finish navigation</button>
	<div inert={!ready}>
		<ColumnPicker {columns} {availableColumns} canReadField={() => true} onChange={changeColumns} navigationIdle={ready} />
	</div>
`;

it("keeps unreadable columns in the selection when readable columns are reordered", async () => {
	const secret = {
		id: "secret",
		name: "secret",
		path: "secret",
		type: "text",
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: "Secret" },
	} as SchemaField;
	const onChange = vi.fn();
	const screen = await render(Harness, {
		columns: [
			{ path: "a", active: true },
			{ path: "secret", active: false },
			{ path: "b", active: true },
		],
		availableColumns: [
			{ path: "a", label: "A" },
			{ path: "secret", label: "Secret", field: secret },
			{ path: "b", label: "B" },
		],
		onChange,
	});
	try {
		const handle = screen.getByRole("button", { name: "Reorder A column" });
		handle.element().focus();
		await userEvent.keyboard("{Space}{ArrowRight}{Space}");
		await expect.poll(() => onChange.mock.calls.length).toBe(1);
		expect(onChange).toHaveBeenCalledWith([
			{ path: "b", active: true },
			{ path: "secret", active: false },
			{ path: "a", active: true },
		]);
	} finally {
		await screen.unmount();
	}
});

it("restores focus to a toggled column after inert navigation", async () => {
	const screen = await render(NavigatingHarness);
	try {
		const toggle = screen.getByRole("button", { name: "A", exact: true });
		await toggle.click();
		await expect.element(toggle).not.toHaveFocus();
		await screen.getByRole("button", { name: "Finish navigation" }).click();
		await expect.element(toggle).toHaveFocus();
		await expect.element(toggle).toHaveAttribute("aria-pressed", "false");
	} finally {
		await screen.unmount();
	}
});

it("restores focus to a keyboard-reordered column handle after inert navigation", async () => {
	const screen = await render(NavigatingHarness);
	try {
		const handle = screen.getByRole("button", { name: "Reorder A column" });
		handle.element().focus();
		await userEvent.keyboard("{Space}{ArrowRight}{Space}");
		await expect.element(handle).not.toHaveFocus();
		await screen.getByRole("button", { name: "Finish navigation" }).click();
		await expect.element(handle).toHaveFocus();
		expect(
			screen
				.getByRole("button", { name: /^Reorder . column$/ })
				.elements()
				.map((element) => element.getAttribute("aria-label"))
		).toEqual(["Reorder B column", "Reorder A column"]);
	} finally {
		await screen.unmount();
	}
});
