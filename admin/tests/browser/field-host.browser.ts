import { expect, it } from "vitest";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import {
	defineAdminPlugin,
	definePluginField,
	type PluginFieldBinding,
} from "@riducms/plugin/authoring/v1";
import { defineFieldEditor, type FieldBinding } from "@riducms/plugin/editor";
import type { SchemaField } from "@riducms/protocol";
import { createAdminClient } from "@admin/core/api/admin-client";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { FormController } from "@admin/core/forms/form-controller.svelte";
import { draftFixture } from "./draft-fixture";

const NoteEditor = svelte`
	<script module>
		export const bindings = [];
	</script>
	<script>
		let { field } = $props();
		bindings.push(field);
	</script>
	<input aria-label="Note" value={field.value ?? ""} disabled={field.readOnly}
		oninput={event => field.set(event.currentTarget.value)} />
`;
// Inline templates deliberately do not replace the separate component-prop type probes.
const { bindings } = NoteEditor as unknown as {
	bindings: (PluginFieldBinding<string> | FieldBinding<"text">)[];
};

const Harness = svelte`
	<script>
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import { setAdminI18n } from "@riducms/plugin";
		import FieldRenderer from "../../src/fields/field-renderer.svelte";
		let { form, schema, runtime } = $props();
		setAdminRuntime(runtime);
		setAdminI18n(runtime.i18n);
	</script>
	<button onclick={() => form.setRows("rows", form.snapshot().rows.toReversed())}>Reverse rows</button>
	<FieldRenderer field={schema} {form} />
`;

for (const kind of ["plugin", "local"] as const)
	it(`the ${kind} field host preserves mounted bindings through reorder and revokes removed and unmounted editors`, async () => {
		bindings.length = 0;
		const note: SchemaField = {
			id: "note",
			name: "note",
			path: "rows.note",
			type: "plugin",
			category: "plugin",
			required: false,
			unique: false,
			admin: { label: "Note" },
			plugin: { key: "note", config: {} },
		};
		if (kind === "local") {
			note.type = "text";
			note.category = "scalar";
			note.admin.editor = { reference: "app:note" };
			delete note.plugin;
		}
		const schema: SchemaField = {
			id: "rows",
			name: "rows",
			path: "rows",
			type: "array",
			category: "nested",
			required: false,
			unique: false,
			admin: { label: "Rows" },
			nested: { fields: [note] },
		};
		const plugin = defineAdminPlugin({
			key: "notes",
			pairingVersion: 1,
			fields: {
				note: definePluginField({
					component: NoteEditor,
					decodeValue(value: unknown) {
						if (typeof value !== "string") throw new Error("Expected a note string");
						return value;
					},
				}),
			},
		});
		const runtime = new AdminRuntime(
			createAdminClient(),
			kind === "plugin" ? [plugin] : [],
			undefined,
			undefined,
			kind === "local"
				? { "app:note": defineFieldEditor({ type: "text", component: NoteEditor }) }
				: {}
		);
		const form = new FormController();
		form.reset(
			{
				rows: [
					{ _key: "a", note: "First" },
					{ _key: "b", note: "Second" },
				],
			},
			[schema]
		);
		const screen = await render(Harness, { form, schema, runtime });
		await expect.element(screen.getByRole("textbox", { name: "Note" }).nth(0)).toHaveValue("First");
		expect(bindings).toHaveLength(2);
		const first = bindings[0]!;
		const retainedWrite = first.set;
		await screen.getByRole("textbox", { name: "Note" }).nth(0).fill("Edited first");
		await screen.getByRole("button", { name: "Reverse rows" }).click();
		await expect
			.element(screen.getByRole("textbox", { name: "Note" }).nth(1))
			.toHaveValue("Edited first");
		expect(bindings).toHaveLength(2);
		retainedWrite("Delayed first");
		expect(form.get("rows.1.note")).toBe("Delayed first");
		expect(form.get("rows.0.note")).toBe("Second");
		form.writeBlocked = true;
		await expect.element(screen.getByRole("textbox", { name: "Note" }).nth(1)).toBeDisabled();
		expect(() => retainedWrite("Blocked")).toThrow("read-only");
		form.writeBlocked = false;
		form.setRows("rows", []);
		form.setRows("rows", [{ _key: "a", note: "New occurrence" }]);
		expect(() => retainedWrite("Wrong occurrence")).toThrow("stale");
		await expect
			.element(screen.getByRole("textbox", { name: "Note" }))
			.toHaveValue("New occurrence");
		expect(bindings).toHaveLength(3);
		const current = bindings[2]!;
		await screen.unmount();
		expect(current.stale).toBe(true);
		expect(() => current.set("After unmount")).toThrow("stale");
		expect(form.isRegistered("rows.0.note")).toBe(false);
	});

const EmbeddedHarness = svelte`
	<script>
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import { setAdminI18n } from "@riducms/plugin";
		import { embeddedOccurrences } from "../../src/core/forms/embedded-fields";
		import EmbeddedSchemaFields from "../../src/fields/embedded-schema-fields.svelte";
		let { form, schema, runtime } = $props();
		setAdminRuntime(runtime);
		setAdminI18n(runtime.i18n);
		const embedded = $derived(embeddedOccurrences(schema, form.get("body")));
	</script>
	{#each embedded.occurrences as occurrence (occurrence.identity)}
		<EmbeddedSchemaFields field={schema} {form} {occurrence} issues={embedded.issues}
			scope={{ treeKey: occurrence.tree.key, identity: occurrence.identity }} />
	{/each}
`;

it("replacing a plugin envelope preserves nested row mounts through embedded occurrence reorder", async () => {
	bindings.length = 0;
	const note: SchemaField = {
		id: "note",
		name: "note",
		path: "body.rows.note",
		type: "plugin",
		category: "plugin",
		required: false,
		unique: false,
		admin: { label: "Note" },
		plugin: { key: "note", config: {} },
	};
	const rows: SchemaField = {
		id: "rows",
		name: "rows",
		path: "body.rows",
		type: "array",
		category: "nested",
		required: false,
		unique: false,
		admin: { label: "Rows" },
		nested: { fields: [note] },
	};
	const { form, schema, binding } = draftFixture([rows]);
	const widget = (uid: string, note: string) => ({
		kind: "widget",
		content: { schema: "card", uid, rows: [{ _key: "child", note }] },
	});
	form.setEmbedded(schema, { outline: [widget("a", "First"), widget("b", "Second")] });
	const runtime = new AdminRuntime(createAdminClient(), [
		defineAdminPlugin({
			key: "notes",
			pairingVersion: 1,
			fields: {
				note: definePluginField({
					component: NoteEditor,
					decodeValue(value: unknown) {
						if (typeof value !== "string") throw new Error("Expected a note string");
						return value;
					},
				}),
			},
		}),
	]);
	const screen = await render(EmbeddedHarness, { form, schema, runtime });
	await expect.element(screen.getByRole("textbox", { name: "Note" }).nth(0)).toHaveValue("First");
	expect(bindings).toHaveLength(2);
	const writeFirst = bindings[0]!.set;
	const body = form.snapshot().body as { outline: unknown[] };
	form.setEmbedded(schema, { outline: body.outline.toReversed() });
	await expect.element(screen.getByRole("textbox", { name: "Note" }).nth(1)).toHaveValue("First");
	expect(bindings).toHaveLength(2);
	writeFirst("Still first");
	expect(form.get("body.outline.1.content.rows.0.note")).toBe("Still first");
	expect(form.get("body.outline.0.content.rows.0.note")).toBe("Second");
	await screen.unmount();
	binding.destroy();
});
