import { defineFieldPlugin } from "@riducms/plugin";

import ColorField from "./ColorField.svelte";

export const colorFieldPlugin = defineFieldPlugin({
	type: "plugin",
	key: "color",
	component: ColorField,
	canRender: (field) => field.plugin?.key === "color",
});
