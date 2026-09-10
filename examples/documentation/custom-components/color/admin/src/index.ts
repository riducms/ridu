import {
	defineAdminPlugin,
	definePluginField
} from '@riducms/plugin/authoring/v1';
import ColorField from './color-field.svelte';
import { decodeColor } from './value';
export type { Color, ColorInput } from './value';

export const colorAdminPlugin = defineAdminPlugin({
	// Match the Go plugin's Key and AdminPluginPairingVersion.
	key: 'color',
	pairingVersion: 1,
	fields: {
		color: definePluginField({
			component: ColorField,
			// Check incoming data before the editor receives a Color.
			decodeValue: decodeColor
		})
	}
});
