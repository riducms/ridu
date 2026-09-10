import { svelte } from "@sveltejs/vite-plugin-svelte";
import { defineConfig } from "vite";
export default defineConfig({
	resolve: {
		alias: {
			"@riducms/sdk": new URL("../../packages/sdk/src/index.ts", import.meta.url).pathname,
			"@riducms/protocol": new URL("../../packages/protocol/src/index.ts", import.meta.url)
				.pathname,
		},
	},
	plugins: [svelte()],
	server: {
		proxy: { "/api": process.env.RIDU_URL ?? "http://localhost:8080" },
	},
});
