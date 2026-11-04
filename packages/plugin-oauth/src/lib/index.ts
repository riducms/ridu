import { defineAdminPlugin } from "@riducms/plugin/authoring/v1";

import ConsentRoute from "#lib/consent-route.svelte";
import { oauthMessages } from "#lib/messages.js";

/**
 * The admin companion of `github.com/riducms/ridu/plugins/oauth`: the consent page at
 * `/admin/oauth/authorize`, where a signed-in person approves an OAuth client such as an MCP
 * connector and chooses whom it acts as.
 */
export const oauthAdminPlugin = defineAdminPlugin({
	key: "oauth",
	pairingVersion: 1,
	routes: [{ path: "oauth/authorize", component: ConsentRoute }],
	messages: oauthMessages,
});

export { oauthMessages } from "#lib/messages.js";
