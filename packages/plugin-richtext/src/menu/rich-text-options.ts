import { $createCodeNode } from "@lexical/code";
import {
	INSERT_CHECK_LIST_COMMAND,
	INSERT_ORDERED_LIST_COMMAND,
	INSERT_UNORDERED_LIST_COMMAND,
} from "@lexical/list";
import { $createHeadingNode, $createQuoteNode } from "@lexical/rich-text";
import { $setBlocksType } from "@lexical/selection";
import type { SchemaBlockType } from "@riducms/protocol";
import type { AdminI18n, FieldAuthoringHost } from "@riducms/plugin";
import { INSERT_HORIZONTAL_RULE_COMMAND, MenuOption } from "@hvniel/lexical-svelte";
import {
	$createParagraphNode,
	$getSelection,
	$isRangeSelection,
	type ElementNode,
	type LexicalEditor,
} from "lexical";

import {
	OPEN_RELATIONSHIP_BROWSER_COMMAND,
	OPEN_BLOCK_EDITOR_COMMAND,
	OPEN_UPLOAD_BROWSER_COMMAND,
} from "@plugin-richtext/menu/rich-text-commands";
import { hasRichTextFeature, type RichTextConfig } from "@plugin-richtext/field/rich-text-config";

export class RichTextMenuOption extends MenuOption {
	readonly label: string;
	readonly description: string;
	readonly glyph: string;
	readonly keywords: readonly string[];
	readonly select: () => void;
	readonly keyHint: string;
	readonly preservePlaceholder: boolean;
	readonly restoreEditorFocus: boolean;
	readonly blockType: string | undefined;

	constructor(
		key: string,
		label: string,
		description: string,
		glyph: string,
		keywords: readonly string[],
		select: () => void,
		options: {
			keyHint?: string;
			preservePlaceholder?: boolean;
			restoreEditorFocus?: boolean;
			blockType?: string;
		} = {}
	) {
		super(key);
		this.label = label;
		this.description = description;
		this.glyph = glyph;
		this.keywords = keywords;
		this.select = select;
		this.keyHint = options.keyHint ?? "↵";
		this.preservePlaceholder = options.preservePlaceholder ?? true;
		this.restoreEditorFocus = options.restoreEditorFocus ?? true;
		this.blockType = options.blockType;
	}
}

export function buildRichTextOptions(
	editor: LexicalEditor,
	config: RichTextConfig,
	i18n: AdminI18n,
	authoring?: FieldAuthoringHost,
	blockTypes: readonly SchemaBlockType[] = []
): RichTextMenuOption[] {
	const options = [
		new RichTextMenuOption(
			"paragraph",
			i18n.t("plugin.richtext:option.paragraph.label"),
			i18n.t("plugin.richtext:option.paragraph.description"),
			"¶",
			keywords(i18n.t("plugin.richtext:option.paragraph.keywords")),
			() => $setBlock($createParagraphNode)
		),
		new RichTextMenuOption(
			"heading-2",
			i18n.t("plugin.richtext:option.heading2.label"),
			i18n.t("plugin.richtext:option.heading2.description"),
			"h2",
			keywords(i18n.t("plugin.richtext:option.heading2.keywords")),
			() => $setBlock(() => $createHeadingNode("h2"))
		),
		new RichTextMenuOption(
			"heading-3",
			i18n.t("plugin.richtext:option.heading3.label"),
			i18n.t("plugin.richtext:option.heading3.description"),
			"h3",
			keywords(i18n.t("plugin.richtext:option.heading3.keywords")),
			() => $setBlock(() => $createHeadingNode("h3"))
		),
		new RichTextMenuOption(
			"quote",
			i18n.t("plugin.richtext:option.quote.label"),
			i18n.t("plugin.richtext:option.quote.description"),
			'"',
			keywords(i18n.t("plugin.richtext:option.quote.keywords")),
			() => $setBlock($createQuoteNode)
		),
	];

	if (hasRichTextFeature(config, "lists")) {
		options.push(
			new RichTextMenuOption(
				"bulleted-list",
				i18n.t("plugin.richtext:option.bulletedList.label"),
				i18n.t("plugin.richtext:option.bulletedList.description"),
				"•",
				keywords(i18n.t("plugin.richtext:option.bulletedList.keywords")),
				() => editor.dispatchCommand(INSERT_UNORDERED_LIST_COMMAND, undefined)
			),
			new RichTextMenuOption(
				"numbered-list",
				i18n.t("plugin.richtext:option.numberedList.label"),
				i18n.t("plugin.richtext:option.numberedList.description"),
				"1.",
				keywords(i18n.t("plugin.richtext:option.numberedList.keywords")),
				() => editor.dispatchCommand(INSERT_ORDERED_LIST_COMMAND, undefined)
			),
			new RichTextMenuOption(
				"checklist",
				i18n.t("plugin.richtext:option.checklist.label"),
				i18n.t("plugin.richtext:option.checklist.description"),
				"☑",
				keywords(i18n.t("plugin.richtext:option.checklist.keywords")),
				() => editor.dispatchCommand(INSERT_CHECK_LIST_COMMAND, undefined)
			)
		);
	}

	if (hasRichTextFeature(config, "code")) {
		options.push(
			new RichTextMenuOption(
				"code-block",
				i18n.t("plugin.richtext:option.codeBlock.label"),
				i18n.t("plugin.richtext:option.codeBlock.description"),
				"</>",
				keywords(i18n.t("plugin.richtext:option.codeBlock.keywords")),
				() => $setBlock($createCodeNode)
			)
		);
	}

	if (hasRichTextFeature(config, "horizontal-rule")) {
		options.push(
			new RichTextMenuOption(
				"divider",
				i18n.t("plugin.richtext:option.divider.label"),
				i18n.t("plugin.richtext:option.divider.description"),
				"—",
				keywords(i18n.t("plugin.richtext:option.divider.keywords")),
				() => editor.dispatchCommand(INSERT_HORIZONTAL_RULE_COMMAND, undefined),
				{ preservePlaceholder: false }
			)
		);
	}

	if (hasRichTextFeature(config, "uploads")) {
		const allowed = new Set(config.uploadCollections);
		const uploadOptions: RichTextMenuOption[] = [];
		for (const collection of authoring?.collections ?? []) {
			if (!collection.capabilities.upload || (allowed.size > 0 && !allowed.has(collection.slug))) {
				continue;
			}
			uploadOptions.push(
				new RichTextMenuOption(
					`upload-${collection.slug}`,
					collection.labels.singular,
					i18n.t("plugin.richtext:option.upload.description", {
						label: collection.labels.singular.toLocaleLowerCase(i18n.language),
					}),
					"ref",
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
		options.splice(4, 0, ...uploadOptions);
	}

	if (hasRichTextFeature(config, "relationships")) {
		const allowed = new Set(config.relationshipCollections);
		const relationshipOptions: RichTextMenuOption[] = [];
		for (const collection of authoring?.collections ?? []) {
			if (allowed.size > 0 && !allowed.has(collection.slug)) continue;
			relationshipOptions.push(
				new RichTextMenuOption(
					`relationship-${collection.slug}`,
					i18n.t("plugin.richtext:option.relationship.label", {
						label: collection.labels.singular,
					}),
					i18n.t("plugin.richtext:option.relationship.description", {
						label: collection.labels.singular.toLocaleLowerCase(i18n.language),
					}),
					"rel",
					[...keywords(i18n.t("plugin.richtext:option.relationship.keywords")), collection.slug],
					() =>
						queueMicrotask(() =>
							editor.dispatchCommand(OPEN_RELATIONSHIP_BROWSER_COMMAND, {
								collectionSlug: collection.slug,
							})
						),
					{ restoreEditorFocus: false }
				)
			);
		}
		// Upload and relationship labels can intentionally overlap (for example,
		// Asset and Related Asset). Preserve the established upload result first
		// for keyboard selection and append the explicit relationship commands.
		options.push(...relationshipOptions);
	}

	if (hasRichTextFeature(config, "blocks") && authoring?.beginSchemaDraft !== undefined) {
		options.splice(
			4,
			0,
			...blockTypes.map(
				(type) =>
					new RichTextMenuOption(
						`block-${type.slug}`,
						type.labels.singular,
						i18n.t("plugin.richtext:block.description", { key: type.slug }),
						"▣",
						["block", type.slug, type.labels.singular],
						() =>
							queueMicrotask(() =>
								editor.dispatchCommand(OPEN_BLOCK_EDITOR_COMMAND, { blockType: type.slug })
							),
						{ restoreEditorFocus: false, blockType: type.slug }
					)
			)
		);
	}
	return options;
}

export function filterRichTextOptions(
	options: readonly RichTextMenuOption[],
	query: string,
	language?: string
): RichTextMenuOption[] {
	const normalized = query.trim().toLocaleLowerCase(language);
	if (normalized === "") return [...options];
	return options.filter(
		(option) =>
			option.label.toLocaleLowerCase(language).includes(normalized) ||
			option.description.toLocaleLowerCase(language).includes(normalized) ||
			option.keywords.some((keyword) => keyword.toLocaleLowerCase(language).includes(normalized))
	);
}

function keywords(value: string) {
	return value.split(/\s+/).filter(Boolean);
}

// @lexical-scope
function $setBlock(createBlock: () => ElementNode) {
	const selection = $getSelection();
	if ($isRangeSelection(selection)) $setBlocksType(selection, createBlock);
}
