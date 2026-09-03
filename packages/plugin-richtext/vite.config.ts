import { lexicalImports, lexicalPreprocess } from "@hvniel/lexical-svelte/preprocess";
import { createAdminLibraryConfig } from "@riducms/build/vite";

export default createAdminLibraryConfig({
	pluginsBeforeSvelte: [lexicalImports()],
	svelte: { preprocess: [lexicalPreprocess()] },
	dedupe: ["@lexical/extension", "lexical"],
});
