import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import type { SchemaBlockType, SchemaField } from "@riducms/protocol";
import { createAdminI18n } from "@riducms/translations";
import { expect, it, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { render } from "vitest-browser-svelte";

import { ListFilterFields } from "@admin/features/collections/list-filter-fields";
import { listMetadataFields } from "@admin/features/collections/list-workspace";

import { bindBlockFields, blockDefinition } from "../block-manifest";

const Picker = svelte`
	<script>
		import { setAdminI18n } from "@riducms/plugin";
		import { createAdminI18n } from "@riducms/translations";
		import FilterFieldPicker from "../../src/features/collections/controls/filter-field-picker.svelte";
		let { fields, initial = "", changed } = $props();
		// svelte-ignore state_referenced_locally
		let value = $state(initial);
		setAdminI18n(createAdminI18n());
	</script>
	<button type="button">Before</button>
	<FilterFieldPicker {fields} {value} onValueChange={(path) => { value = path; changed(path); }} />
`;

const Builder = svelte`
	<script>
		import { setAdminI18n } from "@riducms/plugin";
		import { createAdminI18n } from "@riducms/translations";
		import FilterBuilder from "../../src/features/collections/controls/filter-builder.svelte";
		let { fields, filters, onChange } = $props();
		setAdminI18n(createAdminI18n());
	</script>
	<FilterBuilder {fields} {filters} label="Pages" {onChange} />
`;

function field(name: string, type: SchemaField["type"], extra: Partial<SchemaField> = {}) {
	const label = name.charAt(0).toUpperCase() + name.slice(1);
	return { name, path: name, type, admin: { label }, ...extra } as SchemaField;
}
const blocks = (name: string, slugs: string[]) =>
	field(name, "blocks", { blocks: { blockReferences: slugs } });

function pages() {
	const note = blockDefinition("note", [field("body", "textarea")], {
		singular: "Note",
		plural: "Notes",
	});
	const hero = blockDefinition(
		"hero",
		[
			field("heading", "text"),
			field("rank", "number"),
			field("author", "relationship", {
				relationship: { collectionSlug: "users" },
			} as Partial<SchemaField>),
			blocks("children", ["note"]),
		],
		{ singular: "Hero", plural: "Heroes" }
	);
	const fields = bindBlockFields([note, hero] satisfies SchemaBlockType[], [
		field("title", "text"),
		blocks("layout", ["hero"]),
	]);
	const i18n = createAdminI18n();
	return new ListFilterFields({ fields, metadata: listMetadataFields(undefined, i18n), i18n });
}

function trigger() {
	return page.getByRole("button", { name: "Filter field", exact: true });
}

function option(name: string | RegExp) {
	return page.getByRole("option", { name, exact: typeof name === "string" });
}

it("navigates from a blocks field through its block type to a nested field", async () => {
	const changed = vi.fn();
	const screen = await render(Picker, { fields: pages(), changed });
	try {
		await trigger().click();
		await expect.element(option("Title")).toBeVisible();
		await expect.element(option("Created At")).toBeVisible();
		await expect.element(option("Heading")).not.toBeInTheDocument();

		await option(/^Layout/).click();
		await expect.element(option(/^Hero/)).toBeVisible();
		await expect.element(option("Title")).not.toBeInTheDocument();
		await option(/^Hero/).click();
		await expect.element(page.getByRole("status")).toHaveTextContent("Layout > Hero");
		await expect.element(option(/^Children/)).toHaveTextContent("Children Blocks");
		await option(/^Children/).click();
		await option(/^Note/).click();
		await option("Body").click();

		await expect.poll(() => changed.mock.lastCall?.[0]).toBe("layout.hero.children.note.body");
		await expect.element(option("Body")).not.toBeInTheDocument();
		await expect.element(trigger()).toHaveTextContent("Layout > Hero > Children > Note > Body");
		await expect.element(trigger()).toHaveFocus();

		// Reopening shows the chosen field among its siblings, then Back returns to its parent.
		await trigger().click();
		await expect.element(option("Body")).toHaveAttribute("aria-selected", "true");
		await page
			.getByRole("button", { name: "Back to Layout > Hero > Children", exact: true })
			.click();
		await expect.element(option(/^Note/)).toBeVisible();
	} finally {
		await screen.unmount();
	}
});

it("supports keyboard navigation, search, Escape and return focus", async () => {
	const changed = vi.fn();
	const screen = await render(Picker, { fields: pages(), initial: "title", changed });
	try {
		await expect.element(trigger()).toHaveTextContent("Title");
		(trigger().element() as HTMLElement).focus();
		await userEvent.keyboard("{Enter}");
		const search = page.getByRole("combobox", { name: "Search fields", exact: true });
		await expect.element(search).toHaveFocus();
		await expect.element(option("Title")).toHaveAttribute("aria-selected", "true");

		// Down to Layout, Right opens it, Enter opens Hero, Left/Backspace go back up.
		await userEvent.keyboard("{ArrowDown}");
		await expect.element(option(/^Layout/)).toHaveAttribute("aria-selected", "true");
		await userEvent.keyboard("{ArrowRight}");
		await expect.element(option(/^Hero/)).toHaveAttribute("aria-selected", "true");
		await userEvent.keyboard("{Enter}");
		await expect.element(option("Heading")).toHaveAttribute("aria-selected", "true");
		await userEvent.keyboard("{ArrowLeft}");
		await expect.element(option(/^Hero/)).toBeVisible();
		await userEvent.keyboard("{Backspace}");
		await expect.element(option("Title")).toBeVisible();
		await expect.element(search).toHaveFocus();

		// Search reaches nested definitions at concrete, fully labelled paths.
		await userEvent.keyboard("rank");
		await expect.element(option("Layout > Hero > Rank")).toHaveAttribute("aria-selected", "true");
		await expect.element(option("Title")).not.toBeInTheDocument();
		await userEvent.keyboard("{Enter}");
		await expect.poll(() => changed.mock.lastCall?.[0]).toBe("layout.hero.rank");
		await expect.element(trigger()).toHaveFocus();

		// Arrow keys open the trigger like the neighbouring Select controls.
		await userEvent.keyboard("{ArrowDown}");
		await expect.element(search).toHaveFocus();
		await expect.element(search).toHaveValue("");
		await expect.element(option("Rank")).toHaveAttribute("aria-selected", "true");
		await userEvent.keyboard("zzz");
		await expect.element(page.getByText("No fields found", { exact: true })).toBeVisible();
		await userEvent.keyboard("{Escape}");
		await expect.element(search).not.toBeInTheDocument();
		await expect.element(trigger()).toHaveFocus();
		expect(changed).toHaveBeenCalledTimes(1);
	} finally {
		await screen.unmount();
	}
});

it("renders one level of a 2^30-placement block graph and bounds search results", async () => {
	const definitions = Array.from({ length: 31 }, (_, level) =>
		blockDefinition(
			`level-${level}`,
			level === 0
				? [field("title", "text")]
				: [
						field("title", "text"),
						blocks("left", [`level-${level - 1}`]),
						blocks("right", [`level-${level - 1}`]),
					]
		)
	);
	const i18n = createAdminI18n();
	const fields = new ListFilterFields({
		fields: bindBlockFields(definitions, [blocks("layout", ["level-30"])]),
		metadata: listMetadataFields(undefined, i18n),
		i18n,
	});
	const changed = vi.fn();
	const screen = await render(Picker, { fields, changed });
	try {
		await trigger().click();
		await expect.element(option(/^Layout/)).toBeVisible();
		expect(document.querySelectorAll('[role="option"]').length).toBe(4);

		await page.getByRole("combobox", { name: "Search fields", exact: true }).fill("title");
		await expect.element(option("Layout > level-30 > Title")).toBeVisible();
		expect(document.querySelectorAll('[role="option"]').length).toBe(50);
		await expect.element(page.getByText(/^Showing the first 50 matches/)).toBeVisible();
		await option(/^Layout > level-30 > Right > level-29 > Left > level-28 > Title$/).click();
		await expect
			.poll(() => changed.mock.lastCall?.[0])
			.toBe("layout.level-30.right.level-29.left.level-28.title");
	} finally {
		await screen.unmount();
	}
});

it("hydrates persisted nested filters and commits their exact block paths", async () => {
	const onChange = vi.fn();
	const screen = await render(Builder, {
		fields: pages(),
		filters: [[{ field: "layout.hero.children.note.body", operator: "like", value: "launch" }]],
		onChange,
	});
	try {
		await expect.element(trigger()).toHaveTextContent("Layout > Hero > Children > Note > Body");
		await page.getByRole("textbox", { name: "Filter value", exact: true }).fill("launched");
		await expect
			.poll(() => onChange.mock.lastCall?.[0], { timeout: 2000 })
			.toEqual([
				[{ field: "layout.hero.children.note.body", operator: "like", value: "launched" }],
			]);

		await trigger().click();
		await page.getByRole("button", { name: /^Back to/ }).click();
		await page.getByRole("button", { name: /^Back to/ }).click();
		await option("Rank").click();
		await expect.element(page.getByLabelText("Filter operator")).toHaveTextContent("equals");
		await page.getByRole("spinbutton", { name: "Filter value", exact: true }).fill("3");
		await expect
			.poll(() => onChange.mock.lastCall?.[0], { timeout: 2000 })
			.toEqual([[{ field: "layout.hero.rank", operator: "equals", value: "3" }]]);
	} finally {
		await screen.unmount();
	}
});
