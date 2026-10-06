import { createAdminI18n } from "@riducms/translations";
import type { SchemaBlockType, SchemaEmbeddedTree, SchemaField } from "@riducms/protocol";
import { FormController } from "@admin/core/forms/form-controller.svelte";
import {
	createEmbeddedSchemaDraft,
	type HostedSchemaDraft,
} from "@admin/core/forms/embedded-schema-draft.svelte";
import { PluginFieldBinding } from "@admin/core/forms/plugin-field-binding";
import { guardPluginAuthoring } from "@admin/core/forms/plugin-field-authoring";

import { bindBlockField, blockDefinition } from "../block-manifest";

/** A field of the `card` definition, with its definition-relative path and ID. */
export const title: SchemaField = {
	id: "block-card-title",
	name: "title",
	path: "title",
	type: "text",
	category: "scalar",
	required: true,
	unique: false,
	admin: { label: "Card title" },
	text: {},
};

/** A registry definition labelled as a card, with definition-relative field paths. */
export function card(fields: SchemaField[], slug = "card"): SchemaBlockType {
	return blockDefinition(slug, fields, { singular: "Card", plural: "Cards" });
}

/** An embedded tree whose `widget` case selects the `slug` definition. */
export function tree(slug = "card"): SchemaEmbeddedTree {
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
				blockReferences: [slug],
			},
		],
	};
}

/**
 * Binds a `body` plugin field whose widgets select the `card` definition with
 * `fields`; `blocks` adds further definitions that those fields select.
 */
export function draftFixture(
	fields = [title],
	payload: Record<string, unknown> = { title: "Original" },
	blocks: SchemaBlockType[] = []
) {
	const schema: SchemaField = bindBlockField([card(fields), ...blocks], {
		id: "body",
		name: "body",
		path: "body",
		type: "plugin",
		category: "plugin",
		required: false,
		unique: false,
		admin: { label: "Body" },
		plugin: { key: "outline", config: {}, embeddedTrees: [tree()] },
	} satisfies SchemaField);
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
			canCreateDocument: () => false,
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
