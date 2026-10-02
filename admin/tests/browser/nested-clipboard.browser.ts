import { afterEach, expect, it, vi } from "vitest";
import { page } from "vitest/browser";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { tick } from "svelte";
import type { SchemaField } from "@riducms/protocol";
import { createAdminClient } from "@admin/core/api/admin-client";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { FormController } from "@admin/core/forms/form-controller.svelte";
import { createFieldClipboardPayload } from "@admin/fields/field-clipboard";

const Harness = svelte`
	<script>
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import { setAdminI18n } from "@riducms/plugin";
		import FieldLayout from "../../src/fields/field-layout.svelte";
		let { form, fields, runtime, visible = true } = $props();
		setAdminRuntime(runtime);
		setAdminI18n(runtime.i18n);
	</script>
	{#if visible}<FieldLayout {form} {fields} />{/if}
`;

afterEach(() => vi.restoreAllMocks());

function schema(type: "array" | "group" = "array", maxRows = 0): SchemaField {
	return {
		id: "items",
		name: "items",
		path: "items",
		type,
		category: "nested",
		required: false,
		unique: false,
		admin: { label: "Items" },
		nested: {
			maxRows,
			rowLabel: "title",
			fields: [
				{
					id: "title",
					name: "title",
					path: "items.title",
					type: "text",
					category: "scalar",
					required: false,
					unique: false,
					admin: { label: "Title" },
					text: {},
				},
			],
		},
	};
}

function fixture(field = schema()) {
	const form = new FormController();
	form.reset(
		{
			items:
				field.type === "group"
					? { title: "Original" }
					: [
							{ _key: "a", title: "Alpha" },
							{ _key: "b", title: "Beta" },
						],
		},
		[field]
	);
	return { form, fields: [field], runtime: new AdminRuntime(createAdminClient()) };
}

async function startPaste(field: SchemaField, kind: "field" | "row") {
	let resolve!: (text: string) => void;
	const pending = new Promise<string>((complete) => {
		resolve = complete;
	});
	const read = vi.spyOn(navigator.clipboard, "readText").mockReturnValue(pending);
	await page
		.getByRole("button", {
			name: `Open ${kind === "row" ? "Alpha" : field.admin.label} actions`,
			exact: true,
		})
		.nth(0)
		.click();
	await page
		.getByRole("menuitem", { name: kind === "row" ? "Paste row" : "Paste field", exact: true })
		.click();
	expect(read).toHaveBeenCalledOnce();
	return async () => {
		const copied = { _key: "copied", title: "Pasted" };
		const value = kind === "field" && field.type === "array" ? [copied] : copied;
		resolve(
			"ridu-field-clipboard:" + JSON.stringify(createFieldClipboardPayload(field, kind, value))
		);
		await pending;
		await tick();
	};
}

it("pastes after the same row at its current position when rows reorder during the clipboard read", async () => {
	const props = fixture();
	const screen = await render(Harness, props);
	const finish = await startPaste(props.fields[0]!, "row");
	const rows = props.form.get("items") as Record<string, unknown>[];
	props.form.setRows("items", [rows[1]!, rows[0]!]);
	await finish();
	expect((props.form.get("items") as Record<string, unknown>[]).map((row) => row.title)).toEqual([
		"Beta",
		"Alpha",
		"Pasted",
	]);
	await screen.unmount();
	props.runtime.dispose();
});

for (const transition of [
	"reset",
	"removed",
	"reinserted",
	"blocked",
	"access denied",
	"full",
	"unmounted",
	"replaced form",
] as const) {
	it(`drops a pending row paste after its owner is ${transition}`, async () => {
		const props = fixture(schema("array", 3));
		const screen = await render(Harness, props);
		const finish = await startPaste(props.fields[0]!, "row");
		const rows = props.form.get("items") as Record<string, unknown>[];
		if (transition === "reset")
			props.form.reset({ items: [{ _key: "a", title: "Replacement" }] }, props.fields);
		if (transition === "removed" || transition === "reinserted") {
			props.form.setRows("items", [rows[1]!]);
			if (transition === "reinserted")
				props.form.setRows("items", [{ _key: "a", title: "Replacement" }, rows[1]!]);
		}
		if (transition === "blocked") props.form.writeBlocked = true;
		if (transition === "access denied")
			props.form.setAccess(
				{
					operations: {
						admin: true,
						create: true,
						read: true,
						readVersions: true,
						update: true,
						delete: true,
						duplicate: true,
						publish: true,
						unpublish: true,
						restoreDeleted: true,
						deletePermanent: true,
						selectAll: true,
					},
					fields: { items: { read: true, create: true, update: false } },
				},
				"update"
			);
		if (transition === "full")
			props.form.setRows("items", [...rows, { _key: "c", title: "Gamma" }]);
		if (transition === "unmounted") await screen.rerender({ visible: false });
		const replacement = fixture();
		if (transition === "replaced form") await screen.rerender({ form: replacement.form });
		const before = props.form.snapshot();
		const replacementBefore = replacement.form.snapshot();
		await finish();
		expect(props.form.snapshot()).toEqual(before);
		expect(replacement.form.snapshot()).toEqual(replacementBefore);
		await screen.unmount();
		props.runtime.dispose();
		replacement.runtime.dispose();
	});
}

for (const type of ["array", "group"] as const) {
	it(`pastes a ${type} field while its original editor is still writable`, async () => {
		const props = fixture(schema(type));
		const screen = await render(Harness, props);
		const finish = await startPaste(props.fields[0]!, "field");
		await finish();
		const value = props.form.get("items");
		expect(type === "array" ? value : [value]).toEqual([
			{ _key: expect.any(String), title: "Pasted" },
		]);
		await screen.unmount();
		props.runtime.dispose();
	});

	for (const transition of ["reset", "blocked", "unmounted"] as const) {
		it(`drops a pending ${type} field paste after its owner is ${transition}`, async () => {
			const props = fixture(schema(type));
			const screen = await render(Harness, props);
			const finish = await startPaste(props.fields[0]!, "field");
			if (transition === "reset")
				props.form.reset({ items: type === "group" ? { title: "Replacement" } : [] }, props.fields);
			if (transition === "blocked") props.form.writeBlocked = true;
			if (transition === "unmounted") await screen.rerender({ visible: false });
			const before = props.form.snapshot();
			await finish();
			expect(props.form.snapshot()).toEqual(before);
			await screen.unmount();
			props.runtime.dispose();
		});
	}
}

it("pasting a group replaces its editors even when the pasted value is unchanged", async () => {
	const field = schema("group");
	field.nested!.fields = [
		{
			id: "data",
			name: "data",
			path: "items.data",
			type: "json",
			category: "scalar",
			required: false,
			unique: false,
			admin: { label: "Data" },
		},
	];
	const props = fixture(field);
	props.form.reset({ items: { data: { kept: true } } }, props.fields);
	const screen = await render(Harness, props);
	const editor = screen.getByRole("textbox", { name: "Data", exact: true });
	await editor.fill('{"broken":');
	expect(props.form.pendingEditIssues()).toHaveLength(1);
	// The copy matches the form value; only the editor's unfinished draft differs.
	const copy = createFieldClipboardPayload(field, "field", props.form.get("items"));
	vi.spyOn(navigator.clipboard, "readText").mockResolvedValue(
		"ridu-field-clipboard:" + JSON.stringify(copy)
	);
	await screen.getByRole("button", { name: "Open Items actions", exact: true }).click();
	await page.getByRole("menuitem", { name: "Paste field", exact: true }).click();
	await expect.poll(() => editor.element().textContent).toContain('"kept": true');
	expect(props.form.pendingEditIssues()).toEqual([]);
	expect(props.form.dirty).toBe(false);
	await screen.unmount();
	props.runtime.dispose();
});

it("resolves a nested group field through its retained parent row after reordering", async () => {
	const details = {
		...schema("group"),
		id: "details",
		name: "details",
		path: "items.details",
		admin: { label: "Details" },
	};
	const field = schema();
	field.nested!.fields = [details];
	const props = fixture(field);
	props.form.reset(
		{
			items: [
				{ _key: "a", details: { title: "Alpha" } },
				{ _key: "b", details: { title: "Beta" } },
			],
		},
		props.fields
	);
	const screen = await render(Harness, props);
	const finish = await startPaste(details, "field");
	const rows = props.form.get("items") as Record<string, unknown>[];
	props.form.setRows("items", [rows[1]!, rows[0]!]);
	await finish();
	expect(props.form.get("items.0.details.title")).toBe("Beta");
	expect(props.form.get("items.1.details.title")).toBe("Pasted");
	await screen.unmount();
	props.runtime.dispose();
});
