import { ADMIN_PLUGIN_API_VERSION, defineAdminPlugin, defineFieldPlugin } from "@riducms/plugin";

import { seoMessages } from "@plugin-seo/messages";

import MetaDescriptionField from "@plugin-seo/meta-description-field.svelte";
import MetaImageField from "@plugin-seo/meta-image-field.svelte";
import MetaTitleField from "@plugin-seo/meta-title-field.svelte";
import OverviewField from "@plugin-seo/overview-field.svelte";
import PreviewField from "@plugin-seo/preview-field.svelte";

export const seoFieldPlugins = [
	defineFieldPlugin({
		type: "ui",
		key: "seo",
		componentKey: "overview",
		component: OverviewField,
		canRender: (field) => field.admin.component?.component === "overview",
	}),
	defineFieldPlugin({
		type: "text",
		key: "seo",
		componentKey: "title",
		component: MetaTitleField,
		canRender: (field) => field.admin.component?.component === "title",
	}),
	defineFieldPlugin({
		type: "textarea",
		key: "seo",
		componentKey: "description",
		component: MetaDescriptionField,
		canRender: (field) => field.admin.component?.component === "description",
	}),
	defineFieldPlugin({
		type: "upload",
		key: "seo",
		componentKey: "image",
		component: MetaImageField,
		canRender: (field) => field.admin.component?.component === "image",
	}),
	defineFieldPlugin({
		type: "ui",
		key: "seo",
		componentKey: "preview",
		component: PreviewField,
		canRender: (field) => field.admin.component?.component === "preview",
	}),
] as const;

export const seoAdminPlugin = defineAdminPlugin({
	apiVersion: ADMIN_PLUGIN_API_VERSION,
	key: "seo",
	pairingVersion: 1,
	fields: seoFieldPlugins,
	messages: seoMessages,
});

export { lengthState, type LengthState, type LengthStatus } from "@plugin-seo/length-indicator";
export { generationScopeToken, generationSnapshotToken } from "@plugin-seo/generation-snapshot";
export { seoMessages } from "@plugin-seo/messages";
