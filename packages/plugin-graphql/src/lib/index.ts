import { withAdminLoader } from "@riducms/plugin";
import { defineAdminPlugin } from "@riducms/plugin/authoring/v1";

import { playgroundLoader } from "#lib/loader.js";
import { graphqlMessages } from "#lib/messages.js";
import PlaygroundRoute from "#lib/playground-route.svelte";

/** The admin companion of `github.com/riducms/ridu/plugins/graphql`: the GraphQL playground. */
export const graphqlAdminPlugin = defineAdminPlugin({
	key: "graphql",
	pairingVersion: 1,
	routes: [
		{
			path: "graphql",
			navigation: { label: "GraphQL", labelKey: "plugin.graphql:navigation" },
			...withAdminLoader(playgroundLoader, PlaygroundRoute),
		},
	],
	messages: graphqlMessages,
});

export { graphqlMessages } from "#lib/messages.js";
