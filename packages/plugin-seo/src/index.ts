import { defineAdminPlugin, defineFieldComponent } from "@riducms/plugin/authoring/v1";
import {
	decodeString,
	decodeUI,
	decodeLengthConfig,
	decodeOverviewConfig,
	decodeImageConfig,
	decodePreviewConfig,
} from "@plugin-seo/seo-config";

import { seoMessages } from "@plugin-seo/messages";

import MetaDescriptionField from "@plugin-seo/meta-description-field.svelte";
import MetaImageField from "@plugin-seo/meta-image-field.svelte";
import MetaTitleField from "@plugin-seo/meta-title-field.svelte";
import OverviewField from "@plugin-seo/overview-field.svelte";
import PreviewField from "@plugin-seo/preview-field.svelte";

export const seoAdminPlugin = defineAdminPlugin({
	key: "seo",
	pairingVersion: 1,
	components: {
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

export { lengthState, type LengthState, type LengthStatus } from "@plugin-seo/length-indicator";
export { generationScopeToken, generationSnapshotToken } from "@plugin-seo/generation-snapshot";
export { seoMessages } from "@plugin-seo/messages";
