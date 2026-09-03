import { lexicalImports, lexicalPreprocess } from "@hvniel/lexical-svelte/preprocess";
import { createAdminApplicationConfig } from "@riducms/build/vite";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const adminRoot = fileURLToPath(new URL(".", import.meta.url));
const frameworkFixtureRoot = resolve(adminRoot, "../tests/contracts/admin_app");
const useFrameworkFixture = process.env.RIDU_FRAMEWORK_ADMIN_FIXTURE === "true";

export default createAdminApplicationConfig({
	outDir: resolve(
		adminRoot,
		process.env.RIDU_ADMIN_ASSET_CHECK === "true"
			? "../.ridu/check/admin-assets"
			: "../internal/adminassets/dist"
	),
	schemaReloadSignal: resolve(adminRoot, "../.ridu/admin-schema.reload"),
	root: useFrameworkFixture ? frameworkFixtureRoot : adminRoot,
	...(useFrameworkFixture ? { cacheDir: resolve(adminRoot, "../.ridu/vite-admin-fixture") } : {}),
	pluginsBeforeSvelte: [lexicalImports()],
	...(process.env.RIDU_ADMIN_DEPENDENCY_INVENTORY === undefined
		? {}
		: {
				dependencyInventory: process.env.RIDU_ADMIN_DEPENDENCY_INVENTORY,
				dependencyInventoryIconSourcePackage: "@iconify/json",
			}),
	svelte: { preprocess: [lexicalPreprocess()] },
	dedupe: ["@internationalized/date", "@lexical/extension", "lexical"],
	optimizeDepsExclude: ["@internationalized/date", "bits-ui"],
	proxyTarget: process.env.RIDU_DEV_API ?? "http://127.0.0.1:8080",
});
