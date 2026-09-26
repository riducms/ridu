import { expect, it } from "vitest";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";

const Harness = svelte`
	<script>
		import { setAdminI18n } from "@riducms/plugin";
		import { createAdminI18n } from "@riducms/translations";
		import SortHeading from "../../src/features/collections/controls/sort-heading.svelte";
		setAdminI18n(createAdminI18n());
		let ready = $state(true);
		let sort = $state("");
		function sortColumn(path, descending) {
			sort = descending ? '-' + path : path;
			ready = false;
		}
	</script>
	<button onclick={() => ready = true}>Finish navigation</button>
	<div inert={!ready}>
		<table><thead><tr>
			<SortHeading column={{ path: "title", label: "Title" }} {sort} {ready} onSort={sortColumn} />
		</tr></thead></table>
	</div>
`;

it("restores the chosen sort direction after inert navigation and exposes it on the column header", async () => {
	const screen = await render(Harness);
	try {
		await screen.getByRole("button", { name: "Sort Title descending" }).click();
		await screen.getByRole("button", { name: "Finish navigation" }).click();
		await expect
			.element(screen.getByRole("button", { name: "Sort Title descending" }))
			.toHaveFocus();
		await expect
			.element(screen.getByRole("columnheader"))
			.toHaveAttribute("aria-sort", "descending");
		await screen.getByRole("button", { name: "Sort Title ascending" }).click();
		await screen.getByRole("button", { name: "Finish navigation" }).click();
		await expect
			.element(screen.getByRole("button", { name: "Sort Title ascending" }))
			.toHaveFocus();
		await expect
			.element(screen.getByRole("columnheader"))
			.toHaveAttribute("aria-sort", "ascending");
	} finally {
		await screen.unmount();
	}
});
