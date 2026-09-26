import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import type { SchemaField } from "@riducms/protocol";
import { expect, it } from "vitest";
import { page } from "vitest/browser";
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
			],
		},
	};
}

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
