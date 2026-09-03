import { lexicalImports, lexicalPreprocess } from "@hvniel/lexical-svelte/preprocess";
import { createAdminApplicationConfig } from "@riducms/build/vite";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const adminRoot = fileURLToPath(new URL(".", import.meta.url));

export default createAdminApplicationConfig({
	outDir: resolve(adminRoot, "../internal/adminassets/dist"),
	schemaReloadSignal: resolve(adminRoot, "../.ridu/admin-schema.reload"),
	cacheDir: resolve(adminRoot, "../.ridu/vite"),
	pluginsBeforeSvelte: [lexicalImports()],
	svelte: { preprocess: [lexicalPreprocess()] },
	dedupe: ["@lexical/extension", "lexical"],
	proxyTarget: process.env.RIDU_DEV_API ?? "http://127.0.0.1:8080",
});
