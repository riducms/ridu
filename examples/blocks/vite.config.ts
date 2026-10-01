import { svelte } from "@sveltejs/vite-plugin-svelte";
import { defineConfig } from "vite";
export default defineConfig({
	resolve: {
		alias: [
			{
				find: /^@riducms\/sdk$/,
				replacement: new URL("../../packages/sdk/src/index.ts", import.meta.url).pathname,
			},
			{
				find: /^@riducms\/sdk\/richtext$/,
				replacement: new URL("../../packages/sdk/src/richtext/index.ts", import.meta.url).pathname,
			},
			{
				find: /^@riducms\/protocol$/,
				replacement: new URL("../../packages/protocol/src/index.ts", import.meta.url).pathname,
			},
		],
	},
	plugins: [svelte()],
	server: {
		proxy: { "/api": process.env.RIDU_URL ?? "http://localhost:8080" },
	},
});
