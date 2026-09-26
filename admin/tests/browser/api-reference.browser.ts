import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { expect, it, vi } from "vitest";
import { userEvent } from "vitest/browser";
import { render } from "vitest-browser-svelte";
import { SCHEMA_MANIFEST_VERSION, type SchemaCollection } from "@riducms/protocol";
import type { AdminClient } from "@admin/core/api/admin-client";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

const Reference = svelte`
	<script>
		import { setAdminI18n } from "@riducms/plugin";
		import { TooltipProvider } from "@riducms/ui";
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import APIReference from "../../src/features/api-reference/api-reference.svelte";
		let { runtime, collection, documentID } = $props();
		setAdminRuntime(runtime);
		setAdminI18n(runtime.i18n);
	</script>
	<TooltipProvider>{#key collection.slug}<APIReference {collection} {documentID} />{/key}</TooltipProvider>
`;

it("navigates documentation, copies the active example, restores focus and discards a changed collection", async () => {
	const collection: SchemaCollection = {
		id: "posts",
		slug: "posts",
		labels: { singular: "Post", plural: "Posts" },
		admin: {},
		capabilities: {
			auth: false,
			upload: false,
			versions: false,
			trash: false,
			global: false,
			locking: false,
		},
		fields: [
			{
				id: "title",
				name: "title",
				path: "title",
				type: "text",
				category: "scalar",
				required: true,
				unique: false,
				admin: { label: "Title" },
			},
		],
	};
	const runtime = new AdminRuntime({} as AdminClient);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: { name: "Reference" },
		collections: [collection],
		plugins: [],
	};
	const copy = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
	const screen = await render(Reference, { runtime, collection, documentID: "one" });
	try {
		const trigger = screen.getByRole("button", { name: "API reference", exact: true });
		await trigger.click();
		await expect
			.element(screen.getByRole("heading", { name: "List / Search (posts)" }))
			.toBeVisible();
		expect(document.querySelectorAll(".ridu-api-reference__code")).toHaveLength(1);
		expect(document.querySelectorAll(".ridu-api-reference .ridu-json-viewer")).toHaveLength(1);
		await screen.getByRole("button", { name: "Read", exact: true }).click();
		await screen.getByRole("tab", { name: "cURL", exact: true }).click();
		expect(document.querySelectorAll(".ridu-api-reference__code")).toHaveLength(1);
		await screen.getByRole("button", { name: "Copy example" }).click();
		expect(copy.mock.lastCall?.[0]).toContain("/api/collections/posts/one");
		await screen.getByRole("tab", { name: "404", exact: true }).click();
		await expect.element(screen.getByText('"Document not found"', { exact: true })).toBeVisible();

		await screen.getByRole("button", { name: "Create", exact: true }).click();
		await expect
			.element(screen.getByRole("tab", { name: "201", exact: true }))
			.toHaveAttribute("aria-selected", "true");
		copy.mockRejectedValueOnce(new Error("Clipboard unavailable"));
		await screen.getByRole("button", { name: "Copy example" }).click();
		await expect
			.element(screen.getByRole("status").filter({ hasText: "Copy unavailable" }))
			.toBeVisible();
		await userEvent.keyboard("{Escape}");
		await expect.element(screen.getByRole("dialog")).not.toBeInTheDocument();
		await expect.element(trigger).toHaveFocus();

		await trigger.click();
		await expect
			.element(screen.getByRole("heading", { name: "List / Search (posts)" }))
			.toBeVisible();
		await screen.rerender({
			runtime,
			collection: { ...collection, slug: "categories" },
			documentID: "two",
		});
		await expect.element(screen.getByRole("dialog")).not.toBeInTheDocument();
		await screen.getByRole("button", { name: "API reference", exact: true }).click();
		await screen.getByRole("button", { name: "Read", exact: true }).click();
		await screen.getByRole("button", { name: "Copy example" }).click();
		expect(copy.mock.lastCall?.[0]).toContain('ridu.find("categories", "two")');
	} finally {
		copy.mockRestore();
		await screen.unmount();
		runtime.dispose();
	}
});
