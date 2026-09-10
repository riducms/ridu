import { defineAdminPlugin, definePluginField } from "@riducms/plugin/authoring/v1";
import { decodeRichTextDocument } from "@plugin-richtext/document-validation";
import { decodeRichTextConfig } from "@plugin-richtext/field/rich-text-config";
import Prism from "prismjs";

import { richTextMessages } from "@plugin-richtext/messages";

const browserGlobals = globalThis as typeof globalThis & { Prism?: typeof Prism };
browserGlobals.Prism ??= Prism;

const { default: RichTextField } = await import("@plugin-richtext/field/rich-text-field.svelte");

export const richTextAdminPlugin = defineAdminPlugin({
	key: "richtext",
	pairingVersion: 1,
	fields: {
		richtext: definePluginField({
			component: RichTextField,
			decodeValue: decodeRichTextDocument,
			decodeConfig: decodeRichTextConfig,
		}),
	},
	messages: richTextMessages,
});

export type {
	RichTextDocument,
	RichTextDocumentInput,
	RichTextNode,
	RichTextRootNode,
	RichTextBlockNode,
	RichTextBlockRenderers,
} from "#richtext/document";

export { richTextMessages } from "@plugin-richtext/messages";
export type { RichTextConfig, RichTextFeature } from "@plugin-richtext/field/rich-text-config";
export type { SerializedUploadNode } from "@plugin-richtext/upload/rich-text-upload-node";
