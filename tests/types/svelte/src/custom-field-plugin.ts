import { definePluginField } from "@riducms/plugin/authoring/v1";

import ColorField from "./ColorField.svelte";

export const colorFieldPlugin = definePluginField({
	component: ColorField,
	decodeValue: (value: unknown): string => {
		if (typeof value !== "string") throw new Error("Expected string");
		return value;
	},
});
