import { ADMIN_PLUGIN_API_VERSION, defineAdminPlugin, defineFieldPlugin } from "@riducms/plugin";
import Prism from "prismjs";

import { richTextMessages } from "@plugin-richtext/messages";

const browserGlobals = globalThis as typeof globalThis & { Prism?: typeof Prism };
browserGlobals.Prism ??= Prism;

const { default: RichTextField } = await import("@plugin-richtext/field/rich-text-field.svelte");

export const richTextFieldPlugin = defineFieldPlugin({
	type: "plugin",
	key: "richtext",
	component: RichTextField,
	canRender: (field) => field.plugin?.key === "richtext",
});

export const richTextAdminPlugin = defineAdminPlugin({
	apiVersion: ADMIN_PLUGIN_API_VERSION,
	key: "richtext",
	pairingVersion: 1,
	fields: [richTextFieldPlugin],
	messages: richTextMessages,
});

export interface RichTextDocument {
	version: 1;
	root: Record<string, unknown>;
}

export { richTextMessages } from "@plugin-richtext/messages";
export type { RichTextConfig, RichTextFeature } from "@plugin-richtext/field/rich-text-config";
export type { SerializedUploadNode } from "@plugin-richtext/upload/rich-text-upload-node";
