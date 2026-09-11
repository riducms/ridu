import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { tick } from "svelte";
import { defineAdminPlugin, definePluginField } from "@riducms/plugin/authoring/v1";
import type { EmbeddedSchemaDraft } from "@riducms/plugin";
import type {
	AccessCapabilitiesEnvelope,
	FieldCapabilities,
	SchemaBlockType,
	SchemaEmbeddedTree,
	SchemaField,
} from "@riducms/protocol";
import { createAdminClient } from "@admin/core/api/admin-client";
import { FieldEditorBinding } from "@admin/core/forms/field-editor-binding";
import { FormController } from "@admin/core/forms/form-controller.svelte";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { scopeRepeatedRowField } from "@admin/fields/nested/scoped-field";

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

const HeaderConsumer = svelte`
	<script module>
		export const drafts = [];
		export const changes = [];
	</script>
	<script>
		let { field, authoring } = $props();
		const scopes = [
			{ treeKey: "alpha", identity: "same", label: "Alpha" },
			{ treeKey: "beta", identity: "same", label: "Beta" },
		];
		function open(scope) {
			drafts.push(authoring.beginSchemaDraft(scope));
		}
		function changed(scope, change) {
			changes.push({ treeKey: scope.treeKey, ...change });
		}
	</script>
	{#each scopes as scope (scope.treeKey)}
		<section data-tree={scope.treeKey}>
			<h2>{scope.label}</h2>
			{#if authoring.schemaHeader !== undefined}
				{@render authoring.schemaHeader({ ...scope, onChange: (change) => changed(scope, change) })}
			{/if}
			<button type="button" onclick={() => open(scope)}>Open {scope.label} draft</button>
		</section>
	{/each}
`;
const { drafts: headerDrafts, changes: headerChanges } = HeaderConsumer as unknown as {
	drafts: EmbeddedSchemaDraft[];
	changes: { treeKey: string; field: string; value: string }[];
};

function text(name: string, options: Partial<SchemaField> = {}): SchemaField {
	return {
		id: `named-${name}`,
		name,
		path: `layout.${name}`,
		type: "text",
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: name === "name" ? "Block name" : name === "summary" ? "Summary" : "Enabled" },
		text: {},
		...options,
	};
}

function checkbox(name: string): SchemaField {
	return {
		id: `named-${name}`,
		name,
		path: `layout.${name}`,
		type: "checkbox",
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: "Enabled" },
	};
}

function block(
	options: {
		name?: Partial<SchemaField>;
		summary?: Partial<SchemaField>;
		slug?: string;
	} = {}
): SchemaBlockType {
	return {
		slug: options.slug ?? "card",
		labels: { singular: "Card", plural: "Cards" },
		admin: { nameField: "name", rowLabel: "summary" },
		fields: [text("name", options.name), text("summary", options.summary), checkbox("enabled")],
	};
}

function blocks(types = [block()]): SchemaField {
	return {
		id: "layout",
		name: "layout",
		path: "layout",
		type: "blocks",
		category: "nested",
		required: false,
		unique: false,
		admin: { label: "Layout" },
		nested: { fields: [] },
		blocks: { types },
	};
}

function embeddedNameBlock(treeKey: string): SchemaBlockType {
	return {
		slug: "card",
		labels: { singular: "Card", plural: "Cards" },
		admin: { nameField: "name" },
		fields: [
			{
				...text("name"),
				id: `${treeKey}-name`,
				path: `body.${treeKey}.widget.card.name`,
			},
		],
	};
}

function embeddedNameSchema(): SchemaField {
	const tree = (key: string): SchemaEmbeddedTree => ({
		version: 1,
		key,
		root: [key],
		children: "items",
		tag: "kind",
		cases: [
			{
				tagValue: "widget",
				payload: "content",
				discriminator: "schema",
				identity: "uid",
				types: [embeddedNameBlock(key)],
			},
		],
	});
	return {
		id: "body",
		name: "body",
		path: "body",
		type: "plugin",
		category: "plugin",
		required: false,
		unique: false,
		admin: { label: "Body" },
		plugin: { key: "header-test", config: {}, embeddedTrees: [tree("alpha"), tree("beta")] },
	};
}

function headerRuntime() {
	return new AdminRuntime(createAdminClient(), {
		plugins: [
			defineAdminPlugin({
				key: "header-test",
				pairingVersion: 1,
				fields: {
					"header-test": definePluginField({
						component: HeaderConsumer,
						decodeValue(value: unknown) {
							if (typeof value !== "object" || value === null || Array.isArray(value))
								throw new Error("Expected the header test envelope");
							return value;
						},
					}),
				},
			}),
		],
	});
}

function runtime() {
	return new AdminRuntime(createAdminClient());
}

function fixture(values: Record<string, unknown>[], field = blocks()) {
	const form = new FormController();
	form.reset({ layout: values }, [field]);
	return { form, fields: [field], runtime: runtime() };
}

function row(key: string, values: Record<string, unknown> = {}) {
	return { _key: key, blockType: "card", ...values };
}

function fieldCapability(read: boolean, update = read): FieldCapabilities {
	return { read, create: update, update };
}

function access(fields: Record<string, FieldCapabilities>): AccessCapabilitiesEnvelope {
	return {
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
		fields,
	};
}

function nameInputs() {
	return [...document.querySelectorAll<HTMLInputElement>('input[name$=".name"]')];
}

function inputValues() {
	return [...document.querySelectorAll<HTMLInputElement>("input")].map((input) => input.value);
}

it("renders the configured name once in the header and never stores its placeholder", async () => {
	const props = fixture([row("one", { summary: "Summary fallback" })]);
	const screen = await render(Harness, props);
	const input = screen.getByRole("textbox", { name: "Block name", exact: true });
	await expect.element(input).toHaveAttribute("placeholder", "Summary fallback");
	expect(nameInputs()).toHaveLength(1);
	expect(props.form.get("layout.0.name")).toBeUndefined();
	expect(props.form.snapshot()).toEqual({
		layout: [row("one", { summary: "Summary fallback" })],
	});
	await input.fill("Homepage hero");
	expect(props.form.get("layout.0.name")).toBe("Homepage hero");
	expect(nameInputs()).toHaveLength(1);
	await screen.unmount();
});

it("refreshes keyed name bindings without reading destroyed schema derivations", async () => {
	const props = fixture([row("one", { name: "Saved name" })]);
	const lifecycleWarnings: string[] = [];
	const warnings = vi.spyOn(console, "warn").mockImplementation((...parts) => {
		if (parts.some((part) => String(part).includes("derived_inert")))
			lifecycleWarnings.push(new Error("derived_inert").stack ?? "missing warning stack");
	});
	const screen = await render(Harness, props);
	const input = screen.getByRole("textbox", { name: "Block name", exact: true });
	await input.fill("Unsaved name");
	let currentFields = props.fields;

	for (const singular of ["Refreshed card", "Refreshed card again"]) {
		const nextField = blocks([{ ...block(), labels: { singular, plural: `${singular}s` } }]);
		props.runtime.manifestRevision += 1;
		props.form.reconcile(currentFields, [nextField]);
		currentFields = [nextField];
		await screen.rerender({ ...props, fields: currentFields });
		await expect.element(input).toHaveValue("Unsaved name");
		expect(props.form.dirty).toBe(true);
	}

	await screen.unmount();
	warnings.mockRestore();
	expect(lifecycleWarnings).toEqual([]);
});

it("keeps hidden and denied names and summaries out of shared block occurrences", async () => {
	const hidden = block({
		slug: "hidden",
		name: { admin: { label: "Block name", hidden: true } },
		summary: { admin: { label: "Summary", hidden: true } },
	});
	const field = blocks([block(), hidden]);
	const props = fixture(
		[
			row("visible", { name: "Visible name", summary: "Visible summary" }),
			row("denied", { name: "Denied name", summary: "Denied summary" }),
			{ _key: "hidden", blockType: "hidden", name: "Hidden name", summary: "Hidden summary" },
		],
		field
	);
	props.form.setAccess(
		access({
			"layout.0.name": fieldCapability(true),
			"layout.0.summary": fieldCapability(true),
			"layout.1.name": fieldCapability(false),
			"layout.1.summary": fieldCapability(false),
		}),
		"update"
	);
	const screen = await render(Harness, props);
	await expect
		.element(screen.getByRole("textbox", { name: "Block name", exact: true }))
		.toHaveValue("Visible name");
	await expect.element(screen.getByRole("button", { name: /Denied name/ })).not.toBeInTheDocument();
	await expect.element(screen.getByRole("button", { name: /Hidden name/ })).not.toBeInTheDocument();
	expect(nameInputs().map((input) => input.value)).toEqual(["Visible name"]);
	expect(inputValues()).not.toContain("Denied summary");
	expect(inputValues()).not.toContain("Hidden summary");
	expect(document.body.textContent).not.toContain("Denied summary");
	expect(document.body.textContent).not.toContain("Hidden summary");

	props.form.setAccess(
		access({
			"layout.0.name": fieldCapability(false),
			"layout.0.summary": fieldCapability(false),
			"layout.1.name": fieldCapability(true),
			"layout.1.summary": fieldCapability(true),
		}),
		"update"
	);
	await expect
		.element(screen.getByRole("textbox", { name: "Block name", exact: true }))
		.toHaveValue("Denied name");
	await expect
		.element(screen.getByRole("button", { name: /Visible name/ }))
		.not.toBeInTheDocument();
	expect(nameInputs().map((input) => input.value)).toEqual(["Denied name"]);
	expect(inputValues()).not.toContain("Visible summary");
	expect(inputValues()).not.toContain("Hidden summary");
	expect(document.body.textContent).not.toContain("Visible summary");
	expect(document.body.textContent).not.toContain("Hidden summary");
	await screen.unmount();
});

it("applies name conditions and current child and ancestor write guards", async () => {
	const conditionalName = text("name", {
		admin: {
			label: "Block name",
			condition: {
				kind: "predicate",
				predicate: {
					scope: "sibling",
					path: "enabled",
					operator: "equals",
					values: [{ type: "boolean", value: "true" }],
				},
			},
		},
	});
	const named = {
		...block(),
		fields: [conditionalName, text("summary"), checkbox("enabled")],
	};
	const props = fixture([row("one", { name: "Conditional", enabled: false })], blocks([named]));
	const screen = await render(Harness, props);
	const input = screen.getByRole("textbox", { name: "Block name", exact: true });
	await expect.element(input).not.toBeInTheDocument();
	props.form.set("layout.0.enabled", true);
	await expect.element(input).toHaveValue("Conditional");

	props.form.setAccess(
		access({
			layout: fieldCapability(true, false),
			"layout.0.name": fieldCapability(true),
		}),
		"update"
	);
	await expect.element(input).toHaveAttribute("readonly");
	props.form.setAccess(
		access({
			layout: fieldCapability(true),
			"layout.0.name": fieldCapability(true, false),
		}),
		"update"
	);
	await expect.element(input).toHaveAttribute("readonly");
	props.form.setAccess(undefined, "update");
	await expect.element(input).not.toHaveAttribute("readonly");
	const readOnlyField = {
		...props.fields[0]!,
		admin: { ...props.fields[0]!.admin, readOnly: true },
	};
	await screen.rerender({ ...props, fields: [readOnlyField] });
	await expect.element(input).toHaveAttribute("readonly");
	await screen.rerender(props);
	await expect.element(input).not.toHaveAttribute("readonly");
	props.form.writeBlocked = true;
	await expect.element(input).toHaveAttribute("readonly");
	await screen.unmount();
});

it("focuses an inline name issue while its block body remains collapsed", async () => {
	const props = fixture([row("one", { name: "Invalid", summary: "Details" })]);
	const screen = await render(Harness, props);
	props.form.issues = [{ path: "layout.0.name", code: "name", message: "Name is required" }];
	const collapse = screen.getByRole("button", { name: "Collapse", exact: true });
	await collapse.click();
	const expand = screen.getByRole("button", { name: "Expand", exact: true });
	await expect.element(expand).toHaveAttribute("aria-expanded", "false");
	await screen
		.getByRole("button", { name: "1 validation errors: Name is required", exact: true })
		.click();
	await expect
		.element(screen.getByRole("textbox", { name: "Block name", exact: true }))
		.toHaveFocus();
	await expect.element(expand).toHaveAttribute("aria-expanded", "false");
	await screen.unmount();
});

it("keeps a header occurrence stable through reorder and replaces it after removal or locale reset", async () => {
	const props = fixture([row("first", { name: "First" }), row("second", { name: "Second" })]);
	const screen = await render(Harness, props);
	await expect
		.element(screen.getByRole("textbox", { name: "Block name", exact: true }).nth(0))
		.toHaveValue("First");
	const first = nameInputs()[0]!;
	props.form.setRows(
		"layout",
		(props.form.snapshot().layout as Record<string, unknown>[]).toReversed()
	);
	first.value = "Delayed first";
	first.dispatchEvent(new Event("input", { bubbles: true }));
	expect(props.form.get("layout.1.name")).toBe("Delayed first");
	expect(props.form.get("layout.0.name")).toBe("Second");
	await tick();
	expect(nameInputs()[1]).toBe(first);

	props.form.setRows("layout", [row("second", { name: "Second" })]);
	props.form.setRows("layout", [
		row("second", { name: "Second" }),
		row("first", { name: "Replacement" }),
	]);
	await tick();
	const replacement = nameInputs()[1]!;
	expect(replacement).not.toBe(first);
	expect(first.isConnected).toBe(false);

	props.form.setLocalization("fr");
	await tick();
	const localized = nameInputs()[1]!;
	expect(localized).not.toBe(replacement);
	expect(replacement.isConnected).toBe(false);
	await screen.unmount();
});

it("locks only the matching public schema header and releases callbacks with its field host", async () => {
	headerDrafts.length = 0;
	headerChanges.length = 0;
	const schema = embeddedNameSchema();
	const original = {
		body: {
			alpha: [{ kind: "widget", content: { schema: "card", uid: "same", name: "Alpha" } }],
			beta: [{ kind: "widget", content: { schema: "card", uid: "same", name: "Beta" } }],
		},
	};
	const form = new FormController();
	form.reset(original, [schema]);
	const props = { form, fields: [schema], runtime: headerRuntime(), visible: true };
	const screen = await render(Harness, props);
	const alpha = screen.getByRole("textbox", { name: "Block name", exact: true }).nth(0);
	const beta = screen.getByRole("textbox", { name: "Block name", exact: true }).nth(1);
	await expect.element(alpha).toHaveValue("Alpha");
	await expect.element(beta).toHaveValue("Beta");

	await alpha.fill("Callback only");
	expect(headerChanges).toEqual([{ treeKey: "alpha", field: "name", value: "Callback only" }]);
	expect(form.snapshot()).toEqual(original);

	await screen.getByRole("button", { name: "Open Alpha draft", exact: true }).click();
	await expect.element(alpha).toBeDisabled();
	await expect.element(beta).not.toBeDisabled();
	expect(headerDrafts).toHaveLength(1);
	await beta.fill("Independent beta");
	expect(headerChanges.at(-1)).toEqual({
		treeKey: "beta",
		field: "name",
		value: "Independent beta",
	});
	expect(form.snapshot()).toEqual(original);

	const detachedBeta = nameInputs()[1]!;
	const changeCount = headerChanges.length;
	await screen.rerender({ ...props, visible: false });
	await tick();
	expect(nameInputs()).toHaveLength(0);
	expect(headerDrafts[0]!.stale).toBe(true);
	detachedBeta.value = "After host removal";
	detachedBeta.dispatchEvent(new Event("input", { bubbles: true }));
	expect(headerChanges).toHaveLength(changeCount);
	expect(form.snapshot()).toEqual(original);
	await screen.unmount();
});

it("checks occurrence lifetime and access before invoking a header apply callback", () => {
	const field = blocks();
	const form = new FormController();
	form.reset({ layout: [row("first", { name: "First" }), row("second", { name: "Second" })] }, [
		field,
	]);
	const schema = scopeRepeatedRowField(block().fields[0]!, "layout.0", "first");
	const apply = vi.fn();
	const binding = new FieldEditorBinding(
		form,
		() => schema,
		"text",
		() => form.editorEpoch,
		apply
	);
	const delayed = binding.set;
	form.setRows("layout", (form.snapshot().layout as Record<string, unknown>[]).toReversed());
	delayed("Moved first");
	expect(apply).toHaveBeenLastCalledWith("Moved first");
	form.setAccess(access({ "layout.1.name": fieldCapability(true, false) }), "update");
	expect(() => delayed("Denied")).toThrow("read-only");
	expect(apply).toHaveBeenCalledTimes(1);
	form.setRows("layout", [row("second", { name: "Second" })]);
	expect(() => delayed("Removed")).toThrow("stale");
	expect(apply).toHaveBeenCalledTimes(1);
	binding.destroy();
});
