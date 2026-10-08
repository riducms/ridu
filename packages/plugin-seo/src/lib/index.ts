import { defineAdminPlugin, defineFieldComponent } from "@riducms/plugin/authoring/v1";
import {
	decodeString,
	decodeUI,
	decodeLengthConfig,
	decodeOverviewConfig,
	decodeImageConfig,
	decodePreviewConfig,
} from "#lib/seo-config.js";

import { seoMessages } from "#lib/messages.js";

import MetaDescriptionField from "#lib/meta-description-field.svelte";
import MetaImageField from "#lib/meta-image-field.svelte";
import MetaTitleField from "#lib/meta-title-field.svelte";
import OverviewField from "#lib/overview-field.svelte";
import PreviewField from "#lib/preview-field.svelte";

export const seoAdminPlugin = defineAdminPlugin({
	key: "seo",
	pairingVersion: 1,
	fieldEditors: {
		overview: defineFieldComponent({
			type: "ui",
			component: OverviewField,
			decodeValue: decodeUI,
			decodeConfig: decodeOverviewConfig,
		}),
		title: defineFieldComponent({
			type: "text",
			component: MetaTitleField,
			decodeValue: decodeString,
			decodeConfig: (raw: unknown) => decodeLengthConfig(raw, "text"),
		}),
		description: defineFieldComponent({
			type: "textarea",
			component: MetaDescriptionField,
			decodeValue: decodeString,
			decodeConfig: (raw: unknown) => decodeLengthConfig(raw, "textarea"),
		}),
		image: defineFieldComponent({
			type: "upload",
			component: MetaImageField,
			decodeValue: decodeString,
			decodeConfig: decodeImageConfig,
		}),
		preview: defineFieldComponent({
			type: "ui",
			component: PreviewField,
			decodeValue: decodeUI,
			decodeConfig: decodePreviewConfig,
		}),
	},
	messages: seoMessages,
});

export { lengthState, type LengthState, type LengthStatus } from "#lib/length-indicator.js";
export { generationScopeToken, generationSnapshotToken } from "#lib/generation-snapshot.js";
export { seoMessages } from "#lib/messages.js";
