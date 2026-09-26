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
