import { expect, it } from "vitest";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { defineFieldEditor, type FieldBinding } from "@riducms/plugin/editor";
import { RiduError } from "@riducms/sdk";
import { defineRowLabel } from "@riducms/plugin/admin";
import type { SchemaField } from "@riducms/protocol";
import { createAdminClient } from "@admin/core/api/admin-client";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { FormController } from "@admin/core/forms/form-controller.svelte";
import { FieldEditorBinding } from "@admin/core/forms/field-editor-binding";
import { indexFieldValues } from "@admin/core/forms/form-issue-correlation";
import { draftFixture, title } from "./draft-fixture";

const Editor = svelte`
	<script module>export const bindings = [];</script>
	<script>
		import Field from "@riducms/plugin/editor/field";
		let { field, config } = $props();
		bindings.push(field);
	</script>
	<Field {field}>
		<input {...field.inputProps} value={field.value ?? ""} readonly={field.readOnly}
			data-editor-config={config.prefix} oninput={event => field.set(event.currentTarget.value)} />
	</Field>
`;
const PlainEditor = svelte`
	<script module>export const bindings = [];</script>
	<script>
		import Field from "@riducms/plugin/editor/field";
		let { field, ...rest } = $props();
		bindings.push(field);
	</script>
	<Field {field}>
		<input {...field.inputProps} value={field.value ?? ""} readonly={field.readOnly}
			data-has-config={"config" in rest} oninput={event => field.set(event.currentTarget.value)} />
	</Field>
`;
const PlainLabel = svelte`
	<script module>export const mounts = [];</script>
	<script>
		let { row, ...rest } = $props();
		mounts.push(row);
	</script>
	<span data-plain-row={row._key} data-has-config={"config" in rest}>{row.title}</span>
`;
const Label = svelte`
	<script module>export const mounts = [];</script>
	<script>
		import { onDestroy } from "svelte";
		let { row, rowNumber, config } = $props();
		const mount = { get row() { return row; }, get number() { return rowNumber; }, destroyed: false };
		mounts.push(mount);
		onDestroy(() => mount.destroyed = true);
	</script>
	<span data-row-label={row._key}>{config.prefix}: {row.title}</span>
`;
const { bindings } = Editor as unknown as { bindings: FieldBinding<"text">[] };
const { bindings: plainBindings } = PlainEditor as unknown as { bindings: FieldBinding<"text">[] };
const { mounts: plainMounts } = PlainLabel as unknown as { mounts: Record<string, unknown>[] };
const { mounts } = Label as unknown as {
	mounts: { row: Record<string, unknown>; number: number; destroyed: boolean }[];
};
const Harness = svelte`
	<script>
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import { setAdminI18n } from "@riducms/plugin";
		import FieldLayout from "../../src/fields/field-layout.svelte";
		let { form, fields, runtime } = $props();
		setAdminRuntime(runtime);
		setAdminI18n(runtime.i18n);
	</script>
	<FieldLayout {form} {fields} />
`;
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
const ErrorBoundary = svelte`
	<script>let { Host, hostProps } = $props();</script>
	<svelte:boundary>
		<Host {...hostProps} />
		{#snippet failed(error)}
			<p role="alert">{error instanceof Error ? error.message : String(error)}</p>
		{/snippet}
	</svelte:boundary>
`;
function runtime() {
	return new AdminRuntime(createAdminClient(), {
		...{
			rowLabels: {
				"app:plainRow": defineRowLabel({ component: PlainLabel }),
				"app:row": defineRowLabel({
					component: Label,
					decodeConfig(value: unknown) {
						if (
							typeof value !== "object" ||
							value === null ||
							!("prefix" in value) ||
							typeof value.prefix !== "string"
						)
							throw new Error("prefix required");
						return { prefix: value.prefix };
					},
				}),
			},
		},
		plugins: [],
		fields: {
			"app:plain": defineFieldEditor({ type: "text", component: PlainEditor }),
			"app:accent": defineFieldEditor({
				type: "text",
				component: Editor,
				decodeConfig(value: unknown) {
					if (
						typeof value !== "object" ||
						value === null ||
						!("prefix" in value) ||
						typeof value.prefix !== "string"
					)
						throw new Error("prefix required");
					return { prefix: value.prefix };
				},
			}),
		},
	});
}
function accent(path: string, label = "Accent"): SchemaField {
	return {
		...title,
		id: path,
		name: path.split(".").at(-1)!,
		path,
		admin: { label, editor: { reference: "app:accent", config: { prefix: "Configured" } } },
	};
}
function rows(type: "array" | "blocks" = "array"): SchemaField {
	const children = [accent("rows.title", "Row title")];
	return {
		...title,
		id: "rows",
		name: "rows",
		path: "rows",
		type,
		category: "nested",
		required: false,
		admin: { label: "Rows" },
		nested: {
			fields: type === "array" ? children : [],
			rowLabelComponent: { reference: "app:row", config: { prefix: "Configured row" } },
		},
		...(type === "blocks"
			? {
					blocks: {
						types: [
							{ slug: "card", labels: { singular: "Card", plural: "Cards" }, fields: children },
							{ slug: "other", labels: { singular: "Other", plural: "Others" }, fields: children },
						],
					},
				}
			: {}),
	};
}

it("one configured editor works at root, group and localized fields and obeys ancestor state", async () => {
	bindings.length = 0;
	const fields: SchemaField[] = [
		accent("accent", "Root accent"),
		{
			...title,
			id: "meta",
			name: "meta",
			path: "meta",
			type: "group",
			category: "nested",
			admin: { label: "Meta" },
			nested: { fields: [{ ...accent("meta.accent", "Localized accent"), localized: true }] },
		},
	];
	const form = new FormController();
	form.reset({ accent: "Root", meta: { accent: "Français" } }, fields);
	form.setLocalization("fr");
	const screen = await render(Harness, { form, fields, runtime: runtime() });
	await expect.element(screen.getByRole("textbox", { name: "Root accent" })).toHaveValue("Root");
	await screen.getByRole("textbox", { name: /Localized accent/ }).fill("Bleu");
	expect(form.get("meta.accent")).toBe("Bleu");
	expect(bindings[1]!.inputProps.required).toBe(true);
	fields[1]!.admin.readOnly = true;
	expect(() => bindings[1]!.set("Blocked")).toThrow("read-only");
	fields[1]!.admin.readOnly = false;
	form.setLocalization("en");
	expect(bindings.every((binding) => binding.stale)).toBe(true);
	await screen.unmount();
});

for (const type of ["array", "blocks"] as const)
	it(`configured ${type} editors and row labels retain occurrence identity and save issues through reorder`, async () => {
		bindings.length = 0;
		mounts.length = 0;
		const field = rows(type);
		const form = new FormController();
		form.reset(
			{
				rows: [
					{ _key: "a", ...(type === "blocks" ? { blockType: "card" } : {}), title: "First" },
					{ _key: "b", ...(type === "blocks" ? { blockType: "card" } : {}), title: "Second" },
				],
			},
			[field]
		);
		const screen = await render(Harness, { form, fields: [field], runtime: runtime() });
		await expect
			.element(screen.getByRole("textbox", { name: "Row title" }).nth(0))
			.toHaveValue("First");
		const first = bindings[0]!;
		const heading = mounts[0]!;
		form.issues = [
			{
				path: "rows.0.title",
				target: indexFieldValues([field], form.snapshot()).find(
					(item) => item.path === "rows.0.title"
				)!.token,
				code: "sku",
				message: "Reserved SKU",
			},
		];
		form.setRows("rows", (form.snapshot().rows as Record<string, unknown>[]).toReversed());
		await expect
			.element(screen.getByRole("textbox", { name: "Row title" }).nth(1))
			.toHaveValue("First");
		expect(bindings).toHaveLength(2);
		expect(mounts).toHaveLength(2);
		expect(heading.number).toBe(2);
		expect(heading.row._key).toBe("a");
		expect(Object.isFrozen(heading.row)).toBe(true);
		expect(first.issues[0]?.path).toBe("rows.1.title");
		expect(first.inputProps["aria-invalid"]).toBe(true);
		if (type === "blocks") {
			form.setRows("rows", [{ _key: "a", blockType: "other", title: "Replacement" }]);
		} else {
			form.setRows("rows", []);
			form.setRows("rows", [{ _key: "a", title: "Replacement" }]);
		}
		expect(first.stale).toBe(true);
		expect(form.issues).toEqual([]);
		await expect
			.element(screen.getByRole("textbox", { name: "Row title" }))
			.toHaveValue("Replacement");
		expect(heading.destroyed).toBe(true);
		expect(() => first.set("Wrong row")).toThrow("stale");
		await screen.unmount();
		expect(mounts.every((mount) => mount.destroyed)).toBe(true);
	});

it("embedded local editors and row labels follow declared plugin identities rather than envelope indexes", async () => {
	bindings.length = 0;
	mounts.length = 0;
	const fixture = draftFixture([rows()]);
	const widget = (uid: string, title: string) => ({
		kind: "widget",
		content: { schema: "card", uid, rows: [{ _key: "same-child", title }] },
	});
	fixture.form.setEmbedded(fixture.schema, {
		outline: [widget("a", "First"), widget("b", "Second")],
	});
	const screen = await render(EmbeddedHarness, { ...fixture, runtime: runtime() });
	await expect
		.element(screen.getByRole("textbox", { name: "Row title" }).nth(0))
		.toHaveValue("First");
	const first = bindings[0]!;
	const heading = mounts[0]!;
	const body = fixture.form.snapshot().body as { outline: unknown[] };
	fixture.form.setEmbedded(fixture.schema, { outline: body.outline.toReversed() });
	await expect
		.element(screen.getByRole("textbox", { name: "Row title" }).nth(1))
		.toHaveValue("First");
	expect(bindings).toHaveLength(2);
	expect(mounts).toHaveLength(2);
	first.set("Still first");
	expect(fixture.form.get("body.outline.1.content.rows.0.title")).toBe("Still first");
	expect(fixture.form.get("body.outline.0.content.rows.0.title")).toBe("Second");
	fixture.form.setEmbedded(fixture.schema, { outline: [widget("b", "Second")] });
	expect(first.stale).toBe(true);
	await expect.element(screen.getByRole("textbox", { name: "Row title" })).toHaveValue("Second");
	expect(heading.destroyed).toBe(true);
	await screen.unmount();
	fixture.binding.destroy();
});

it("embedded configured editors use the ordinary controller for save issues, detached Apply and Cancel", async () => {
	bindings.length = 0;
	mounts.length = 0;
	const fixture = draftFixture([rows()], {
		rows: [
			{ _key: "a", title: "Original" },
			{ _key: "b", title: "Other" },
		],
	});
	const path = "body.outline.0.content.rows.0.title";
	fixture.form.issues = [
		{
			path,
			target: indexFieldValues([fixture.schema], fixture.form.snapshot()).find(
				(item) => item.path === path
			)!.token,
			code: "sku",
			message: "Reserved embedded SKU",
		},
	];
	const draft = fixture.authoring.beginSchemaDraft!({ treeKey: "widgets", identity: "a" });
	const session = fixture.sessions[0]!;
	const screen = await render(Harness, {
		form: session.form,
		fields: session.fields,
		runtime: runtime(),
	});
	await expect
		.element(screen.getByRole("textbox", { name: "Row title" }).nth(0))
		.toHaveValue("Original");
	const first = bindings[0]!;
	expect(first.issues[0]?.message).toBe("Reserved embedded SKU");
	session.form.setRows(
		`${draft.id}.rows`,
		(draft.payload().rows as Record<string, unknown>[]).toReversed()
	);
	expect(first.issues[0]?.path).toBe(`${draft.id}.rows.1.title`);
	first.set("Applied");
	expect(fixture.form.get(path)).toBe("Original");
	expect(draft.validate()).toBe(true);
	const applied = draft.payload();
	draft.discard();
	expect(first.stale).toBe(true);
	fixture.binding.set({ outline: [{ kind: "widget", content: applied }] });
	expect(fixture.form.get("body.outline.0.content.rows.1.title")).toBe("Applied");
	await screen.unmount();
	const cancel = fixture.authoring.beginSchemaDraft!({ treeKey: "widgets", identity: "a" });
	const cancelSession = fixture.sessions[1]!;
	const cancelEditor = new FieldEditorBinding(
		cancelSession.form,
		() => ({ ...session.fields[0]!.nested!.fields[0]!, path: `${cancel.id}.rows.0.title` }),
		"text"
	);
	cancelEditor.set("Discard me");
	cancel.discard();
	expect(cancelEditor.stale).toBe(true);
	expect(fixture.form.get("body.outline.0.content.rows.0.title")).toBe("Other");
	fixture.binding.destroy();
});

for (const transition of [
	"remove and reuse key",
	"replace and restore Block case",
	"locale replacement",
	"reorder",
] as const)
	it(`inflight save issues respect occurrence lifetime after ${transition}`, async () => {
		const field = rows("blocks");
		const form = new FormController();
		const original = [
			{ _key: "a", blockType: "card", title: "Invalid" },
			{ _key: "b", blockType: "card", title: "Other" },
		];
		form.reset({ rows: original }, [field]);
		const target = indexFieldValues([field], form.snapshot()).find(
			(item) => item.path === "rows.0.title"
		)!.token;
		let reject!: (error: unknown) => void;
		const result = form
			.submit(
				[field],
				false,
				() =>
					new Promise((_resolve, rejectRequest) => {
						reject = rejectRequest;
					})
			)
			.catch((error) => error);
		if (transition === "remove and reuse key") {
			form.setRows("rows", []);
			form.setRows("rows", structuredClone(original));
		} else if (transition === "replace and restore Block case") {
			form.setRows("rows", [{ _key: "a", blockType: "other", title: "Invalid" }]);
			form.setRows("rows", structuredClone(original));
		} else if (transition === "locale replacement") {
			form.setLocalization("fr");
		} else form.setRows("rows", structuredClone(original).toReversed());
		reject(
			new RiduError({
				code: "validation",
				status: 422,
				message: "Invalid",
				issues: [{ path: "rows.0.title", target, code: "sku", message: "Reserved SKU" }],
			})
		);
		await result;
		expect(form.issues.map((issue) => issue.path)).toEqual(
			transition === "reorder" ? ["rows.1.title"] : []
		);
	});

it("removing an initialized group permanently expires its editor while an absent optional group can initialize", () => {
	const field = accent("meta.accent");
	const group: SchemaField = {
		...title,
		id: "meta",
		name: "meta",
		path: "meta",
		type: "group",
		category: "nested",
		required: false,
		admin: { label: "Meta" },
		nested: { fields: [field] },
	};
	for (const initiallyPresent of [true, false]) {
		const form = new FormController();
		form.reset(initiallyPresent ? { meta: { accent: "Original" } } : {}, [group]);
		const binding = new FieldEditorBinding(form, () => field, "text");
		binding.set("Initialized");
		expect(form.get("meta.accent")).toBe("Initialized");
		form.set("meta", null);
		expect(binding.stale).toBe(true);
		form.set("meta", { accent: "Replacement" });
		expect(() => binding.set("Resurrected")).toThrow("stale");
		expect(form.get("meta.accent")).toBe("Replacement");
	}
});

function plain(path: string, label: string): SchemaField {
	return { ...accent(path, label), admin: { label, editor: { reference: "app:plain" } } };
}
function plainRows(type: "array" | "blocks"): SchemaField {
	const field = rows(type);
	const children = [plain("rows.title", "Plain row")];
	return {
		...field,
		nested: {
			fields: type === "array" ? children : [],
			rowLabelComponent: { reference: "app:plainRow" },
		},
		...(type === "blocks"
			? {
					blocks: {
						types: [
							{ slug: "card", labels: { singular: "Card", plural: "Cards" }, fields: children },
						],
					},
				}
			: {}),
	};
}

for (const type of ["array", "blocks"] as const)
	it(`editors without settings receive no config prop at root, localized group and ${type} occurrences`, async () => {
		const fields: SchemaField[] = [
			plain("accent", "Plain root"),
			{
				...title,
				id: "meta",
				name: "meta",
				path: "meta",
				type: "group",
				category: "nested",
				admin: { label: "Meta" },
				nested: { fields: [{ ...plain("meta.accent", "Plain localized"), localized: true }] },
			},
			plainRows(type),
		];
		const form = new FormController();
		form.reset(
			{
				accent: "Root",
				meta: { accent: "Français" },
				rows: [
					{ _key: "one", ...(type === "blocks" ? { blockType: "card" } : {}), title: "Original" },
				],
			},
			fields
		);
		form.setLocalization("fr");
		const screen = await render(Harness, { form, fields, runtime: runtime() });
		for (const label of ["Plain root", "Plain localized", "Plain row"])
			await expect
				.element(screen.getByRole("textbox", { name: new RegExp(label) }))
				.toHaveAttribute("data-has-config", "false");
		await expect
			.element(screen.getByText("Original", { exact: true }))
			.toHaveAttribute("data-has-config", "false");
		await screen.getByRole("textbox", { name: /Plain localized/ }).fill("Bleu");
		await screen.getByRole("textbox", { name: "Plain row" }).fill("Changed");
		expect(form.get("meta.accent")).toBe("Bleu");
		expect(form.get("rows.0.title")).toBe("Changed");
		await screen.unmount();
	});

it("embedded repeated editors and labels need neither settings nor config props", async () => {
	const fixture = draftFixture([plainRows("array")], {
		rows: [{ _key: "one", title: "Embedded" }],
	});
	const screen = await render(EmbeddedHarness, { ...fixture, runtime: runtime() });
	const input = screen.getByRole("textbox", { name: "Plain row" });
	await expect.element(input).toHaveValue("Embedded");
	await expect.element(input).toHaveAttribute("data-has-config", "false");
	await expect
		.element(screen.getByText("Embedded", { exact: true }))
		.toHaveAttribute("data-has-config", "false");
	await input.fill("Updated embedded");
	expect(fixture.form.get("body.outline.0.content.rows.0.title")).toBe("Updated embedded");
	await screen.unmount();
	fixture.binding.destroy();
});

const invalidSettings = [
	{
		name: "malformed config",
		configured: true,
		selection: { config: { prefix: 42 } },
		message: "prefix required",
	},
	{
		name: "expected but absent config",
		configured: true,
		selection: {},
		message: "config is required by decodeConfig; supply a configuration object in Go",
	},
	{
		name: "settings supplied to a component without config",
		configured: false,
		selection: { config: { prefix: "Unexpected" } },
		message:
			"config was supplied but there is no decodeConfig; remove the Go configuration or register a decoder",
	},
] as const;

for (const host of ["ordinary", "embedded"] as const)
	for (const kind of ["editor", "row label"] as const)
		for (const scenario of invalidSettings)
			it(`${host} ${kind} host rejects ${scenario.name} before mounting the component`, async () => {
				bindings.length = 0;
				plainBindings.length = 0;
				mounts.length = 0;
				plainMounts.length = 0;
				const reference =
					kind === "editor"
						? scenario.configured
							? "app:accent"
							: "app:plain"
						: scenario.configured
							? "app:row"
							: "app:plainRow";
				const selection = { reference, ...scenario.selection };
				const field: SchemaField =
					kind === "editor"
						? { ...accent("accent"), admin: { label: "Accent", editor: selection } }
						: {
								...rows(),
								nested: {
									fields: [plain("rows.title", "Plain row")],
									rowLabelComponent: selection,
								},
							};
				const values =
					kind === "editor"
						? { accent: "Original" }
						: { rows: [{ _key: "one", title: "Original" }] };
				const embedded = host === "embedded" ? draftFixture([field], values) : undefined;
				const form = embedded?.form ?? new FormController();
				if (embedded === undefined) form.reset(values, [field]);
				const before = form.snapshot();
				const screen = await render(ErrorBoundary, {
					Host: embedded === undefined ? Harness : EmbeddedHarness,
					hostProps:
						embedded === undefined
							? { form, fields: [field], runtime: runtime() }
							: { ...embedded, runtime: runtime() },
				});
				const path = host === "embedded" ? `body.outline.0.content.${field.name}` : field.name;
				const label = kind === "editor" ? "Editor" : "Row label";
				const guidance =
					kind === "editor"
						? ". Fix the field component config in Go or the decoder in admin/src/admin.config.ts."
						: "";
				await expect
					.element(screen.getByRole("alert"))
					.toHaveTextContent(
						`${label} ${reference} config for ${path} is invalid: ${scenario.message}${guidance}`
					);
				if (kind === "editor") {
					expect(bindings).toHaveLength(0);
					expect(plainBindings).toHaveLength(0);
				} else {
					expect(mounts).toHaveLength(0);
					expect(plainMounts).toHaveLength(0);
				}
				expect(form.snapshot()).toEqual(before);
				await screen.unmount();
				embedded?.binding.destroy();
			});
