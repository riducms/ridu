import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import type { SchemaField } from "@riducms/protocol";
import { expect, it } from "vitest";
import { render } from "vitest-browser-svelte";

import type { AdminClient } from "@admin/core/api/admin-client";
import { FormController } from "@admin/core/forms/form-controller.svelte";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

const SlugHarness = svelte`
	<script>
		import { setAdminI18n } from "@riducms/plugin";
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import SlugField from "../../src/fields/text/slug-field.svelte";
		let { runtime, form, field } = $props();
		setAdminRuntime(runtime);
		setAdminI18n(runtime.i18n);
	</script>
	<SlugField {field} {form} />
`;

it("rebinds a mounted slug after reset without turning form reads into effect dependencies", async () => {
	const runtime = new AdminRuntime({} as AdminClient);
	const form = new FormController({ title: "Hello World", slug: "hello-world" }, runtime.i18n);
	const screen = await render(SlugHarness, { runtime, form, field: slugField });
	try {
		const input = screen.getByRole("textbox", { name: "Slug" });
		await expect.element(input).toHaveValue("hello-world");

		form.set("title", "Next Title");
		await expect.element(input).toHaveValue("next-title");

		await screen.getByRole("button", { name: "Unlock slug" }).click();
		await input.fill("kept-by-author");
		input.element().blur();
		form.set("title", "Ignored Title");
		await expect.element(input).toHaveValue("kept-by-author");

		form.reset({ title: "Fresh Route", slug: "" });
		await expect.element(input).toHaveValue("fresh-route");
		form.set("title", "Retained Route");
		await expect.element(input).toHaveValue("retained-route");
	} finally {
		await screen.unmount();
		form.disposeBindings();
		runtime.dispose();
	}
});

const slugField = {
	id: "slug",
	name: "slug",
	path: "slug",
	type: "text",
	category: "scalar",
	required: false,
	unique: true,
	admin: { label: "Slug" },
	text: { slug: { sourcePath: "title" } },
} as SchemaField;
