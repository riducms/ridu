import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import type { SchemaField } from "@riducms/protocol";
import { createAdminI18n } from "@riducms/translations";
import { expect, it } from "vitest";
import { render } from "vitest-browser-svelte";

import { ListFilterFields } from "@admin/features/collections/list-filter-fields";
import { listMetadataFields } from "@admin/features/collections/list-workspace";

const Harness = svelte`
	<script>
		import { setAdminI18n } from "@riducms/plugin";
		import { createAdminI18n } from "@riducms/translations";
		import FilterBuilder from "../../src/features/collections/controls/filter-builder.svelte";
		let { fields } = $props();
		setAdminI18n(createAdminI18n());
		let ready = $state(true);
		let filters = $state([[{ field: "title", operator: "like", value: "launch" }]]);
		let pending = [];
		function changeFilters(next) {
			pending = next;
			ready = false;
		}
	</script>
	<button onclick={() => { filters = pending; ready = true; }}>Finish navigation</button>
	<div inert={!ready}>
		<FilterBuilder {fields} {filters} label="Pages" onChange={changeFilters} navigationIdle={ready} />
	</div>
`;

function titleFields() {
	const i18n = createAdminI18n();
	const title = { name: "title", path: "title", type: "text", admin: { label: "Title" } };
	return new ListFilterFields({
		fields: [title as SchemaField],
		metadata: listMetadataFields(undefined, i18n),
		i18n,
	});
}

it("restores the filter value focus and selection after inert navigation", async () => {
	const screen = await render(Harness, { fields: titleFields() });
	try {
		const value = screen.getByRole("textbox", { name: "Filter value", exact: true });
		await value.fill("launched");
		const input = value.element() as HTMLInputElement;
		input.setSelectionRange(2, 4);
		// The debounced commit navigates, which makes the controls inert until it finishes.
		await expect.element(value).not.toHaveFocus();
		await screen.getByRole("button", { name: "Finish navigation" }).click();
		await expect.element(value).toHaveFocus();
		expect([input.selectionStart, input.selectionEnd]).toEqual([2, 4]);
	} finally {
		await screen.unmount();
	}
});
