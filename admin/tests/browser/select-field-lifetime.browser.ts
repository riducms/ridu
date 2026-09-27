import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import type { SchemaField } from "@riducms/protocol";
import { expect, it } from "vitest";
import { page, userEvent } from "vitest/browser";
import { render } from "vitest-browser-svelte";

import type { AdminClient } from "@admin/core/api/admin-client";
import { FormController } from "@admin/core/forms/form-controller.svelte";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

const SelectHarness = svelte`
	<script>
		import { setAdminI18n } from "@riducms/plugin";
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import SelectField from "../../src/fields/select/select-field.svelte";
		let { runtime, form, field } = $props();
		setAdminRuntime(runtime);
		setAdminI18n(runtime.i18n);
	</script>
	<SelectField {field} {form} />
`;

function selectField(hasMany: boolean): SchemaField {
	return {
		id: "category",
		name: "category",
		path: "category",
		type: "select",
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: "Category" },
		select: {
			hasMany,
			options: [
				{ value: "news", label: "News" },
				{ value: "guides", label: "Guides" },
				{ value: "reviews", label: "Reviews" },
			],
		},
	};
}

it.each(["mouse", "keyboard"])(
	"hides selected options and clears the multi-select search after %s selection",
	async (method) => {
		const runtime = new AdminRuntime({} as AdminClient);
		const field = selectField(true);
		const form = new FormController({}, runtime.i18n);
		form.reset({ category: ["news"] }, [field]);
		const screen = await render(SelectHarness, { runtime, form, field });
		const input = screen.getByRole("combobox", { name: "Category", exact: true });
		const guides = page.getByRole("option", { name: "Guides", exact: true });
		const reviews = page.getByRole("option", { name: "Reviews", exact: true });

		try {
			await input.click();
			await expect.element(guides).toBeVisible();
			await expect
				.element(page.getByRole("option", { name: "News", exact: true }))
				.not.toBeInTheDocument();

			if (method === "keyboard") {
				await input.fill("Review");
				await expect.element(guides).not.toBeInTheDocument();
				await userEvent.keyboard("{ArrowDown}{Enter}");
			} else {
				await reviews.click();
			}

			await expect.poll(() => form.get("category")).toEqual(["news", "reviews"]);
			await expect.element(input).toHaveValue("");
			await expect.element(input).toHaveFocus();
			await expect.element(reviews).not.toBeInTheDocument();
			await expect.element(guides).toBeVisible();

			await screen.getByRole("button", { name: "Remove Reviews", exact: true }).click();
			await input.click();
			await expect.element(reviews).toBeVisible();
			await reviews.click();
			await expect.element(input).toHaveValue("");
			await userEvent.keyboard("{Escape}");
			await input.click();
			await expect.element(reviews).not.toBeInTheDocument();

			await guides.click();
			await expect.poll(() => form.get("category")).toEqual(["news", "reviews", "guides"]);
			await expect.element(page.getByRole("option")).not.toBeInTheDocument();
			await expect.element(page.getByText("No Category match")).not.toBeInTheDocument();
			await input.fill("missing");
			await expect.element(page.getByText("No Category match")).toBeVisible();
		} finally {
			await screen.unmount();
			form.disposeBindings();
			runtime.dispose();
		}
	}
);

for (const initialHasMany of [false, true]) {
	const initialMode = initialHasMany ? "multiple" : "single";
	const nextMode = initialHasMany ? "single" : "multiple";
	it(`writes a ${nextMode} value after a mounted select changes from ${initialMode} to ${nextMode}`, async () => {
		const runtime = new AdminRuntime({} as AdminClient);
		const field = selectField(initialHasMany);
		const form = new FormController({}, runtime.i18n);
		form.reset({ category: initialHasMany ? [] : "" }, [field]);
		const screen = await render(SelectHarness, { runtime, form, field });
		try {
			await expect
				.element(screen.getByRole("combobox", { name: "Category", exact: true }))
				.toBeVisible();

			const nextField = selectField(!initialHasMany);
			form.reset({ category: initialHasMany ? "" : [] }, [nextField]);
			await screen.rerender({ field: nextField });
			await screen.getByRole("combobox", { name: "Category", exact: true }).click();
			await page.getByRole("option", { name: "News", exact: true }).click();

			expect(form.get("category")).toEqual(initialHasMany ? "news" : ["news"]);
		} finally {
			await screen.unmount();
			form.disposeBindings();
			runtime.dispose();
		}
	});
}
