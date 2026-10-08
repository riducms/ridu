import type { RichTextIconName } from "#lib/toolbar/toolbar-icon.svelte";
import { $createCodeNode } from "@lexical/code";
import {
	INSERT_CHECK_LIST_COMMAND,
	INSERT_ORDERED_LIST_COMMAND,
	INSERT_UNORDERED_LIST_COMMAND,
} from "@lexical/list";
import { $createHeadingNode, $createQuoteNode, type HeadingTagType } from "@lexical/rich-text";
import { $setBlocksType } from "@lexical/selection";
import type { AdminI18n } from "@riducms/plugin";
import { INSERT_HORIZONTAL_RULE_COMMAND, MenuOption } from "@hvniel/lexical-svelte";
import {
	$createParagraphNode,
	$getSelection,
	$isRangeSelection,
	type ElementNode,
	type LexicalEditor,
} from "lexical";

import type { RichTextEditorExtension } from "#lib/editor/rich-text-extension.js";
import type { RichTextEditorFeature } from "#lib/field/rich-text-config.js";

export { filterRichTextOptions } from "#lib/menu/rich-text-option-filter.js";

export class RichTextMenuOption extends MenuOption {
	readonly label: string;
	readonly description: string;
	readonly icon: RichTextIconName;
	readonly group: "lists" | "basic" | "blocks";
	readonly keywords: readonly string[];
	readonly select: () => void;
	readonly preservePlaceholder: boolean;
	readonly restoreEditorFocus: boolean;
	readonly blockType: string | undefined;

	constructor(
		key: string,
		label: string,
		description: string,
		icon: RichTextIconName,
		keywords: readonly string[],
		select: () => void,
		options: {
			group?: "lists" | "basic" | "blocks";
			preservePlaceholder?: boolean;
			restoreEditorFocus?: boolean;
			blockType?: string;
		} = {}
	) {
		super(key);
		this.label = label;
		this.description = description;
		this.icon = icon;
		this.group = options.group ?? "basic";
		this.keywords = keywords;
		this.select = select;
		this.preservePlaceholder = options.preservePlaceholder ?? true;
		this.restoreEditorFocus = options.restoreEditorFocus ?? true;
		this.blockType = options.blockType;
	}
}

export function buildRichTextOptions(
	editor: LexicalEditor,
	features: readonly RichTextEditorFeature[],
	i18n: AdminI18n,
	extensions: readonly RichTextEditorExtension[]
): RichTextMenuOption[] {
	const options = [
		new RichTextMenuOption(
			"paragraph",
			i18n.t("plugin.richtext:option.paragraph.label"),
			i18n.t("plugin.richtext:option.paragraph.description"),
			"paragraph",
			keywords(i18n.t("plugin.richtext:option.paragraph.keywords")),
			() => $setBlock($createParagraphNode)
		),
		...[1, 2, 3, 4, 5, 6].map(
			(level) =>
				new RichTextMenuOption(
					`heading-${level}`,
					i18n.t("plugin.richtext:editor.heading", { level }),
					"",
					`h${level}` as RichTextIconName,
					["heading", "title", `h${level}`],
					() => $setBlock(() => $createHeadingNode(`h${level}` as HeadingTagType))
				)
		),
		new RichTextMenuOption(
			"quote",
			i18n.t("plugin.richtext:option.quote.label"),
			i18n.t("plugin.richtext:option.quote.description"),
			"quote",
			keywords(i18n.t("plugin.richtext:option.quote.keywords")),
			() => $setBlock($createQuoteNode)
		),
	];

	if (features.includes("lists")) {
		options.push(
			new RichTextMenuOption(
				"bulleted-list",
				i18n.t("plugin.richtext:option.bulletedList.label"),
				i18n.t("plugin.richtext:option.bulletedList.description"),
				"unordered",
				keywords(i18n.t("plugin.richtext:option.bulletedList.keywords")),
				() => editor.dispatchCommand(INSERT_UNORDERED_LIST_COMMAND, undefined),
				{ group: "lists" }
			),
			new RichTextMenuOption(
				"numbered-list",
				i18n.t("plugin.richtext:option.numberedList.label"),
				i18n.t("plugin.richtext:option.numberedList.description"),
				"ordered",
				keywords(i18n.t("plugin.richtext:option.numberedList.keywords")),
				() => editor.dispatchCommand(INSERT_ORDERED_LIST_COMMAND, undefined),
				{ group: "lists" }
			),
			new RichTextMenuOption(
				"checklist",
				i18n.t("plugin.richtext:option.checklist.label"),
				i18n.t("plugin.richtext:option.checklist.description"),
				"check",
				keywords(i18n.t("plugin.richtext:option.checklist.keywords")),
				() => editor.dispatchCommand(INSERT_CHECK_LIST_COMMAND, undefined),
				{ group: "lists" }
			)
		);
	}

	if (features.includes("code")) {
		options.push(
			new RichTextMenuOption(
				"code-block",
				i18n.t("plugin.richtext:option.codeBlock.label"),
				i18n.t("plugin.richtext:option.codeBlock.description"),
				"code-block",
				keywords(i18n.t("plugin.richtext:option.codeBlock.keywords")),
				() => $setBlock($createCodeNode)
			)
		);
	}

	if (features.includes("horizontal-rule")) {
		options.push(
			new RichTextMenuOption(
				"divider",
				i18n.t("plugin.richtext:option.divider.label"),
				i18n.t("plugin.richtext:option.divider.description"),
				"divider",
				keywords(i18n.t("plugin.richtext:option.divider.keywords")),
				() => editor.dispatchCommand(INSERT_HORIZONTAL_RULE_COMMAND, undefined),
				{ preservePlaceholder: false }
			)
		);
	}

	options.push(...extensions.flatMap((extension) => extension.options?.(editor, i18n) ?? []));
	// Later basic features move their merged group after the list group in Payload's default menu.
	const groupOrder = { lists: 0, basic: 1, blocks: 2 };
	const iconOrder: RichTextIconName[] = [
		"check",
		"ordered",
		"unordered",
		"divider",
		"upload",
		"quote",
		"relationship",
		"h1",
		"h2",
		"h3",
		"h4",
		"h5",
		"h6",
		"paragraph",
		"code-block",
		"block",
	];
	return options.sort(
		(a, b) =>
			groupOrder[a.group] - groupOrder[b.group] ||
			iconOrder.indexOf(a.icon) - iconOrder.indexOf(b.icon)
	);
}

/** Split a translated keyword list into the words a menu search matches. */
export function keywords(value: string) {
	return value.split(/\s+/).filter(Boolean);
}

// @lexical-scope
function $setBlock(createBlock: () => ElementNode) {
	const selection = $getSelection();
	if ($isRangeSelection(selection)) $setBlocksType(selection, createBlock);
}
