import { createAdminI18n } from "@riducms/translations";
import type { SchemaEmbeddedTree, SchemaField } from "@riducms/protocol";
import { FormController } from "@admin/core/forms/form-controller.svelte";
import {
	createEmbeddedSchemaDraft,
	type HostedSchemaDraft,
} from "@admin/core/forms/embedded-schema-draft.svelte";
import { PluginFieldBinding } from "@admin/core/forms/plugin-field-binding";
import { guardPluginAuthoring } from "@admin/core/forms/plugin-field-authoring";

export const title: SchemaField = {
	id: "title",
	name: "title",
	path: "body.title",
	type: "text",
	category: "scalar",
	required: true,
	unique: false,
	admin: { label: "Card title" },
	text: {},
};

export function tree(fields: SchemaField[]): SchemaEmbeddedTree {
	return {
		version: 1,
		key: "widgets",
		root: ["outline"],
		children: "items",
		tag: "kind",
		cases: [
			{
				tagValue: "widget",
				payload: "content",
				discriminator: "schema",
				identity: "uid",
				types: [{ slug: "card", labels: { singular: "Card", plural: "Cards" }, fields }],
			},
		],
	};
}

export function draftFixture(
	fields = [title],
	payload: Record<string, unknown> = { title: "Original" }
) {
	const schema: SchemaField = {
		id: "body",
		name: "body",
		path: "body",
		type: "plugin",
		category: "plugin",
		required: false,
		unique: false,
		admin: { label: "Body" },
		plugin: { key: "outline", config: {}, embeddedTrees: [tree(fields)] },
	};
	const form = new FormController();
	form.reset(
		{ body: { outline: [{ kind: "widget", content: { ...payload, schema: "card", uid: "a" } }] } },
		[schema]
	);
	const binding = new PluginFieldBinding(form, () => schema, { decodeValue: (value) => value });
	const sessions: HostedSchemaDraft[] = [];
	const disposals: { count: number }[] = [];
	const authoring = guardPluginAuthoring(
		{
			collections: [],
			documentRevision: 0,
			referenceBrowser: () => ({}),
			findDocument: async () => ({ id: "one" }),
			beginSchemaDraft(scope) {
				const session = createEmbeddedSchemaDraft(
					form,
					() => binding.schema,
					scope,
					createAdminI18n(),
					() => !binding.stale
				);
				const disposal = { count: 0 };
				const discard = session.draft.discard;
				session.draft.discard = () => {
					disposal.count++;
					discard();
				};
				sessions.push(session);
				disposals.push(disposal);
				return session.draft;
			},
		},
		binding
	);
	return { form, schema, binding, authoring, sessions, disposals };
}
