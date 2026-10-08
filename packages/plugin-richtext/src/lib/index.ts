import { defineAdminPlugin, definePluginField } from "@riducms/plugin/authoring/v1";
import { decodeRichTextConfig } from "#lib/field/rich-text-config.js";
import { decodeRichTextDocument } from "#lib/field/rich-text-value.js";

import { richTextMessages } from "#lib/messages.js";
import { installPrismGlobal } from "#lib/prism.js";

installPrismGlobal();
const { default: RichTextField } = await import("#lib/field/rich-text-field.svelte");

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

export { richTextMessages } from "#lib/messages.js";
export type { RichTextConfig, RichTextFeature } from "#lib/field/rich-text-config.js";
export type { SerializedUploadNode } from "#lib/upload/rich-text-upload-node.js";
