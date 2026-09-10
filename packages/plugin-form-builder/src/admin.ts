import { defineAdminPlugin } from "@riducms/plugin/authoring/v1";

import { formBuilderMessages } from "./messages";

export const formBuilderAdminPlugin = defineAdminPlugin({
	key: "form-builder",
	pairingVersion: 1,
	messages: formBuilderMessages,
});

export { formBuilderMessages } from "./messages";
