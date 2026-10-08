import type { AdminI18n, FieldAuthoringHost } from "@riducms/plugin";
import type { LexicalEditor } from "lexical";

import type { RichTextConfig } from "#lib/field/rich-text-config.js";
import { OPEN_RELATIONSHIP_BROWSER_COMMAND } from "#lib/menu/rich-text-commands.js";
import { keywords, RichTextMenuOption } from "#lib/menu/rich-text-options.js";

/** The collections the field can relate to and the admin can browse. */
export function richTextRelationshipCollections(
	config: RichTextConfig,
	authoring: FieldAuthoringHost | undefined
) {
	return (authoring?.collections ?? []).filter(
		(collection) =>
			config.relationshipCollections.length === 0 ||
			config.relationshipCollections.includes(collection.slug)
	);
}

/** The relationship insert option, when the admin can browse a collection the field allows. */
export function richTextRelationshipOptions(
	editor: LexicalEditor,
	config: RichTextConfig,
	i18n: AdminI18n,
	authoring: FieldAuthoringHost | undefined
): RichTextMenuOption[] {
	if (richTextRelationshipCollections(config, authoring).length === 0) return [];
	return [
		new RichTextMenuOption(
			"relationship",
			i18n.t("plugin.richtext:option.relationship.label"),
			i18n.t("plugin.richtext:option.relationship.description"),
			"relationship",
			keywords(i18n.t("plugin.richtext:option.relationship.keywords")),
			() => queueMicrotask(() => editor.dispatchCommand(OPEN_RELATIONSHIP_BROWSER_COMMAND, {})),
			{ restoreEditorFocus: false }
		),
	];
}
