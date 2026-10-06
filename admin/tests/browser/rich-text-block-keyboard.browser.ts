import { expect, it } from "vitest";
import { userEvent } from "vitest/browser";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import type { SchemaBlockType, SchemaField } from "@riducms/protocol";
import { richTextAdminPlugin } from "@riducms/plugin-richtext";
import { createAdminClient } from "@admin/core/api/admin-client";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { FormController } from "@admin/core/forms/form-controller.svelte";

import { bindBlockField } from "../block-manifest";

const Harness = svelte`
 <script>
  import { setAdminI18n } from "@riducms/plugin";
  import { TooltipProvider } from "@riducms/ui";
  import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
  import FieldLayout from "../../src/fields/field-layout.svelte";
  let { form, fields, runtime } = $props();
  setAdminRuntime(runtime);
  setAdminI18n(runtime.i18n);
 </script>
 <TooltipProvider><FieldLayout {form} {fields} /></TooltipProvider>
`;

function bodyField(type: "code" | "json"): SchemaField {
	const note: SchemaBlockType = {
		slug: "note",
		labels: { singular: "Note", plural: "Notes" },
		fields: [
			{
				id: "block-note-value",
				name: "value",
				path: "value",
				type,
				category: "scalar",
				required: false,
				unique: false,
				admin: { label: type === "json" ? "Metadata" : "Code" },
			},
		],
	};
	return bindBlockField([note], {
		id: "body",
		name: "body",
		path: "body",
		type: "plugin",
		category: "plugin",
		required: false,
		unique: false,
		admin: { label: "Body" },
		plugin: {
			key: "richtext",
			config: { features: ["blocks"] },
			embeddedTrees: [
				{
					version: 1,
					key: "blocks",
					root: ["root"],
					children: "children",
					tag: "type",
					cases: [
						{
							tagValue: "block",
							payload: "fields",
							discriminator: "blockType",
							identity: "_key",
							blockReferences: ["note"],
						},
					],
				},
			],
		},
	});
}

function blocks(form: FormController) {
	const body = form.get("body") as {
		root: { children: { type: string; fields?: Record<string, unknown> }[] };
	};
	return body.root.children.filter((node) => node.type === "block");
}

for (const type of ["code", "json"] as const) {
	for (const edited of [false, true]) {
		it(`${type} ${edited ? "edited" : "empty"} history consumes undo without removing its rich-text block`, async () => {
			const runtime = new AdminRuntime(createAdminClient(), { plugins: [richTextAdminPlugin] });
			const form = new FormController({}, runtime.i18n);
			const body = bodyField(type);
			form.reset(
				{
					body: {
						version: 1,
						root: { type: "root", children: [{ type: "paragraph", children: [] }] },
					},
				},
				[body]
			);
			const screen = await render(Harness, { runtime, form, fields: [body] });

			try {
				await screen.getByRole("textbox", { name: "Body", exact: true }).fill("/Note");
				await screen.getByRole("option", { name: "Note", exact: true }).click();
				const select = screen.getByRole("button", { name: "Select Note block", exact: true });
				const child = screen.getByRole("textbox", {
					name: type === "json" ? "Metadata" : "Code",
					exact: true,
				});
				await expect.element(select).toBeVisible();
				await expect
					.poll(() => child.element().textContent?.trim())
					.toBe(type === "json" ? "{}" : "");
				const identity = blocks(form)[0]?.fields?._key;
				expect(identity).toBeTypeOf("string");

				if (edited) {
					await child.fill(type === "json" ? '{"unfinished":' : "const answer = 42;");
					if (type === "json") expect(form.pendingEditIssues()).toHaveLength(1);
				}
				await child.click();
				await userEvent.keyboard("{ControlOrMeta>}z{/ControlOrMeta}");
				await expect
					.poll(() => child.element().textContent?.trim())
					.toBe(type === "json" ? "{}" : "");
				expect(form.pendingEditIssues()).toEqual([]);
				// An exhausted child history still owns the next undo shortcut.
				await userEvent.keyboard("{ControlOrMeta>}z{/ControlOrMeta}");
				await expect.poll(() => blocks(form).map((node) => node.fields?._key)).toEqual([identity]);
				await expect.element(select).toBeVisible();
				await expect.element(child).toHaveFocus();
			} finally {
				await screen.unmount();
				runtime.dispose();
			}
		});
	}
}
