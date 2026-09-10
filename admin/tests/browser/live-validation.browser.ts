import { connectDocumentLiveValidation } from "@admin/core/forms/live-validation.svelte";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { createAdminClient } from "@admin/core/api/admin-client";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { afterEach, expect, it, vi } from "vitest";
import type { LiveValidationEnvelope, LiveValidationRequest, SchemaField } from "@riducms/protocol";
import { RiduError } from "@riducms/sdk";
import { FormController } from "@admin/core/forms/form-controller.svelte";
import { FieldEditorBinding } from "@admin/core/forms/field-editor-binding";
import { indexFieldValues } from "@admin/core/forms/form-issue-correlation";
import { draftFixture, title, tree } from "./draft-fixture";
import { createEmbeddedSchemaDraft } from "@admin/core/forms/embedded-schema-draft.svelte";
import { createAdminI18n } from "@riducms/translations";

const sku: SchemaField = {
	...title,
	id: "sku",
	name: "sku",
	path: "sku",
	required: false,
	liveValidation: true,
};
const supplier: SchemaField = {
	...sku,
	id: "supplier",
	name: "supplier",
	path: "supplier",
	liveValidation: false,
};
function deferred<Value>() {
	let resolve!: (value: Value) => void;
	let reject!: (reason: unknown) => void;
	const promise = new Promise<Value>((yes, no) => {
		resolve = yes;
		reject = no;
	});
	return { promise, resolve, reject };
}
function setup(
	fields = [supplier, sku],
	values: Record<string, unknown> = { supplier: "acme", sku: "A-1" }
) {
	const form = new FormController();
	form.reset(values, fields);
	form.setResource({ collection: "products", id: "one" });
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
	return { form, requests };
}
function answer(input: LiveValidationRequest, message = "Invalid SKU"): LiveValidationEnvelope {
	return {
		evaluations: input.fields.map((path) => ({
			path,
			status: "checked",
			issues: message ? [{ path, code: "sku", message }] : [],
		})),
	};
}
async function settle() {
	await Promise.resolve();
	await Promise.resolve();
	await Promise.resolve();
}
afterEach(() => {
	vi.useRealTimers();
});

it("is opt-in, idle on load, debounced on edits and flushed by blur", async () => {
	vi.useFakeTimers();
	const { form, requests } = setup();
	await vi.advanceTimersByTimeAsync(1000);
	form.set("supplier", "globex");
	await vi.advanceTimersByTimeAsync(1000);
	expect(requests).toHaveLength(0);
	form.set("sku", "G-1");
	await vi.advanceTimersByTimeAsync(499);
	expect(requests).toHaveLength(0);
	form.liveValidation.flush("sku");
	expect(requests).toHaveLength(1);
	expect(requests[0]!.input).toEqual({
		id: "one",
		data: { supplier: "globex", sku: "G-1" },
		fields: ["sku"],
	});
	requests[0]!.response.resolve(answer(requests[0]!.input, ""));
	await settle();
	expect(form.liveValidation.forField("sku").status).toBe("checked");
	form.liveValidation.flush("sku");
	expect(requests).toHaveLength(1);
	form.set("sku", "bad");
	await vi.advanceTimersByTimeAsync(500);
	expect(requests).toHaveLength(2);
	form.disposeBindings();
});

it("a sibling edit cancels old work, clears old feedback and ignores out-of-order responses", async () => {
	const { form, requests } = setup();
	form.set("sku", "bad");
	form.liveValidation.flush("sku");
	form.set("supplier", "globex");
	form.liveValidation.flush("sku");
	expect(requests[0]!.signal.aborted).toBe(true);
	expect(form.issuesFor("sku")).toEqual([]);
	requests[1]!.response.resolve(answer(requests[1]!.input, ""));
	await settle();
	requests[0]!.response.resolve(answer(requests[0]!.input, "Obsolete"));
	await settle();
	expect(form.issuesFor("sku")).toEqual([]);
	expect(form.liveValidation.forField("sku").status).toBe("checked");
	form.disposeBindings();
});

it("failed and missing evaluations remain explicit failures and can be retried", async () => {
	const { form, requests } = setup();
	form.set("sku", "bad");
	form.liveValidation.flush("sku");
	requests[0]!.response.reject(new Error("private server details"));
	await settle();
	expect(form.liveValidation.forField("sku").status).toBe("failed");
	expect(form.issuesFor("sku")).toEqual([]);
	form.liveValidation.forField("sku").retry();
	requests[1]!.response.resolve({ evaluations: [] });
	await settle();
	expect(form.liveValidation.forField("sku").status).toBe("failed");
	form.liveValidation.forField("sku").retry();
	requests[2]!.response.resolve({ evaluations: [{ path: "sku", status: "skipped", issues: [] }] });
	await settle();
	expect(form.liveValidation.forField("sku").status).toBe("skipped");
	form.disposeBindings();
});

it("save validation is independent and a late advisory success cannot erase save errors", async () => {
	const { form, requests } = setup();
	form.set("sku", "bad");
	form.liveValidation.flush("sku");
	requests[0]!.response.resolve(answer(requests[0]!.input));
	await settle();
	expect(form.issues).toEqual([]);
	expect(form.issuesFor("sku")).toHaveLength(1);
	form.set("supplier", "globex");
	form.liveValidation.flush("sku");
	const save = vi.fn(async () => {
		throw new RiduError({
			code: "validation",
			message: "Save failed",
			status: 400,
			issues: [{ path: "sku", code: "save_sku", message: "Authoritative SKU error" }],
		});
	});
	await expect(form.submit([supplier, sku], false, save)).rejects.toThrow("Save failed");
	expect(save).toHaveBeenCalledOnce();
	expect(requests[1]!.signal.aborted).toBe(true);
	requests[1]!.response.resolve(answer(requests[1]!.input, ""));
	await settle();
	expect(form.issuesFor("sku")[0]?.message).toBe("Authoritative SKU error");
	form.disposeBindings();
});

for (const outcome of ["success", "validation error", "request error"] as const)
	it(`keeps advisory issues visible during a pending save, then ${outcome === "success" ? "clears them" : outcome === "validation error" ? "replaces them" : "retains them"}`, async () => {
		const { form, requests } = setup();
		form.set("sku", "bad");
		form.liveValidation.flush("sku");
		requests[0]!.response.resolve(answer(requests[0]!.input, "Advisory SKU error"));
		await settle();
		expect(form.issues).toEqual([]);
		expect(form.issuesFor("sku").map((issue) => issue.message)).toEqual(["Advisory SKU error"]);

		const completion = deferred<string>();
		const submission = form.submit([supplier, sku], false, () => completion.promise);
		expect(form.submitting).toBe(true);
		expect(form.issues).toEqual([]);
		expect(form.issuesFor("sku").map((issue) => issue.message)).toEqual(["Advisory SKU error"]);

		if (outcome === "success") {
			completion.resolve("saved");
			await expect(submission).resolves.toBe("saved");
			expect(form.issuesFor("sku")).toEqual([]);
		} else if (outcome === "validation error") {
			const rejected = expect(submission).rejects.toThrow("Save failed");
			completion.reject(
				new RiduError({
					code: "validation",
					message: "Save failed",
					status: 422,
					issues: [{ path: "sku", code: "save_sku", message: "Authoritative SKU error" }],
				})
			);
			await rejected;
			expect(form.issuesFor("sku").map((issue) => issue.message)).toEqual([
				"Authoritative SKU error",
			]);
			form.set("sku", "A-corrected");
			expect(form.issuesFor("sku")).toEqual([]);
		} else {
			const rejected = expect(submission).rejects.toThrow("Document changed");
			completion.reject(
				new RiduError({
					code: "conflict",
					message: "Document changed",
					status: 409,
					issues: [],
				})
			);
			await rejected;
			expect(form.issuesFor("sku").map((issue) => issue.message)).toEqual(["Advisory SKU error"]);
			form.set("sku", "A-corrected");
			expect(form.issuesFor("sku")).toEqual([]);
		}
		expect(form.submitting).toBe(false);
		form.disposeBindings();
	});

for (const transition of ["reset", "document", "locale", "dispose"] as const)
	it(`ignores pending work after ${transition}`, async () => {
		const { form, requests } = setup();
		form.set("sku", "bad");
		form.liveValidation.flush("sku");
		if (transition === "reset") form.reset(form.snapshot());
		if (transition === "document") form.setResource({ collection: "products", id: "two" });
		if (transition === "locale") {
			form.setLocalization("fr");
			form.setLocalization("en");
		}
		if (transition === "dispose") form.disposeBindings();
		expect(requests[0]!.signal.aborted).toBe(true);
		requests[0]!.response.resolve(answer(requests[0]!.input));
		await settle();
		expect(form.issuesFor("sku")).toEqual([]);
		form.disposeBindings();
	});

for (const kind of ["array", "blocks"] as const)
	it(`${kind} aggregate checks preserve nested identity on reorder and discard replaced occurrences`, async () => {
		const links: SchemaField = {
			...sku,
			id: "links",
			name: "links",
			path: "sections.links",
			type: "array",
			category: "nested",
			liveValidation: false,
			nested: { fields: [sku] },
		};
		const field: SchemaField = {
			...links,
			id: "sections",
			name: "sections",
			path: "sections",
			type: kind,
			liveValidation: true,
			nested: { fields: [sku, links] },
			...(kind === "blocks"
				? {
						blocks: {
							types: [
								{
									slug: "card",
									labels: { singular: "Card", plural: "Cards" },
									fields: [sku, links],
								},
								{
									slug: "note",
									labels: { singular: "Note", plural: "Notes" },
									fields: [sku, links],
								},
							],
						},
					}
				: {}),
		};
		const first = {
			_key: "A",
			...(kind === "blocks" ? { blockType: "card" } : {}),
			links: [{ _key: "link-a", sku: "bad" }],
		};
		const second = { _key: "B", ...(kind === "blocks" ? { blockType: "card" } : {}), links: [] };
		const { form, requests } = setup([field], { sections: [first, second] });
		form.set("sections.0.links.0.sku", "invalid");
		form.liveValidation.flush("sections");
		form.setRows("sections", [second, first]);
		form.liveValidation.flush("sections");
		expect(requests[0]!.signal.aborted).toBe(true);
		requests[0]!.response.resolve(answer(requests[0]!.input, "obsolete"));
		const path = "sections.1.links.0.sku";
		const target = indexFieldValues([field], form.values).find(
			(location) => location.path === path
		)!.token;
		requests[1]!.response.resolve({
			evaluations: requests[1]!.input.fields.map((fieldPath) => ({
				path: fieldPath,
				status: "checked",
				issues: [{ path, target, code: "sku", message: "Current row only" }],
			})),
		});
		await settle();
		expect(form.issuesFor("sections.0")).toEqual([]);
		expect(form.issuesFor(path)[0]?.message).toBe("Current row only");
		form.setRows("sections", [second]);
		form.setRows("sections", [
			second,
			{ ...first, blockType: kind === "blocks" ? "note" : undefined },
		]);
		expect(form.issuesFor(path)).toEqual([]);
		form.disposeBindings();
	});

it("custom binding shares host feedback and retained retries expire with the editor", async () => {
	const { form, requests } = setup();
	const binding = new FieldEditorBinding(form, () => sku, "text");
	binding.set("bad");
	form.liveValidation.flush("sku");
	expect(binding.liveValidation.status).toBe("pending");
	requests[0]!.response.reject(new Error("offline"));
	await settle();
	const feedback = binding.liveValidation;
	expect(feedback.status).toBe("failed");
	feedback.retry();
	requests[1]!.response.resolve(answer(requests[1]!.input));
	await settle();
	expect(binding.issues[0]?.message).toBe("Invalid SKU");
	binding.destroy();
	expect(() => feedback.retry()).toThrow("stale");
	form.disposeBindings();
});

it("generic embedded forms send declared scopes, rebase issues and cancel/reopen without stale work", async () => {
	const fixture = draftFixture([{ ...title, liveValidation: true }]);
	const requests: {
		input: LiveValidationRequest;
		signal: AbortSignal;
		response: ReturnType<typeof deferred<LiveValidationEnvelope>>;
	}[] = [];
	fixture.form.configureLiveValidation((input, signal) => {
		const response = deferred<LiveValidationEnvelope>();
		requests.push({ input, signal, response });
		return response.promise;
	});
	const draft = fixture.authoring.beginSchemaDraft!({ treeKey: "widgets", identity: "a" });
	const session = fixture.sessions[0]!;
	expect(requests).toHaveLength(0);
	session.form.set(`${draft.id}.title`, "bad");
	session.form.liveValidation.flush(`${draft.id}.title`);
	expect(requests[0]!.input.fields).toEqual(["title"]);
	expect(requests[0]!.input.embedded).toEqual([
		{
			field: "body",
			treeKey: "widgets",
			caseTag: "widget",
			variantSlug: "card",
			identity: "a",
			data: { title: "bad", schema: "card", uid: "a" },
		},
	]);
	expect(requests[0]!.input.data).toEqual(fixture.form.liveValidationData());
	requests[0]!.response.resolve({
		evaluations: [
			{
				path: "title",
				status: "checked",
				issues: [{ path: "title", target: '["title"]', code: "title", message: "Embedded issue" }],
			},
		],
	});
	await settle();
	expect(session.form.issuesFor(`${draft.id}.title`)[0]?.message).toBe("Embedded issue");
	session.form.set(`${draft.id}.title`, "new");
	session.form.liveValidation.flush(`${draft.id}.title`);
	draft.discard();
	expect(requests[1]!.signal.aborted).toBe(true);
	requests[1]!.response.resolve(answer(requests[1]!.input));
	await settle();
	const next = fixture.authoring.beginSchemaDraft!({ treeKey: "widgets", identity: "a" });
	expect(fixture.sessions[1]!.form.issuesFor(`${next.id}.title`)).toEqual([]);
	next.discard();
	fixture.binding.destroy();
});

it("times out stalled transport as a retryable failure and ignores its eventual response", async () => {
	vi.useFakeTimers();
	const { form, requests } = setup();
	form.set("sku", "bad");
	form.liveValidation.flush("sku");
	await vi.advanceTimersByTimeAsync(8_000);
	expect(requests[0]!.signal.aborted).toBe(true);
	expect(form.liveValidation.forField("sku").status).toBe("failed");
	requests[0]!.response.resolve(answer(requests[0]!.input));
	await settle();
	expect(form.issuesFor("sku")).toEqual([]);
	form.liveValidation.forField("sku").retry();
	expect(requests).toHaveLength(2);
	form.disposeBindings();
});

it("keeps unchanged fallback values out of advisory input", () => {
	const localized = { ...sku, localized: true };
	const { form, requests } = setup([localized, supplier]);
	form.setLocalization("fr", { sku: "en" });
	form.set("supplier", "globex");
	expect(form.liveValidationData()).toEqual({ supplier: "globex" });
	expect(requests).toHaveLength(0);
	form.set("sku", "FR-1");
	form.liveValidation.flush("sku");
	expect(requests[0]!.input.data).toEqual({ sku: "FR-1", supplier: "globex" });
	form.disposeBindings();
});

it("nested detached forms propagate root edits and use a declared selector chain", async () => {
	const nested: SchemaField = {
		...title,
		id: "nested",
		name: "nested",
		path: "body.nested",
		type: "plugin",
		category: "plugin",
		required: false,
		plugin: {
			key: "outline",
			config: {},
			embeddedTrees: [tree([{ ...title, liveValidation: true }])],
		},
	};
	const fixture = draftFixture([title, nested], {
		title: "Outer",
		nested: {
			outline: [{ kind: "widget", content: { schema: "card", uid: "inner", title: "Inner" } }],
		},
	});
	const requests: {
		input: LiveValidationRequest;
		signal: AbortSignal;
		response: ReturnType<typeof deferred<LiveValidationEnvelope>>;
	}[] = [];
	fixture.form.configureLiveValidation((input, signal) => {
		const response = deferred<LiveValidationEnvelope>();
		requests.push({ input, signal, response });
		return response.promise;
	});
	const outer = fixture.authoring.beginSchemaDraft!({ treeKey: "widgets", identity: "a" });
	const outerSession = fixture.sessions[0]!;
	const inner = createEmbeddedSchemaDraft(
		outerSession.form,
		() => outerSession.fields[1]!,
		{ treeKey: "widgets", identity: "inner" },
		createAdminI18n()
	);
	inner.form.set(`${inner.draft.id}.title`, "Bad");
	inner.form.liveValidation.flush(`${inner.draft.id}.title`);
	expect(requests[0]!.input.fields).toEqual(["title"]);
	expect(requests[0]!.input.embedded?.map(({ field, identity }) => ({ field, identity }))).toEqual([
		{ field: "body", identity: "a" },
		{ field: "nested", identity: "inner" },
	]);
	expect(requests[0]!.input.embedded?.[1]?.data).toEqual({
		schema: "card",
		uid: "inner",
		title: "Bad",
	});
	fixture.form.set("unrelated", "root edit");
	expect(requests[0]!.signal.aborted).toBe(true);
	inner.form.liveValidation.flush(`${inner.draft.id}.title`);
	expect(requests).toHaveLength(2);
	requests[1]!.response.resolve({
		evaluations: [
			{
				path: "title",
				status: "checked",
				issues: [{ path: "title", target: '["title"]', code: "title", message: "Nested feedback" }],
			},
		],
	});
	await settle();
	await expect
		.poll(() => inner.form.liveValidation.forField(`${inner.draft.id}.title`).status)
		.toBe("checked");
	expect(inner.form.issuesFor(`${inner.draft.id}.title`)[0]?.message).toBe("Nested feedback");
	inner.draft.discard();
	outer.discard();
	fixture.binding.destroy();
});

it("opening a detached editor never promotes parent advisory feedback into save issues", async () => {
	const fixture = draftFixture([{ ...title, liveValidation: true }]);
	fixture.form.configureLiveValidation(async (input) => answer(input, "Parent advisory"));
	fixture.form.set("body.outline.0.content.title", "Bad");
	fixture.form.liveValidation.flush("body.outline.0.content.title");
	await settle();
	expect(fixture.form.issuesFor("body.outline.0.content.title")[0]?.message).toBe(
		"Parent advisory"
	);
	const draft = fixture.authoring.beginSchemaDraft!({ treeKey: "widgets", identity: "a" });
	const session = fixture.sessions[0]!;
	expect(session.form.issues).toEqual([]);
	expect(session.form.issuesFor(`${draft.id}.title`)).toEqual([]);
	draft.discard();
	fixture.binding.destroy();
	fixture.form.disposeBindings();
});

it("revoking write access cancels pending feedback", async () => {
	const { form, requests } = setup();
	form.set("sku", "bad");
	form.liveValidation.flush("sku");
	form.writeBlocked = true;
	expect(requests[0]!.signal.aborted).toBe(true);
	requests[0]!.response.resolve(answer(requests[0]!.input));
	await settle();
	expect(form.issuesFor("sku")).toEqual([]);
	expect(form.liveValidation.forField("sku").status).toBe("idle");
	form.disposeBindings();
});

for (const transition of [
	"Block case replacement",
	"same-key reuse after deletion",
	"row replacement",
] as const)
	it(`rejects delayed feedback after ${transition}`, async () => {
		const field: SchemaField = {
			...sku,
			id: "content",
			name: "content",
			path: "content",
			type: "blocks",
			category: "nested",
			liveValidation: false,
			blocks: {
				types: ["card", "note"].map((slug) => ({
					slug,
					labels: { singular: slug, plural: slug },
					fields: [sku],
				})),
			},
		};
		const row = { _key: "A", blockType: "card", sku: "bad" };
		const { form, requests } = setup([field], { content: [row] });
		form.set("content.0.sku", "invalid");
		form.liveValidation.flush("content.0.sku");
		if (transition === "Block case replacement")
			form.setRows("content", [{ ...row, blockType: "note" }]);
		else if (transition === "same-key reuse after deletion") {
			form.setRows("content", []);
			form.setRows("content", [row]);
		} else form.setRows("content", [{ ...row, _key: "B" }]);
		expect(requests[0]!.signal.aborted).toBe(true);
		requests[0]!.response.resolve(answer(requests[0]!.input, "Old occurrence"));
		await settle();
		expect(form.issuesFor("content.0.sku")).toEqual([]);
		form.disposeBindings();
	});

it("never serializes malformed numeric input as a successfully decoded empty value", async () => {
	const number: SchemaField = {
		...sku,
		id: "amount",
		name: "amount",
		path: "amount",
		type: "number",
		number: {},
	};
	const { form, requests } = setup([sku, number], { sku: "A-1", amount: 1 });
	form.set("sku", "bad");
	form.liveValidation.flush("sku");
	form.set("amount", Number.NaN);
	expect(requests[0]!.signal.aborted).toBe(true);
	expect(form.liveValidation.forField("sku").status).toBe("skipped");
	expect(form.liveValidation.forField("amount").status).toBe("skipped");
	form.liveValidation.flush("amount");
	expect(requests).toHaveLength(1);
	requests[0]!.response.resolve(answer(requests[0]!.input));
	await settle();
	expect(form.issuesFor("sku")).toEqual([]);
	form.set("amount", 2);
	form.liveValidation.flush("amount");
	expect(requests[1]!.input.data.amount).toBe(2);
	form.disposeBindings();
});

const JSONEditor = svelte`
	<script>
		import ScalarField from "../../src/fields/scalar/scalar-field.svelte";
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		let { form, field, runtime } = $props();
		setAdminRuntime(runtime);
	</script>
	<ScalarField {form} {field} />
`;
it("a malformed JSON buffer cancels root-dependent checks instead of validating the previous value", async () => {
	const json: SchemaField = {
		...sku,
		id: "details",
		name: "details",
		path: "details",
		type: "json",
		admin: { label: "Details" },
	};
	const { form, requests } = setup([sku, json], { sku: "A-1", details: { previous: true } });
	const screen = await render(JSONEditor, {
		form,
		field: json,
		runtime: new AdminRuntime(createAdminClient()),
	});
	form.set("sku", "bad");
	form.liveValidation.flush("sku");
	await screen.getByRole("textbox", { name: "Details" }).fill("{");
	expect(form.get("details")).toEqual({ previous: true });
	expect(requests[0]!.signal.aborted).toBe(true);
	expect(form.liveValidation.forField("sku").status).toBe("skipped");
	expect(form.liveValidation.forField("details").status).toBe("skipped");
	await screen.getByRole("textbox", { name: "Details" }).fill('{"next":true}');
	form.liveValidation.flush("details");
	expect(requests[1]!.input.data.details).toEqual({ next: true });
	form.disposeBindings();
	await screen.unmount();
});

it("global ordinary and detached requests omit document IDs at the SDK boundary", async () => {
	const client = createAdminClient();
	const check = vi.spyOn(client, "globalLiveValidation").mockResolvedValue({ evaluations: [] });
	const { form } = setup();
	form.setResource({ collection: "settings", id: "settings", global: true });
	connectDocumentLiveValidation(form, client);
	form.set("sku", "bad");
	form.liveValidation.flush("sku");
	await settle();
	expect(check.mock.calls[0]![0]).toBe("settings");
	expect(check.mock.calls[0]![1]).not.toHaveProperty("id");
	form.disposeBindings();
	const fixture = draftFixture([{ ...title, liveValidation: true }]);
	fixture.form.setResource({ collection: "settings", id: "settings", global: true });
	connectDocumentLiveValidation(fixture.form, client);
	const session = createEmbeddedSchemaDraft(
		fixture.form,
		() => fixture.schema,
		{ treeKey: "widgets", identity: "a" },
		createAdminI18n()
	);
	const draft = session.draft;
	session.form.set(`${draft.id}.title`, "Bad");
	session.form.liveValidation.flush(`${draft.id}.title`);
	await settle();
	expect(check.mock.calls[1]![1]).not.toHaveProperty("id");
	expect(check.mock.calls[1]![1].embedded).toHaveLength(1);
	draft.discard();
	fixture.binding.destroy();
	check.mockRestore();
});

it("skips an unavailable embedded structure without rejecting an unowned promise", () => {
	const plugin: SchemaField = {
		...sku,
		id: "body",
		name: "body",
		path: "body",
		type: "plugin",
		category: "plugin",
		plugin: { key: "outline", config: {}, embeddedTrees: [tree([title])] },
	};
	const { form, requests } = setup([sku, plugin], { sku: "A-1", body: { outline: "malformed" } });
	form.set("sku", "bad");
	form.liveValidation.flush("sku");
	expect(requests).toHaveLength(0);
	expect(form.liveValidation.forField("sku").status).toBe("skipped");
	form.disposeBindings();
});

const RepeatedJSONEditors = svelte`
	<script>
		import ScalarField from "../../src/fields/scalar/scalar-field.svelte";
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		let { form, field, runtime } = $props();
		setAdminRuntime(runtime);
		function rowField(index, row) {
			return { ...field, id: "details-" + row._key, path: "rows." + index + ".details", admin: { label: "Details " + row._key } };
		}
	</script>
	{#each form.get("rows") as row, index (form.rowMountKey(row))}
		<ScalarField {form} field={rowField(index, row)} />
	{/each}
`;
for (const kind of ["array", "blocks"] as const)
	it(`unfinished JSON follows its ${kind} occurrence through reorder and releases on disposal`, async () => {
		const json: SchemaField = {
			...sku,
			id: "details",
			name: "details",
			path: "rows.details",
			type: "json",
			admin: { label: "Details" },
		};
		const rows: SchemaField = {
			...sku,
			id: "rows",
			name: "rows",
			path: "rows",
			type: kind,
			category: "nested",
			liveValidation: false,
			...(kind === "array"
				? { nested: { fields: [json] } }
				: {
						blocks: {
							types: [
								{ slug: "card", labels: { singular: "Card", plural: "Cards" }, fields: [json] },
							],
						},
					}),
		};
		const values = ["A", "B"].map((_key) => ({
			_key,
			...(kind === "blocks" ? { blockType: "card" } : {}),
			details: { previous: _key },
		}));
		const { form, requests } = setup([sku, rows], { sku: "A-1", rows: values });
		const screen = await render(RepeatedJSONEditors, {
			form,
			field: json,
			runtime: new AdminRuntime(createAdminClient()),
		});
		form.set("sku", "bad");
		form.liveValidation.flush("sku");
		await screen.getByRole("textbox", { name: "Details A" }).fill("{");
		expect(requests[0]!.signal.aborted).toBe(true);
		const reordered = (form.snapshot().rows as Record<string, unknown>[]).toReversed();
		form.setRows("rows", reordered);
		await expect.element(screen.getByRole("textbox", { name: "Details A" })).toHaveValue("{");
		expect(form.liveValidation.forField("sku").status).toBe("skipped");
		form.liveValidation.flush("sku");
		expect(requests).toHaveLength(1);
		await screen.getByRole("textbox", { name: "Details A" }).fill('{"valid":true}');
		form.liveValidation.flush("sku");
		expect(requests).toHaveLength(2);
		expect((requests[1]!.input.data.rows as Record<string, unknown>[])[1]!.details).toEqual({
			valid: true,
		});
		await screen.getByRole("textbox", { name: "Details A" }).fill("{");
		expect(form.liveValidation.forField("sku").status).toBe("skipped");
		await screen.unmount();
		form.liveValidation.flush("sku");
		expect(requests).toHaveLength(3);
		form.disposeBindings();
	});

it("unfinished input leases are removed with their row and do not attach to a replacement case", () => {
	const json: SchemaField = {
		...sku,
		id: "details",
		name: "details",
		path: "rows.details",
		type: "json",
	};
	const rows: SchemaField = {
		...sku,
		id: "rows",
		name: "rows",
		path: "rows",
		type: "blocks",
		category: "nested",
		liveValidation: false,
		blocks: {
			types: ["card", "note"].map((slug) => ({
				slug,
				labels: { singular: slug, plural: slug },
				fields: [json],
			})),
		},
	};
	const row = { _key: "A", blockType: "card", details: {} };
	const { form, requests } = setup([sku, rows], { sku: "A-1", rows: [row] });
	form.set("sku", "bad");
	const release = form.setLiveInputUnavailable("rows.0.details");
	form.setRows("rows", [{ ...row, blockType: "note" }]);
	form.liveValidation.flush("sku");
	expect(requests).toHaveLength(1);
	form.setLiveInputUnavailable("rows.0.details");
	form.setRows("rows", []);
	form.setRows("rows", [row]);
	form.liveValidation.flush("sku");
	expect(requests).toHaveLength(2);
	release();
	form.disposeBindings();
});
