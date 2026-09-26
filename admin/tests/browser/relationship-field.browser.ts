import { expect, it, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { SCHEMA_MANIFEST_VERSION, type Pagination, type SchemaField } from "@riducms/protocol";
import type { AdminClient, AdminDocument } from "@admin/core/api/admin-client";
import { FormController } from "@admin/core/forms/form-controller.svelte";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { RelationshipFieldController } from "@admin/fields/relationship/relationship-field-controller.svelte";

const Harness = svelte`
 <script>
  let { Controller, runtime, form, field, ready } = $props();
  // svelte-ignore state_referenced_locally
  const controller = new Controller({ runtime, form, get field() { return field; } });
  ready(controller);
 </script>
 <p>{controller.selections.map((item) => item.label).join(", ")}</p>
`;
const field: SchemaField = {
	id: "related",
	name: "related",
	path: "related",
	type: "relationship",
	category: "relationship",
	required: false,
	unique: false,
	admin: { label: "Related" },
	relationship: {
		hasMany: true,
		polymorphic: true,
		onDelete: "nullify",
		targets: [
			{ collectionId: "posts", collectionSlug: "posts" },
			{ collectionId: "pages", collectionSlug: "pages" },
		],
	},
};
const related = [
	{ relationTo: "posts", id: "1" },
	{ relationTo: "pages", id: "1" },
	{ relationTo: "posts", id: "2" },
];
async function fixture(
	find = vi.fn(async (slug: string, id: string) => ({ id, title: `${slug} ${id}` })),
	documentCount = 2
) {
	const list = vi.fn(
		async (
			slug: string,
			options: { page: number; limit: number; where?: Record<string, unknown> }
		) => {
			const title = options.where?.title as { like?: string } | undefined;
			const docs = Array.from({ length: documentCount }, (_, index) => String(index + 1))
				.map((id) => ({ id, title: `${slug} ${id}` }))
				.filter((document) => document.title.includes(title?.like ?? ""));
			const start = (options.page - 1) * options.limit;

			return {
				docs: docs.slice(start, start + options.limit),
				pagination: {
					page: options.page,
					limit: options.limit,
					totalDocs: docs.length,
					totalPages: Math.ceil(docs.length / options.limit),
					hasNextPage: start + options.limit < docs.length,
					hasPrevPage: options.page > 1,
				},
			};
		}
	);
	const runtime = new AdminRuntime({ find, list } as unknown as AdminClient);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: { name: "References" },
		plugins: [],
		collections: ["posts", "pages"].map((slug) => ({
			id: slug,
			slug,
			labels: { singular: slug, plural: slug },
			admin: { useAsTitle: "title" },
			fields: [
				{
					id: "title",
					name: "title",
					path: "title",
					type: "text",
					category: "scalar",
					required: false,
					unique: false,
					admin: { label: "Title" },
				},
			],
			capabilities: { auth: false, upload: false, versions: false, trash: false, locking: false },
		})),
	};
	const form = new FormController();
	form.reset({ related }, [field]);
	let controller!: RelationshipFieldController;
	const props = {
		Controller: RelationshipFieldController,
		runtime,
		form,
		field,
		ready: (value: RelationshipFieldController) => (controller = value),
	};
	const screen = await render(Harness, props);
	return { runtime, form, controller, props, screen, find };
}
it("hydrates colliding IDs from every target, preserves ordering, and prevents read-only mutations", async () => {
	const fixtureValue = await fixture();
	const { controller, form, screen, props } = fixtureValue;
	await expect
		.poll(() => controller.selections.map((item) => item.label))
		.toEqual(["posts 1", "pages 1", "posts 2"]);
	controller.commit(["1", "2"]);
	expect(form.get("related")).toEqual(related);
	controller.move(2, 0);
	expect(form.get("related")).toEqual([related[2], related[0], related[1]]);
	controller.removeReference(related[0]!);
	expect(form.get("related")).toEqual([related[2], related[1]]);
	await screen.rerender({
		...props,
		field: { ...field, admin: { label: "Related", readOnly: true } },
	});
	controller.move(1, 0);
	controller.commit([]);
	controller.removeReference(related[2]!);
	expect(form.get("related")).toEqual([related[2], related[1]]);
	await screen.unmount();
});
it("ignores hydration completed after a locale change or unmount", async () => {
	const old = Promise.withResolvers<AdminDocument>();
	const find = vi.fn(async (slug: string, id: string) => ({ id, title: `${slug} ${id}` }));
	find.mockImplementationOnce(() => old.promise as Promise<{ id: string; title: string }>);
	const { form, controller, screen } = await fixture(find);
	form.setLocalization("fr");
	await expect.poll(() => controller.selections[0]?.label).toBe("posts 1");
	old.resolve({ id: "1", title: "stale" });
	await old.promise;
	expect(controller.selections[0]?.label).toBe("posts 1");
	await screen.unmount();
});

const Selection = svelte`
 <script>
  import { DragDropProvider } from "@dnd-kit-svelte/svelte";
  import { setAdminI18n } from "@riducms/plugin";
  import RelationshipSelection from "../../src/fields/relationship/relationship-selection.svelte";
  let { i18n, readOnly = false, upload = false, edit, remove } = $props();
  setAdminI18n(i18n);
 </script>
 <DragDropProvider><RelationshipSelection id="one" index={0} label="Selected post" sortable {readOnly} {upload} onEdit={edit} onRemove={remove} /></DragDropProvider>
`;

it.each([false, true])(
	"preserves the selected item's surface while dragging (upload: %s)",
	async (upload) => {
		const runtime = new AdminRuntime({} as AdminClient);
		const screen = await render(Selection, {
			i18n: runtime.i18n,
			upload,
			edit: vi.fn(),
			remove: vi.fn(),
		});
		const handle = screen.getByRole("button", { name: "Drag Selected post", exact: true });
		const surface = handle.element().closest(".ridu-upload-reference, .ridu-relationship-chip")!;
		const appearance = () => {
			const style = getComputedStyle(surface);
			const bounds = surface.getBoundingClientRect();

			return {
				background: style.backgroundColor,
				border: style.border,
				padding: style.padding,
				opacity: style.opacity,
				width: bounds.width,
				height: bounds.height,
			};
		};
		const resting = appearance();
		expect(resting.background).not.toBe("rgba(0, 0, 0, 0)");
		expect(resting.opacity).toBe("1");

		handle.element().focus();
		await userEvent.keyboard("{Space}");
		await expect.element(handle).toHaveAttribute("aria-grabbed", "true");
		expect(appearance()).toEqual(resting);

		await userEvent.keyboard("{Escape}");
		await expect.element(handle).toHaveFocus();
		await expect.poll(appearance).toEqual(resting);
		await screen.unmount();
	}
);

it("sortable metadata does not disable the selection's edit and remove controls", async () => {
	const runtime = new AdminRuntime({} as AdminClient);
	const edit = vi.fn(),
		remove = vi.fn();
	const props = { i18n: runtime.i18n, edit, remove };
	const screen = await render(Selection, props);
	const editButton = screen.getByRole("button", { name: "Edit Selected post", exact: true });
	const removeButton = screen.getByRole("button", { name: "Remove Selected post", exact: true });
	await expect.element(editButton).toBeEnabled();
	expect(editButton.element().closest('[aria-disabled="true"]')).toBeNull();
	await editButton.click();
	await removeButton.click();
	expect(edit).toHaveBeenCalledOnce();
	expect(remove).toHaveBeenCalledOnce();
	await screen.rerender({ ...props, readOnly: true });
	await expect.element(editButton).toBeEnabled();
	expect(editButton.element().closest('[aria-disabled="true"]')).toBeNull();
	await expect.element(removeButton).not.toBeInTheDocument();
	await screen.unmount();
});

const Relationship = svelte`
 <script>
  import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
  import { setAdminI18n } from "@riducms/plugin";
  import RelationshipField from "../../src/fields/relationship/relationship-field.svelte";
  let { runtime, field, form } = $props();
  setAdminRuntime(runtime);
  setAdminI18n(runtime.i18n);
 </script>
 <div style="width: 800px"><RelationshipField {field} {form} /></div>
`;

it.each(["mouse", "keyboard"])(
	"clears the multi-value search after %s selection and hides only selected references",
	async (method) => {
		const { runtime, form, screen: controllerScreen } = await fixture();
		await controllerScreen.unmount();
		form.reset({ related: [related[0]] }, [field]);
		const screen = await render(Relationship, { runtime, form, field });
		const input = screen.getByRole("combobox", { name: "Related", exact: true });
		const post = page.getByRole("option", { name: "posts 2", exact: true });

		await input.click();
		await expect.element(post).toBeVisible();
		await expect
			.element(page.getByRole("option", { name: "posts 1", exact: true }))
			.not.toBeInTheDocument();
		await expect.element(page.getByRole("option", { name: "pages 1", exact: true })).toBeVisible();

		if (method === "keyboard") {
			await input.fill("posts 2");
			await expect
				.element(page.getByRole("option", { name: "pages 1", exact: true }))
				.not.toBeInTheDocument();
			await userEvent.keyboard("{ArrowDown}{Enter}");
		} else {
			await post.click();
		}

		await expect.poll(() => form.get("related")).toEqual([related[0], related[2]]);
		await expect.element(input).toHaveValue("");
		await expect.element(input).toHaveFocus();
		await expect.element(post).not.toBeInTheDocument();
		await expect.element(page.getByRole("option", { name: "pages 1", exact: true })).toBeVisible();

		await screen.getByRole("button", { name: "Remove posts 2", exact: true }).click();
		await input.click();
		await expect.element(post).toBeVisible();
		await post.click();
		await expect.element(input).toHaveValue("");
		await expect.poll(() => form.get("related")).toEqual([related[0], related[2]]);
		await userEvent.keyboard("{Escape}");
		await input.click();
		await expect.element(post).not.toBeInTheDocument();
		await expect.element(input).toHaveValue("");
		await screen.unmount();
	}
);

it("keeps browsing available when all options are selected and restores options after clearing", async () => {
	const { runtime, form, screen: controllerScreen } = await fixture();
	await controllerScreen.unmount();
	form.reset({ related: [...related, { relationTo: "pages", id: "2" }] }, [field]);
	const screen = await render(Relationship, { runtime, form, field });
	const input = screen.getByRole("combobox", { name: "Related", exact: true });

	await input.click();
	await expect.element(page.getByRole("button", { name: "Browse all posts" })).toBeVisible();
	await expect.element(page.getByRole("option")).not.toBeInTheDocument();
	await expect.element(page.getByText("No posts match", { exact: true })).not.toBeInTheDocument();
	await input.fill("missing");
	await expect.element(page.getByText("No posts match", { exact: true })).toBeVisible();
	await input.fill("   ");
	await expect.element(page.getByText("No posts match", { exact: true })).not.toBeInTheDocument();
	await input.fill("");
	await screen.getByRole("button", { name: "Clear selection", exact: true }).click();
	await input.click();
	await expect.element(page.getByRole("option", { name: "posts 1", exact: true })).toBeVisible();
	await expect.element(page.getByRole("option", { name: "pages 1", exact: true })).toBeVisible();
	await expect.element(input).toHaveValue("");
	expect(form.get("related")).toEqual([]);
	await screen.unmount();
});

it.each([5, 105])("fills five suggestions with %i references already selected", async (count) => {
	const { runtime, form, screen: controllerScreen } = await fixture(undefined, count + 5);
	await controllerScreen.unmount();
	const ids = Array.from({ length: count }, (_, index) => String(index + 1));
	form.reset({ related: ids.map((id) => ({ relationTo: "posts", id })) }, [field]);
	const list = vi.spyOn(runtime.client, "list");
	const screen = await render(Relationship, { runtime, form, field });
	await screen.getByRole("combobox", { name: "Related", exact: true }).click();
	await expect
		.element(page.getByRole("option", { name: `posts ${count + 5}`, exact: true }))
		.toBeVisible();
	await expect
		.element(page.getByRole("option", { name: "posts 1", exact: true }))
		.not.toBeInTheDocument();
	await expect.element(page.getByRole("option", { name: "pages 1", exact: true })).toBeVisible();
	expect(
		page.getByRole("group", { name: "posts", exact: true }).getByRole("option").elements()
	).toHaveLength(5);
	for (const [, options] of list.mock.calls) {
		expect(options?.limit).toBeLessThanOrEqual(100);
		expect(options?.where).toBeUndefined();
	}
	await screen.unmount();
});

it("discards an unfinished suggestion page after the search changes", async () => {
	const { runtime, form, screen: controllerScreen } = await fixture(undefined, 110);
	await controllerScreen.unmount();
	form.reset(
		{
			related: Array.from({ length: 105 }, (_, index) => ({
				relationTo: "posts",
				id: String(index + 1),
			})),
		},
		[field]
	);
	const list = vi.spyOn(runtime.client, "list");
	const listDocuments = list.getMockImplementation()!;
	const secondPage = Promise.withResolvers<{ docs: AdminDocument[]; pagination: Pagination }>();
	let oldSignal: AbortSignal | undefined;
	list.mockImplementation((slug, options) => {
		if (slug === "posts" && options?.page === 2) {
			oldSignal = options.signal;
			return secondPage.promise;
		}
		return listDocuments(slug, options);
	});
	const screen = await render(Relationship, { runtime, form, field });
	const input = screen.getByRole("combobox", { name: "Related", exact: true });
	await input.click();
	await expect.poll(() => oldSignal).toBeDefined();

	await input.fill("posts 110");
	expect(oldSignal?.aborted).toBe(true);
	secondPage.resolve({
		docs: [{ id: "106", title: "Stale page" }],
		pagination: {
			page: 2,
			limit: 100,
			totalDocs: 110,
			totalPages: 2,
			hasNextPage: false,
			hasPrevPage: true,
		},
	});
	await expect.element(page.getByRole("option", { name: "posts 110", exact: true })).toBeVisible();
	await expect
		.element(page.getByRole("option", { name: "Stale page", exact: true }))
		.not.toBeInTheDocument();
	await screen.unmount();
});

it("preserves the selected label and replacement options for single-value relationships", async () => {
	const { runtime, form, screen: controllerScreen } = await fixture();
	await controllerScreen.unmount();
	const singleField = { ...field, relationship: { ...field.relationship!, hasMany: false } };
	form.reset({ related: related[0] }, [singleField]);
	const screen = await render(Relationship, { runtime, form, field: singleField });
	const input = screen.getByRole("combobox", { name: "Related", exact: true });

	await expect.element(input).toHaveValue("posts 1");
	await input.click();
	await expect.element(page.getByRole("option", { name: "posts 1", exact: true })).toBeVisible();
	await page.getByRole("option", { name: "pages 2", exact: true }).click();
	await expect.element(input).toHaveValue("pages 2");
	await expect.element(input).toHaveAttribute("aria-expanded", "false");
	expect(form.get("related")).toEqual({ relationTo: "pages", id: "2" });

	await input.fill("posts");
	await userEvent.keyboard("{Escape}");
	await expect.element(input).toHaveValue("pages 2");
	await screen.getByRole("button", { name: "Remove pages 2", exact: true }).click();
	await expect.element(input).toHaveValue("");
	expect(form.get("related")).toBeNull();
	await screen.unmount();
});

it("commits keyboard drag order to form values and leaves cancelled drags unchanged", async () => {
	const { runtime, form, screen: controllerScreen } = await fixture();
	await controllerScreen.unmount();
	const screen = await render(Relationship, { runtime, form, field });
	const handle = screen.getByRole("button", { name: "Drag posts 2", exact: true });
	await expect.element(handle).toBeVisible();
	handle.element().focus();
	await userEvent.keyboard("{Space}{ArrowLeft}{Space}");
	await expect.poll(() => form.get("related")).toEqual([related[0], related[2], related[1]]);
	expect(form.dirty).toBe(true);

	handle.element().focus();
	await userEvent.keyboard("{Space}{ArrowLeft}{Escape}");
	expect(form.get("related")).toEqual([related[0], related[2], related[1]]);
	await screen.unmount();
});
