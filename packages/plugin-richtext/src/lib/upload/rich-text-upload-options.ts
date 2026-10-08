import type { AdminI18n, FieldAuthoringHost } from "@riducms/plugin";
import type { LexicalEditor } from "lexical";

import type { RichTextConfig } from "#lib/field/rich-text-config.js";
import { OPEN_UPLOAD_BROWSER_COMMAND } from "#lib/menu/rich-text-commands.js";
import { keywords, RichTextMenuOption } from "#lib/menu/rich-text-options.js";

/** The upload collections the field allows and the admin can browse. */
export function richTextUploadCollections(
	config: RichTextConfig,
	authoring: FieldAuthoringHost | undefined
) {
	return (authoring?.collections ?? []).filter(
		(collection) =>
			collection.capabilities.upload &&
			(config.uploadCollections.length === 0 || config.uploadCollections.includes(collection.slug))
	);
}

/** One insert option per upload collection the field allows and the admin can browse. */
export function richTextUploadOptions(
	editor: LexicalEditor,
	config: RichTextConfig,
	i18n: AdminI18n,
	authoring: FieldAuthoringHost | undefined
): RichTextMenuOption[] {
	return richTextUploadCollections(config, authoring).map(
		(collection) =>
			new RichTextMenuOption(
				`upload-${collection.slug}`,
				collection.labels.singular,
				i18n.t("plugin.richtext:option.upload.description", {
					label: collection.labels.singular.toLocaleLowerCase(i18n.language),
				}),
				"upload",
				[...keywords(i18n.t("plugin.richtext:option.upload.keywords")), collection.slug],
				() =>
					queueMicrotask(() =>
						editor.dispatchCommand(OPEN_UPLOAD_BROWSER_COMMAND, {
							collectionSlug: collection.slug,
						})
					),
				{ restoreEditorFocus: false }
			)
	);
}
