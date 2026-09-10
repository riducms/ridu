import { expect, it } from "vitest";
import type { SchemaCollection, SchemaField } from "@riducms/protocol";
import { createAdminI18n } from "@riducms/translations";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { render } from "vitest-browser-svelte";

import type { ReferenceBrowserWorkflow } from "@admin/features/reference-browser/reference-browser-workflow.svelte";
const Harness = svelte`
	<script>
		import { ReferenceBrowserWorkflow } from "../../src/features/reference-browser/reference-browser-workflow.svelte";
		let { options, readOnly = false, onReady } = $props();
		const workflow = new ReferenceBrowserWorkflow({
			...options,
			get readOnly() {
				return readOnly;
			},
		});
		onReady(workflow);
	</script>
	<button disabled={!workflow.canSave}>Save reference</button>
`;
it("reference workflow intersects live owner editability before dispatch and after responses", async () => {
	const field = {
		id: "title",
		name: "title",
		path: "title",
		type: "text",
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: "Title" },
	} as SchemaField;
	const collection = {
		slug: "posts",
		fields: [field],
		labels: { singular: "Post", plural: "Posts" },
		capabilities: {},
	} as SchemaCollection;
	let calls = 0;
	let signal: AbortSignal | undefined;
	let finish!: (value: { id: string }) => void;
	const options = {
		runtime: {
			i18n: createAdminI18n(),
			client: {
				update: (
					_collection: string,
					_id: string,
					_data: unknown,
					request: { signal: AbortSignal }
				) => {
					signal = request.signal;
					calls++;
					return new Promise<{ id: string }>((resolve) => {
						finish = resolve;
					});
				},
			},
			collectionOperations: { posts: { create: true } },
			documentsChanged() {},
		},
		notifications: { error() {}, success() {}, validation() {} },
		field,
		collection,
		hasMany: false,
		selectedIDs: [],
		open: false,
		setOpen() {},
		onCommit() {},
		onClose() {},
	} as unknown as ConstructorParameters<typeof ReferenceBrowserWorkflow>[0];
	let workflow!: InstanceType<typeof ReferenceBrowserWorkflow>;
	const screen = await render(Harness, {
		options,
		onReady: (value: typeof workflow) => {
			workflow = value;
		},
	});
	workflow.screen = "document";
	workflow.editorDocument = { id: "one" };
	workflow.form.reset({ title: "Original" }, [field]);
	workflow.form.set("title", "Edited");
	workflow.form.setAccess(
		{ operations: { update: true, create: true }, fields: {} } as never,
		"update"
	);
	expect(workflow.canSave).toBe(true);
	await expect.element(screen.getByRole("button", { name: "Save reference" })).toBeEnabled();
	await screen.rerender({ readOnly: true });
	await expect.element(screen.getByRole("button", { name: "Save reference" })).toBeDisabled();
	expect(workflow.canSave).toBe(false);
	expect(workflow.canCreateDocument).toBe(false);
	workflow.toggleSelection("two");
	expect(workflow.workingSelection).toEqual([]);
	expect(await workflow.saveEditor()).toBe(false);
	expect(calls).toBe(0);
	await screen.rerender({ readOnly: false });
	const pending = workflow.saveEditor();
	expect(calls).toBe(1);
	await screen.rerender({ readOnly: true });
	finish({ id: "two" });
	expect(await pending).toBe(false);
	expect(workflow.editorDocument?.id).toBe("one");
	expect(workflow.workingSelection).toEqual([]);
	await screen.rerender({ readOnly: false });
	workflow.form.set("title", "Edited before unmount");
	const unmounted = workflow.saveEditor();
	expect(calls).toBe(2);
	expect(signal?.aborted).toBe(false);
	await screen.unmount();
	expect(signal?.aborted).toBe(true);
	finish({ id: "late" });
	expect(await unmounted).toBe(false);
	expect(workflow.editorDocument?.id).toBe("one");
});
