import { ADMIN_PLUGIN_API_VERSION, defineAdminPlugin } from "@riducms/plugin";

import { formBuilderMessages } from "./messages";

export const formBuilderAdminPlugin = defineAdminPlugin({
	apiVersion: ADMIN_PLUGIN_API_VERSION,
	key: "form-builder",
	pairingVersion: 1,
	fields: [],
	messages: formBuilderMessages,
});

export { formBuilderMessages } from "./messages";
