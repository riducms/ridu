import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { createAdminI18n } from "@riducms/translations";
import { createAdminClient } from "@admin/core/api/admin-client";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { PluginFieldBinding } from "@admin/core/forms/plugin-field-binding";
import { guardPluginAuthoring } from "@admin/core/forms/plugin-field-authoring";
import { createEmbeddedSchemaDraft } from "@admin/core/forms/embedded-schema-draft.svelte";
import { draftFixture, tree } from "./draft-fixture";

const Drawer = svelte`
	<script>
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import { setAdminI18n } from "@riducms/plugin";
		import DraftEditor from "../../src/fields/embedded-schema-draft-editor.svelte";
		let { session, options, runtime } = $props();
		setAdminRuntime(runtime);
		setAdminI18n(runtime.i18n);
	</script>
	<p role="status">{session.draft.dirty ? "Unsaved draft" : "No draft changes"}</p>
	<DraftEditor {session} {options} />
`;

it("discard updates the mounted drawer and immediately revokes retained child writes", async () => {
	const fixture = draftFixture();
	const draft = fixture.authoring.beginSchemaDraft!({ treeKey: "widgets", identity: "a" });
	const session = fixture.sessions[0]!;
	const child = new PluginFieldBinding(session.form, () => session.fields[0]!, {
		decodeValue: (value) => value,
	});
	const runtime = new AdminRuntime(createAdminClient());
	const screen = await render(Drawer, {
		session,
		runtime,
		options: { draft, onApply: vi.fn(), onCancel: vi.fn() },
	});
	await screen.getByRole("textbox", { name: "Card title" }).fill("Unsaved");
	await expect.element(screen.getByRole("status")).toHaveTextContent("Unsaved draft");
	await expect.element(screen.getByRole("button", { name: "Apply", exact: true })).toBeEnabled();
	draft.discard();
	expect(() => child.set("Too late")).toThrow("stale");
	expect(fixture.form.pendingEditIssues()).toEqual([]);
	await expect.element(screen.getByRole("status")).toHaveTextContent("No draft changes");
	await expect.element(screen.getByRole("button", { name: "Apply", exact: true })).toBeDisabled();
	await expect.element(screen.getByRole("textbox", { name: "Card title" })).not.toBeInTheDocument();
	await expect
		.element(screen.getByRole("alert"))
		.toHaveTextContent(runtime.i18n.t("errors:staleEmbeddedEdit"));
	await screen.unmount();
	fixture.binding.destroy();
});

it("Apply, Cancel and drawer unmount release their raw draft without retaining owner cleanup", async () => {
	const fixture = draftFixture();
	const runtime = new AdminRuntime(createAdminClient());
	for (const action of ["Apply", "Cancel", "unmount"] as const) {
		const draft = fixture.authoring.beginSchemaDraft!({ treeKey: "widgets", identity: "a" });
		const session = fixture.sessions.at(-1)!;
		const onApply = vi.fn(),
			onCancel = vi.fn();
		const screen = await render(Drawer, {
			session,
			runtime,
			options: { draft, onApply, onCancel },
		});
		await screen.getByRole("textbox", { name: "Card title" }).fill(action);
		expect(fixture.form.dirty).toBe(true);
		if (action !== "unmount")
			await screen.getByRole("button", { name: action, exact: true }).click();
		await screen.unmount();
		expect(draft.stale).toBe(true);
		expect(fixture.form.dirty).toBe(false);
		if (action === "Apply")
			expect(onApply).toHaveBeenCalledWith(expect.objectContaining({ title: "Apply" }));
		if (action === "Cancel") expect(onCancel).toHaveBeenCalledOnce();
	}
	const completed = fixture.disposals.map(({ count }) => count);
	fixture.binding.destroy();
	expect(fixture.disposals.map(({ count }) => count)).toEqual(completed);
});

const DerivedReader = svelte`
	<script>
		let { child } = $props();
		const status = $derived.by(() => {
			try {
				child.value;
				return "Active";
			} catch (error) {
				return error.message;
			}
		});
	</script>
	<p role="status">{status}</p>
`;

it("a rendered derived read can revoke nested drafts without unsafe reactive cleanup writes", async () => {
	const fixture = draftFixture(
		[
			{
				id: "inner",
				name: "inner",
				path: "body.inner",
				type: "plugin",
				category: "plugin",
				required: false,
				unique: false,
				admin: { label: "Inner" },
				plugin: { key: "outline", config: {}, embeddedTrees: [tree([])] },
			},
		],
		{ inner: { outline: [] } }
	);
	fixture.authoring.beginSchemaDraft!({ treeKey: "widgets", identity: "a" });
	const session = fixture.sessions[0]!;
	const child = new PluginFieldBinding(session.form, () => session.fields[0]!, {
		decodeValue: (value) => value,
	});
	const authoring = guardPluginAuthoring(
		{
			collections: [],
			documentRevision: 0,
			referenceBrowser: () => ({}),
			findDocument: async () => ({ id: "one" }),
			beginSchemaDraft: (scope) =>
				createEmbeddedSchemaDraft(
					session.form,
					() => child.schema,
					scope,
					createAdminI18n(),
					() => !child.stale
				).draft,
		},
		child
	);
	const nested = authoring.beginSchemaDraft!({
		treeKey: "widgets",
		caseTag: "widget",
		variantSlug: "card",
	});
	const screen = await render(DerivedReader, { child });
	await expect.element(screen.getByRole("status")).toHaveTextContent("Active");
	fixture.form.setAccess(
		{
			operations: { update: true },
			fields: { body: { read: true, update: false, create: false } },
		} as never,
		"update"
	);
	await expect
		.element(screen.getByRole("status"))
		.toHaveTextContent(
			"This plugin field binding is stale. Use the current mounted field occurrence."
		);
	expect(child.stale).toBe(true);
	expect(nested.stale).toBe(true);
	expect(() => child.set({ outline: [] })).toThrow("stale");
	expect(session.form.pendingEditIssues()).toEqual([]);
	await screen.unmount();
	fixture.binding.destroy();
});
