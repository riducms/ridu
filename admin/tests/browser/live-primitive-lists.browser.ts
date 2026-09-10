import { afterEach, expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import type { LiveValidationEnvelope, LiveValidationRequest, SchemaField } from "@riducms/protocol";
import { RiduError } from "@riducms/sdk";
import { createAdminClient } from "@admin/core/api/admin-client";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { FormController, FormValidationError } from "@admin/core/forms/form-controller.svelte";
import { FieldEditorBinding } from "@admin/core/forms/field-editor-binding";
import { createEmbeddedSchemaDraft } from "@admin/core/forms/embedded-schema-draft.svelte";
import { indexFieldValues } from "@admin/core/forms/form-issue-correlation";
import { createAdminI18n } from "@riducms/translations";
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
const supplier: SchemaField = {
	id: "supplier",
	name: "supplier",
	path: "supplier",
	type: "text",
	category: "scalar",
	required: false,
	unique: false,
	admin: { label: "Supplier" },
	liveValidation: true,
};
function list(
	type: "text-list" | "number-list" = "text-list",
	options: Partial<SchemaField> = {}
): SchemaField {
	return {
		...supplier,
		id: "values",
		name: "values",
		path: "values",
		type,
		admin: { label: "Values" },
		...options,
	};
}
function deferred<Value>() {
	let resolve!: (value: Value) => void;
	const promise = new Promise<Value>((yes) => {
		resolve = yes;
	});
	return { promise, resolve };
}
function transport(form: FormController) {
	const requests: {
		input: LiveValidationRequest;
		signal: AbortSignal;
		response: ReturnType<typeof deferred<LiveValidationEnvelope>>;
	}[] = [];
	form.configureLiveValidation((input, signal) => {
		const response = deferred<LiveValidationEnvelope>();
		requests.push({ input, signal, response });
		return response.promise;
	});
	return requests;
}
function fixture(
	fields = [supplier, list()],
	values: Record<string, unknown> = { supplier: "acme", values: ["A-1", "A-1"] }
) {
	const form = new FormController();
	form.reset(values, fields);
	form.setResource({ collection: "products", id: "one" });
	return {
		form,
		fields,
		requests: transport(form),
		runtime: new AdminRuntime(createAdminClient()),
	};
}
function answer(
	input: LiveValidationRequest,
	message = "",
	target?: string
): LiveValidationEnvelope {
	return {
		evaluations: input.fields.map((path) => ({
			path,
			status: "checked",
			issues: message
				? [{ path, code: "supplier_values", message, ...(target ? { target } : {}) }]
				: [],
		})),
	};
}
async function settle() {
	await Promise.resolve();
	await Promise.resolve();
	await Promise.resolve();
}
afterEach(() => vi.useRealTimers());

it("checks one opted-in whole list, preserving duplicates, zero, empty strings, and sibling input", async () => {
	vi.useFakeTimers();
	const values = list("text-list", { liveValidation: false });
	const sizes = list("number-list", { id: "sizes", name: "sizes", path: "sizes" });
	const { form, requests } = fixture([supplier, values, sizes], {
		supplier: "acme",
		values: ["", ""],
		sizes: [0, 0],
	});
	await vi.advanceTimersByTimeAsync(1_000);
	form.set("values", ["", ""]);
	await vi.advanceTimersByTimeAsync(1_000);
	expect(requests).toHaveLength(0);
	form.set("sizes", [0, 0]);
	await vi.advanceTimersByTimeAsync(499);
	expect(requests).toHaveLength(0);
	form.liveValidation.flush("sizes");
	expect(requests[0]!.input).toEqual({
		id: "one",
		fields: ["sizes"],
		data: { supplier: "acme", values: ["", ""], sizes: [0, 0] },
	});
	form.set("supplier", "globex");
	form.liveValidation.flush("sizes");
	expect(requests[0]!.signal.aborted).toBe(true);
	expect(requests[1]!.input.data.supplier).toBe("globex");
	expect(requests[1]!.input.fields).toEqual(["supplier", "sizes"]);
	requests[1]!.response.resolve(answer(requests[1]!.input));
	await settle();
	requests[0]!.response.resolve(answer(requests[0]!.input, "Obsolete supplier"));
	await settle();
	expect(form.issuesFor("sizes")).toEqual([]);
	expect(form.liveValidation.forField("sizes").status).toBe("checked");
	form.disposeBindings();
});

it("a real equal-duplicate move cancels positional feedback despite an identical serialized list", async () => {
	const props = fixture();
	const screen = await render(Harness, props);
	props.form.set("values", ["A-1", "A-1"]);
	props.form.liveValidation.flush("values");
	await screen.getByRole("button", { name: "Move item 1 down", exact: true }).click();
	expect(props.form.get("values")).toEqual(["A-1", "A-1"]);
	expect(props.requests[0]!.signal.aborted).toBe(true);
	props.form.liveValidation.flush("values");
	const latest = props.requests.at(-1)!;
	latest.response.resolve(answer(latest.input));
	await settle();
	props.requests[0]!.response.resolve(answer(props.requests[0]!.input, "Item 1 was rejected"));
	await settle();
	expect(props.form.issuesFor("values")).toEqual([]);
	expect(props.form.liveValidation.forField("values").status).toBe("checked");
	await screen.unmount();
	props.form.disposeBindings();
});

for (const change of ["edit", "delete", "reorder", "edit and undo"] as const)
	it(`drops delayed whole-list feedback after ${change}`, async () => {
		const { form, requests } = fixture();
		form.set("values", ["A-1", "A-2"]);
		form.liveValidation.flush("values");
		if (change === "edit") form.set("values", ["A-1", "A-3"]);
		if (change === "delete") form.set("values", ["A-1"]);
		if (change === "reorder") form.set("values", ["A-2", "A-1"]);
		if (change === "edit and undo") {
			form.set("values", ["A-1", "temporary"]);
			form.set("values", ["A-1", "A-2"]);
		}
		form.liveValidation.flush("values");
		expect(requests[0]!.signal.aborted).toBe(true);
		requests[1]!.response.resolve(answer(requests[1]!.input));
		await settle();
		requests[0]!.response.resolve(answer(requests[0]!.input, "Old item 2 issue"));
		await settle();
		expect(form.issuesFor("values")).toEqual([]);
		form.disposeBindings();
	});

it("unfinished numeric list input skips dependent checks through hide/remount and resumes after correction", async () => {
	const props = fixture([supplier, list("number-list")], { supplier: "acme", values: [0, 8] });
	const screen = await render(Harness, props);
	props.form.set("supplier", "globex");
	props.form.liveValidation.flush("supplier");
	const second = screen.getByRole("textbox", { name: "Values, item 2", exact: true });
	await second.fill("1e-");
	expect(props.form.get("values")).toEqual([0, "1e-"]);
	expect(props.requests[0]!.signal.aborted).toBe(true);
	for (const path of ["supplier", "values"]) {
		expect(props.form.liveValidation.forField(path).status).toBe("skipped");
		props.form.liveValidation.flush(path);
	}
	expect(props.requests).toHaveLength(1);
	await screen.rerender({ visible: false });
	props.form.set("supplier", "acme");
	expect(props.form.liveValidation.forField("supplier").status).toBe("skipped");
	await screen.rerender({ visible: true });
	await expect.element(second).toHaveValue("1e-");
	const save = vi.fn(async (values) => values);
	await expect(props.form.submit(props.fields, false, save)).rejects.toBeInstanceOf(
		FormValidationError
	);
	expect(save).not.toHaveBeenCalled();
	await second.fill("1e-2");
	props.form.liveValidation.flush("values");
	expect(props.requests.at(-1)!.input.data).toEqual({ supplier: "acme", values: [0, 0.01] });
	props.requests.at(-1)!.response.resolve(answer(props.requests.at(-1)!.input));
	await settle();
	props.requests[0]!.response.resolve(answer(props.requests[0]!.input, "Old numeric values"));
	await settle();
	expect(props.form.issuesFor("supplier")).toEqual([]);
	await screen.unmount();
	props.form.disposeBindings();
});

it("malformed lists never masquerade as typed values and empty containers remain available", () => {
	const { form, requests } = fixture([list("number-list")], { values: [0] });
	for (const invalid of [
		[""],
		["-"],
		["2."],
		["1e-"],
		["1e9999"],
		[NaN],
		[Infinity],
		[null],
		Array(1),
		"not a list",
	]) {
		form.set("values", invalid);
		form.liveValidation.flush("values");
		expect(form.liveValidation.forField("values").status).toBe("skipped");
	}
	expect(requests).toHaveLength(0);
	form.set("values", []);
	form.liveValidation.flush("values");
	expect(requests[0]!.input.data.values).toEqual([]);
	form.set("values", null);
	form.liveValidation.flush("values");
	expect(requests[1]!.input.data.values).toBe(null);
	form.disposeBindings();
});

for (const kind of ["array", "blocks"] as const)
	it(`${kind} enclosing identity survives reorder, including a literal @locale key, without retargeting list issues`, async () => {
		const child = list();
		const rows: SchemaField = {
			...list(),
			id: "rows",
			name: "rows",
			path: "rows",
			type: kind,
			category: "nested",
			liveValidation: false,
			...(kind === "array"
				? { nested: { fields: [child] } }
				: {
						blocks: {
							types: [
								{ slug: "card", labels: { singular: "Card", plural: "Cards" }, fields: [child] },
								{ slug: "other", labels: { singular: "Other", plural: "Others" }, fields: [child] },
							],
						},
					}),
		};
		const a = {
			_key: "@locale",
			...(kind === "blocks" ? { blockType: "card" } : {}),
			values: ["A", "A"],
		};
		const b = { _key: "b", ...(kind === "blocks" ? { blockType: "card" } : {}), values: ["B"] };
		const { form, requests } = fixture([rows], { rows: [a, b] });
		form.setLocalization("fr");
		const token = indexFieldValues([rows], form.values).find(
			(location) => location.path === "rows.0.values"
		)!.token;
		expect(JSON.parse(token)).toContain("@locale");
		const binding = new FieldEditorBinding(
			form,
			() => ({ ...child, path: "rows.0.values" }),
			"text-list"
		);
		binding.set(["A", "A"]);
		form.liveValidation.flush("rows.0.values");
		form.setRows("rows", [b, a]);
		expect(binding.stale).toBe(false);
		expect(binding.schema.path).toBe("rows.1.values");
		form.liveValidation.flush("rows.1.values");
		expect(requests[0]!.signal.aborted).toBe(true);
		const latest = requests.at(-1)!;
		latest.response.resolve({
			evaluations: latest.input.fields.map((path) => ({
				path,
				status: "checked",
				issues:
					path === "rows.1.values"
						? [
								{
									path,
									target: token,
									locale: "fr",
									code: "list",
									message: "Current list feedback",
								},
							]
						: [],
			})),
		});
		await settle();
		requests[0]!.response.resolve(answer(requests[0]!.input, "Obsolete", token));
		await settle();
		expect(form.issuesFor("rows.0.values")).toEqual([]);
		expect(binding.issues[0]?.message).toBe("Current list feedback");
		binding.set(["pending"]);
		form.liveValidation.flush("rows.1.values");
		const pending = requests.at(-1)!;
		if (kind === "blocks") form.setRows("rows", [b, { ...a, blockType: "other" }]);
		else {
			form.setRows("rows", [b]);
			form.setRows("rows", [b, a]);
		}
		expect(pending.signal.aborted).toBe(true);
		expect(binding.stale).toBe(true);
		pending.response.resolve(answer(pending.input, "Deleted or replaced", token));
		await settle();
		expect(form.issuesFor("rows.1.values")).toEqual([]);
		form.disposeBindings();
	});

it("unfinished numeric lists stay unavailable after enclosing row reorder, and deletion restores checks", () => {
	const rows: SchemaField = {
		...list(),
		id: "rows",
		name: "rows",
		path: "rows",
		type: "array",
		category: "nested",
		liveValidation: false,
		nested: { fields: [list("number-list")] },
	};
	const { form, requests } = fixture([supplier, rows], {
		supplier: "acme",
		rows: [
			{ _key: "a", values: [0] },
			{ _key: "b", values: [1] },
		],
	});
	form.set("supplier", "globex");
	form.liveValidation.flush("supplier");
	form.set("rows.0.values", ["-"]);
	const [a, b] = form.get("rows") as Record<string, unknown>[];
	form.setRows("rows", [b!, a!]);
	expect(form.liveValidation.forField("rows.1.values").status).toBe("skipped");
	expect(form.liveValidation.forField("supplier").status).toBe("skipped");
	expect(requests).toHaveLength(1);
	form.setRows("rows", [b!]);
	form.liveValidation.flush("supplier");
	expect(requests.at(-1)!.input.data.rows).toEqual([b]);
	expect(form.liveValidation.forField("supplier").status).toBe("pending");
	form.disposeBindings();
});

it("custom list bindings retain typed copies, host feedback, and exact-locale lifetimes", async () => {
	const field = list("text-list", { localized: true });
	const { form, requests } = fixture([supplier, field]);
	form.setLocalization("fr", { values: "en" });
	const binding = new FieldEditorBinding(form, () => field, "text-list");
	form.set("supplier", "globex");
	form.liveValidation.flush("supplier");
	expect(requests[0]!.input.data).toEqual({ supplier: "globex" });
	const values = ["G-1", "G-1"];
	binding.set(values);
	values.push("outside mutation");
	binding.value!.push("read mutation");
	form.liveValidation.flush("values");
	expect(requests.at(-1)!.input.data.values).toEqual(["G-1", "G-1"]);
	expect(binding.liveValidation.status).toBe("pending");
	const latest = requests.at(-1)!;
	latest.response.resolve(answer(latest.input, "Current exact-locale feedback"));
	await settle();
	expect(binding.issues[0]?.message).toBe("Current exact-locale feedback");
	binding.set(["next"]);
	form.liveValidation.flush("values");
	const pending = requests.at(-1)!;
	form.setLocalization("en");
	form.setLocalization("fr");
	expect(pending.signal.aborted).toBe(true);
	expect(binding.stale).toBe(true);
	pending.response.resolve(answer(pending.input, "Stale locale"));
	await settle();
	expect(form.issuesFor("values")).toEqual([]);
	expect(() => binding.set(["late editor"])).toThrow("stale");
	form.disposeBindings();
});

it("advisory list success never overwrites an authoritative Save issue", async () => {
	const { form, fields, requests } = fixture();
	form.set("values", ["invalid"]);
	form.liveValidation.flush("values");
	requests[0]!.response.resolve(answer(requests[0]!.input, "Advisory feedback"));
	await settle();
	form.set("supplier", "globex");
	form.liveValidation.flush("values");
	const pending = requests.at(-1)!;
	const save = vi.fn(async () => {
		throw new RiduError({
			code: "validation",
			message: "Save failed",
			status: 422,
			issues: [{ path: "values", code: "save_values", message: "Authoritative list rejection" }],
		});
	});
	await expect(form.submit(fields, false, save)).rejects.toThrow("Save failed");
	expect(save).toHaveBeenCalledOnce();
	expect(pending.signal.aborted).toBe(true);
	pending.response.resolve(answer(pending.input));
	await settle();
	expect(form.issuesFor("values")).toEqual([
		{ path: "values", code: "save_values", message: "Authoritative list rejection" },
	]);
	form.disposeBindings();
});

it("generic embedded list forms apply valid typed payloads despite advisory issues and discard work on Cancel/reopen", async () => {
	const source = draftFixture([list("number-list")], { values: [0, 8] });
	const requests = transport(source.form);
	const runtime = new AdminRuntime(createAdminClient());
	const draft = source.authoring.beginSchemaDraft!({ treeKey: "widgets", identity: "a" });
	const session = source.sessions[0]!;
	const onApply = vi.fn((payload: Record<string, unknown>) =>
		source.binding.set({ outline: [{ kind: "widget", content: payload }] })
	);
	const screen = await render(Drawer, {
		session,
		runtime,
		options: { draft, onApply, onCancel: vi.fn() },
	});
	const path = `${draft.id}.values`;
	await screen.getByRole("textbox", { name: "Values, item 2", exact: true }).fill("9");
	session.form.liveValidation.flush(path);
	expect(requests[0]!.input.fields).toEqual(["values"]);
	expect(requests[0]!.input.embedded?.[0]?.data.values).toEqual([0, 9]);
	expect(requests[0]!.input.data).toEqual(source.form.liveValidationData());
	requests[0]!.response.resolve(answer(requests[0]!.input, "Advisory list feedback", '["values"]'));
	await expect.poll(() => session.form.liveValidation.forField(path).status).toBe("checked");
	await expect.element(screen.getByText("Advisory list feedback", { exact: true })).toBeVisible();
	await screen.getByRole("button", { name: "Apply", exact: true }).click();
	expect(onApply).toHaveBeenCalledWith(expect.objectContaining({ values: [0, 9] }));
	expect(source.form.get("body.outline.0.content.values")).toEqual([0, 9]);
	await screen.unmount();
	const next = source.authoring.beginSchemaDraft!({ treeKey: "widgets", identity: "a" });
	const nextSession = source.sessions.at(-1)!;
	const onCancel = vi.fn();
	const cancelScreen = await render(Drawer, {
		session: nextSession,
		runtime,
		options: { draft: next, onApply, onCancel },
	});
	expect(nextSession.form.liveValidation.forField(`${next.id}.values`).status).toBe("idle");
	await cancelScreen.getByRole("textbox", { name: "Values, item 2", exact: true }).fill("10");
	nextSession.form.liveValidation.flush(`${next.id}.values`);
	const pending = requests.at(-1)!;
	await cancelScreen.getByRole("button", { name: "Cancel", exact: true }).click();
	expect(pending.signal.aborted).toBe(true);
	pending.response.resolve(answer(pending.input, "Discarded embedded edit"));
	await settle();
	expect(source.form.get("body.outline.0.content.values")).toEqual([0, 9]);
	await cancelScreen.unmount();
	const reopened = source.authoring.beginSchemaDraft!({ treeKey: "widgets", identity: "a" });
	expect(source.sessions.at(-1)!.form.issuesFor(`${reopened.id}.values`)).toEqual([]);
	reopened.discard();
	source.binding.destroy();
	source.form.disposeBindings();
});

it("unfinished embedded lists cancel parent-dependent checks and cannot be applied until decoded", () => {
	const source = draftFixture([supplier, list("number-list")], {
		supplier: "acme",
		values: [0, 8],
	});
	source.form.reset({ ...source.form.snapshot(), supplier: "acme" }, [supplier, source.schema]);
	const requests = transport(source.form);
	const { draft, form } = createEmbeddedSchemaDraft(
		source.form,
		() => source.schema,
		{ treeKey: "widgets", identity: "a" },
		createAdminI18n()
	);
	form.set(`${draft.id}.supplier`, "globex");
	form.liveValidation.flush(`${draft.id}.supplier`);
	form.set(`${draft.id}.values`, [0, "-"]);
	expect(requests[0]!.signal.aborted).toBe(true);
	expect(form.liveValidation.forField(`${draft.id}.supplier`).status).toBe("skipped");
	source.form.set("supplier", "root changed");
	form.liveValidation.flush(`${draft.id}.supplier`);
	expect(requests).toHaveLength(1);
	expect(draft.validate()).toBe(false);
	form.set(`${draft.id}.values`, [0, 2]);
	form.liveValidation.flush(`${draft.id}.values`);
	expect(requests.at(-1)!.input.data.supplier).toBe("root changed");
	expect(requests.at(-1)!.input.embedded?.[0]?.data.values).toEqual([0, 2]);
	expect(draft.validate()).toBe(true);
	expect(requests.at(-1)!.signal.aborted).toBe(true);
	draft.discard();
	source.binding.destroy();
	source.form.disposeBindings();
});
