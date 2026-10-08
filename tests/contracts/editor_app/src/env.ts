import { defineEnvVars } from "@sveltejs/kit/env";

export const variables = defineEnvVars({
	RIDU_URL: {
		description: "The fixture's Go server, tests/contracts/editor_server.",
		schema: (value) => value || "http://127.0.0.1:4182",
	},
});
