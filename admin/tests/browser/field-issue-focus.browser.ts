import { expect, it } from "vitest";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import type { SchemaField } from "@riducms/protocol";
import { FormController } from "@admin/core/forms/form-controller.svelte";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { createAdminClient } from "@admin/core/api/admin-client";

const LazyRows = svelte`
	<script>
		import { focusFieldIssue, fieldIssueRevealEvent } from "../../src/core/forms/field-issue-focus";
		let root;
		let outer = $state(false);
		let inner = $state(false);
		function revealOuter(node) {
			const reveal = () => outer = true;
			node.addEventListener(fieldIssueRevealEvent, reveal);
			return () => node.removeEventListener(fieldIssueRevealEvent, reveal);
		}
		function revealInner(node) {
			const reveal = () => inner = true;
			node.addEventListener(fieldIssueRevealEvent, reveal);
			return () => node.removeEventListener(fieldIssueRevealEvent, reveal);
		}
	</script>
	<div bind:this={root}>
		<button onclick={() => focusFieldIssue("layout.0.entries.0.title", root)}>Reveal issue</button>
		<div data-field-path="layout.0" {@attach revealOuter}>
			{#if outer}
				<div data-field-path="layout.0.entries.0" {@attach revealInner}>
					{#if inner}
						<div data-field-path="layout.0.entries.0.title"><input aria-label="Nested title" /></div>
					{/if}
				</div>
			{/if}
		</div>
	</div>
`;

it("progressively reveals nested lazy rows before focusing the exact invalid field", async () => {
	const screen = await render(LazyRows);
	try {
		await screen.getByRole("button", { name: "Reveal issue" }).click();
		await expect.element(screen.getByRole("textbox", { name: "Nested title" })).toHaveFocus();
	} finally {
		await screen.unmount();
	}
});

const DuplicatePaths = svelte`
	<script>
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import { setAdminI18n } from "@riducms/plugin";
		import FieldLayout from "../../src/fields/field-layout.svelte";
		let { runtime, parent, drawer, fields } = $props();
		setAdminRuntime(runtime);
		setAdminI18n(runtime.i18n);
	</script>
	<section data-testid="parent"><FieldLayout form={parent} fields={fields("parent")} /></section>
	<section data-testid="drawer"><FieldLayout form={drawer} fields={fields("drawer")} /></section>
`;

function fields(prefix: string): SchemaField[] {
	return [
		{
			id: `${prefix}-tags`,
			name: "tags",
			path: "tags",
			type: "array",
			category: "nested",
			required: false,
			unique: false,
			admin: { label: "Tags" },
			nested: {
				fields: [
					{
						id: `${prefix}-label`,
						name: "label",
						path: "tags.label",
						type: "text",
						category: "scalar",
						required: true,
						unique: false,
						admin: { label: "Label" },
					},
				],
			},
		},
	];
}

it("reveals and focuses an error badge inside its own field when another editor has the same path", async () => {
	const runtime = new AdminRuntime(createAdminClient());
	const parent = new FormController();
	const drawer = new FormController();
	for (const [name, form] of [
		["parent", parent],
		["drawer", drawer],
	] as const) {
		form.reset({ tags: [{ _key: "one", label: "" }] }, fields(name));
		form.issues = [{ path: "tags.0.label", code: "required", message: "Label is required" }];
	}
	const screen = await render(DuplicatePaths, { runtime, parent, drawer, fields });
	try {
		const parentUI = screen.getByTestId("parent");
		const drawerUI = screen.getByTestId("drawer");
		await parentUI.getByRole("button", { name: "Collapse", exact: true }).click();
		await drawerUI.getByRole("button", { name: "Collapse", exact: true }).click();
		await drawerUI.getByRole("button", { name: "1 Error: Label is required", exact: true }).click();
		await expect.element(drawerUI.getByRole("textbox", { name: "Label" })).toHaveFocus();
		await expect
			.element(parentUI.getByRole("button", { name: "Expand", exact: true }))
			.toHaveAttribute("aria-expanded", "false");
	} finally {
		await screen.unmount();
		parent.disposeBindings();
		drawer.disposeBindings();
		runtime.dispose();
	}
});
