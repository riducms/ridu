import { expect, it, vi } from "vitest";
import { userEvent } from "vitest/browser";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import type { SchemaField } from "@riducms/protocol";
import { RiduError } from "@riducms/sdk";
import { createAdminClient } from "@admin/core/api/admin-client";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { FormController, FormValidationError } from "@admin/core/forms/form-controller.svelte";
import { FieldEditorBinding } from "@admin/core/forms/field-editor-binding";
import { PluginFieldBinding } from "@admin/core/forms/plugin-field-binding";
import { indexFieldValues } from "@admin/core/forms/form-issue-correlation";
import { draftFixture } from "./draft-fixture";

const Harness = svelte`
	<script>
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import { setAdminI18n } from "@riducms/plugin";
		import FieldLayout from "../../src/fields/field-layout.svelte";
		let { form, fields, runtime, visible = true } = $props();
		setAdminRuntime(runtime); setAdminI18n(runtime.i18n);
	</script>
	{#if visible}<FieldLayout {form} {fields} />{/if}
`;
const Drawer = svelte`
	<script>
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import { setAdminI18n } from "@riducms/plugin";
		import DraftEditor from "../../src/fields/embedded-schema-draft-editor.svelte";
		let { session, options, runtime } = $props();
		setAdminRuntime(runtime); setAdminI18n(runtime.i18n);
	</script>
	<DraftEditor {session} {options} />
`;
function list(
	type: "text-list" | "number-list" = "text-list",
	options: Partial<SchemaField> = {}
): SchemaField {
	return {
		id: "values",
		name: "values",
		path: "values",
		type,
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: "Values" },
		...options,
	};
}
function fixture(field = list(), values: unknown[] = []) {
	const form = new FormController();
	form.reset({ values }, [field]);
	return { form, fields: [field], runtime: new AdminRuntime(createAdminClient()) };
}

it("adds duplicate strings, reorders with Alt+Arrow, preserves focus, and removes without retargeting feedback", async () => {
	const props = fixture(list("text-list", { list: { maxRows: 3 } }), ["oak", "oak"]);
	const screen = await render(Harness, props);
	await screen.getByRole("button", { name: "Add item", exact: true }).click();
	const third = screen.getByRole("textbox", { name: "Values, item 3", exact: true });
	await expect.element(third).toHaveFocus();
	await third.fill("warranty");
	await expect
		.element(screen.getByRole("button", { name: "Add item", exact: true }))
		.toBeDisabled();
	await userEvent.keyboard("{Alt>}{ArrowUp}{/Alt}");
	await expect
		.element(screen.getByRole("textbox", { name: "Values, item 2", exact: true }))
		.toHaveFocus();
	expect(props.form.get("values")).toEqual(["oak", "warranty", "oak"]);
	props.form.issues = [{ code: "test", path: "values", message: "Item 2 rejected" }];
	await expect.element(screen.getByText("Item 2 rejected")).toBeVisible();
	await screen.getByRole("button", { name: "Remove item 2", exact: true }).click();
	expect(props.form.issues).toEqual([]);
	await expect
		.element(screen.getByRole("textbox", { name: "Values, item 2", exact: true }))
		.toHaveFocus();
	expect(props.form.get("values")).toEqual(["oak", "oak"]);
	await screen.getByRole("button", { name: "Remove item 2", exact: true }).click();
	await screen.getByRole("button", { name: "Remove item 1", exact: true }).click();
	await expect.element(screen.getByRole("button", { name: "Add item", exact: true })).toHaveFocus();
	await expect.element(screen.getByText("No items yet.")).toBeVisible();
	await screen.unmount();
});

it("retains incomplete numeric input across unmount, blocks submit, and saves exact finite primitive values", async () => {
	const props = fixture(list("number-list"), [0, 8]);
	const screen = await render(Harness, props);
	await screen.getByRole("textbox", { name: "Values, item 2", exact: true }).fill("1e-");
	expect(props.form.get("values")).toEqual([0, "1e-"]);
	await screen.rerender({ visible: false });
	const save = vi.fn(async (values) => values);
	await expect(props.form.submit(props.fields, false, save)).rejects.toBeInstanceOf(
		FormValidationError
	);
	expect(save).not.toHaveBeenCalled();
	await screen.rerender({ visible: true });
	const input = screen.getByRole("textbox", { name: "Values, item 2", exact: true });
	await expect.element(input).toHaveValue("1e-");
	await expect.element(input).toHaveAttribute("aria-invalid", "true");
	await input.fill("1e-2");
	await expect(props.form.submit(props.fields, false, save)).resolves.toEqual({
		values: [0, 0.01],
	});
	await screen.unmount();
});

it("read-only and access-restricted list controls cannot mutate values", async () => {
	const props = fixture(list("text-list", { admin: { label: "Values", readOnly: true } }), ["oak"]);
	const screen = await render(Harness, props);
	await expect
		.element(screen.getByRole("textbox", { name: "Values, item 1", exact: true }))
		.toHaveAttribute("readonly");
	for (const name of ["Add item", "Remove item 1", "Move item 1 down"])
		await expect.element(screen.getByRole("button", { name, exact: true })).toBeDisabled();
	await screen.rerender({ fields: [{ ...props.fields[0]!, admin: { label: "Values" } }] });
	props.form.writeBlocked = true;
	await expect
		.element(screen.getByRole("button", { name: "Add item", exact: true }))
		.toBeDisabled();
	await screen.unmount();
});

it("list requiredness describes the container rather than a native input's contents", async () => {
	const props = fixture(list("text-list", { required: true }), [""]);
	const binding = new FieldEditorBinding(props.form, () => props.fields[0]!, "text-list");
	expect(binding.inputProps.required).toBe(false);
	await expect(props.form.submit(props.fields, false, async (values) => values)).resolves.toEqual({
		values: [""],
	});
	binding.destroy();
});

it("local and paired bindings enforce list types and detach read/write arrays", () => {
	const props = fixture(list("number-list"), [0, 8]);
	const local = new FieldEditorBinding(props.form, () => props.fields[0]!, "number-list");
	const input = [2, 2];
	local.set(input);
	input[0] = 100;
	const read = local.value!;
	read.push(3);
	expect(props.form.get("values")).toEqual([2, 2]);
	const paired = new PluginFieldBinding(props.form, () => props.fields[0]!, {
		decodeValue(value) {
			if (
				!Array.isArray(value) ||
				!value.every((item) => typeof item === "number" && Number.isFinite(item))
			)
				throw new Error("Expected finite numeric items");
			return value as number[];
		},
	});
	paired.set([0, 0]);
	props.form.set("values", [0, "-"]);
	expect(() => local.value).toThrow("array of numbers");
	expect(() => paired.value).toThrow("Expected finite numeric items");
	expect(() => local.set([NaN])).toThrow("array of numbers");
	expect(() => local.set(Array<number>(2))).toThrow("array of numbers");
	local.destroy();
	paired.destroy();
});

it("embedded Apply validates list drafts and Cancel leaves primitive payloads untouched", async () => {
	const source = draftFixture([list("number-list")], { values: [0, 8] });
	const runtime = new AdminRuntime(createAdminClient());
	const draft = source.authoring.beginSchemaDraft!({ treeKey: "widgets", identity: "a" });
	const onApply = vi.fn();
	const onCancel = vi.fn();
	const screen = await render(Drawer, {
		session: source.sessions[0],
		runtime,
		options: { draft, onApply, onCancel },
	});
	const second = screen.getByRole("textbox", { name: "Values, item 2", exact: true });
	await second.fill("-");
	await screen.getByRole("button", { name: "Apply", exact: true }).click();
	expect(onApply).not.toHaveBeenCalled();
	await expect.element(second).toHaveAttribute("aria-invalid", "true");
	await second.fill("10");
	await screen.getByRole("button", { name: "Apply", exact: true }).click();
	expect(onApply).toHaveBeenCalledWith(expect.objectContaining({ values: [0, 10] }));
	expect(source.form.get("body.outline.0.content.values")).toEqual([0, 8]);
	await screen.unmount();
	const cancelDraft = source.authoring.beginSchemaDraft!({ treeKey: "widgets", identity: "a" });
	const cancelScreen = await render(Drawer, {
		session: source.sessions.at(-1),
		runtime,
		options: { draft: cancelDraft, onApply, onCancel },
	});
	await cancelScreen.getByRole("button", { name: "Add item", exact: true }).click();
	await cancelScreen.getByRole("button", { name: "Cancel", exact: true }).click();
	expect(source.form.get("body.outline.0.content.values")).toEqual([0, 8]);
	await cancelScreen.unmount();
	source.binding.destroy();
});

for (const change of ["edit", "remove", "reorder", "edit and undo", "duplicate swap"] as const)
	it(`drops stale pending positional item feedback after list ${change}, preserving enclosing row identity`, async () => {
		const child = list();
		const field: SchemaField = {
			...list(),
			id: "rows",
			name: "rows",
			path: "rows",
			type: "array",
			category: "nested",
			nested: { fields: [child] },
		};
		const form = new FormController();
		form.reset(
			{
				rows: [
					{
						_key: "a",
						values: change === "duplicate swap" ? ["same", "same"] : ["first", "second"],
					},
					{ _key: "b", values: ["other"] },
				],
			},
			[field]
		);
		form.setLocalization("fr");
		const target = indexFieldValues([field], form.snapshot()).find(
			(location) => location.path === "rows.0.values"
		)!.token;
		let reject!: (error: unknown) => void;
		const pending = form
			.submit(
				[field],
				false,
				() =>
					new Promise((_resolve, rejectRequest) => {
						reject = rejectRequest;
					})
			)
			.catch((error) => error);
		form.setRows("rows", (form.snapshot().rows as Record<string, unknown>[]).toReversed());
		form.set(
			"rows.1.values",
			change === "edit" || change === "edit and undo"
				? ["changed", "second"]
				: change === "remove"
					? ["second"]
					: change === "duplicate swap"
						? ["same", "same"].toReversed()
						: ["second", "first"]
		);
		if (change === "edit and undo") form.set("rows.1.values", ["first", "second"]);
		reject(
			new RiduError({
				code: "validation",
				status: 422,
				message: "Invalid list",
				issues: [
					{ path: "rows.0.values", code: "item", message: "Item 1 invalid", target, locale: "fr" },
				],
			})
		);
		await pending;
		expect(form.issues).toEqual([]);
	});

for (const kind of ["array", "blocks", "embedded"] as const)
	for (const transition of [
		"reorder",
		"delete and reinsert",
		"locale",
		...(kind === "blocks" ? ["replace case"] : []),
	])
		it(`primitive list feedback follows ${kind} occurrence through ${transition}`, async () => {
			const child = list();
			const rows = [
				{ _key: "a", blockType: "card", values: ["same", "same"] },
				{ _key: "b", blockType: "card", values: ["other"] },
			];
			const embedded =
				kind === "embedded" ? draftFixture([child], { values: rows[0]!.values }) : undefined;
			const field: SchemaField = embedded?.schema ?? {
				...list(),
				id: "rows",
				name: "rows",
				path: "rows",
				type: kind === "array" ? "array" : "blocks",
				category: "nested",
				...(kind === "array"
					? { nested: { fields: [child] } }
					: {
							blocks: {
								types: [
									{ slug: "card", labels: { singular: "Card", plural: "Cards" }, fields: [child] },
									{ slug: "note", labels: { singular: "Note", plural: "Notes" }, fields: [child] },
								],
							},
						}),
			};
			const form = embedded?.form ?? new FormController();
			if (!embedded) form.reset({ rows }, [field]);
			const write = (items: typeof rows) =>
				embedded
					? form.setEmbedded(field, {
							outline: items.map((row) => ({
								kind: "widget",
								content: { schema: "card", uid: row._key, values: row.values },
							})),
						})
					: form.setRows("rows", items);
			write(rows);
			form.setLocalization("fr");
			const path = (index: number) =>
				embedded ? `body.outline.${index}.content.values` : `rows.${index}.values`;
			const target = indexFieldValues([field], form.snapshot()).find(
				(value) => value.path === path(0)
			)!.token;
			let reject!: (error: unknown) => void;
			const pending = form
				.submit(
					[field],
					false,
					() =>
						new Promise((_resolve, rejectRequest) => {
							reject = rejectRequest;
						})
				)
				.catch((error) => error);
			if (transition === "reorder") write(rows.toReversed());
			else if (transition === "delete and reinsert") {
				write([rows[1]!]);
				write(rows);
			} else if (transition === "replace case") {
				write([{ ...rows[0]!, blockType: "note" }, rows[1]!]);
				write(rows);
			} else {
				form.setLocalization("en");
				form.setLocalization("fr");
			}
			reject(
				new RiduError({
					code: "validation",
					status: 422,
					message: "Invalid list",
					issues: [
						{ path: path(0), code: "max_length", message: "Item 1 invalid", target, locale: "fr" },
					],
				})
			);
			await pending;
			expect(form.issues.map((issue) => issue.path)).toEqual(
				transition === "reorder" ? [path(1)] : []
			);
			embedded?.binding.destroy();
		});
