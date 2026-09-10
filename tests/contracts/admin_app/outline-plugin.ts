import {
	defineAdminPlugin,
	definePluginField,
	defineFieldComponent,
} from "@riducms/plugin/authoring/v1";
import OutlineField from "./outline-field.svelte";
import { decodeOutline } from "./outline-value";
export const outlineAdminPlugin = defineAdminPlugin({
	key: "outline",
	pairingVersion: 1,
	components: {
		Outline: defineFieldComponent({
			type: "plugin",
			fieldType: "outline",
			component: OutlineField,
			decodeValue: decodeOutline,
		}),
	},
	fields: { outline: definePluginField({ component: OutlineField, decodeValue: decodeOutline }) },
});
