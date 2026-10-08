import type { AdminI18n, FieldAuthoringHost } from "@riducms/plugin";
import type { SchemaBlockType } from "@riducms/protocol";
import type { LexicalEditor } from "lexical";

import { hasRichTextFeature, type RichTextConfig } from "#lib/field/rich-text-config.js";
import { INSERT_BLOCK_COMMAND } from "#lib/menu/rich-text-commands.js";
import { RichTextMenuOption } from "#lib/menu/rich-text-options.js";

/** One insert option per block type, when the field allows blocks and the admin can create them. */
export function richTextBlockOptions(
	editor: LexicalEditor,
	config: RichTextConfig,
	i18n: AdminI18n,
	authoring: FieldAuthoringHost | undefined,
	blockTypes: readonly SchemaBlockType[]
): RichTextMenuOption[] {
	if (!hasRichTextFeature(config, "blocks") || authoring?.createSchemaPayload === undefined)
		return [];
	return blockTypes.map(
		(type) =>
			new RichTextMenuOption(
				`block-${type.slug}`,
				type.labels.singular,
				i18n.t("plugin.richtext:block.description", { key: type.slug }),
				"block",
				["block", type.slug, type.labels.singular],
				() =>
					queueMicrotask(() =>
						editor.dispatchCommand(INSERT_BLOCK_COMMAND, { blockType: type.slug })
					),
				{ blockType: type.slug, group: "blocks" }
			)
	);
}
