import Prism from "prismjs";

/**
 * Lexical's code highlighting reads Prism from the global scope as it loads. Bundlers can evaluate
 * a statically imported module's dependencies first, so each entry calls this and then imports the
 * editor dynamically.
 */
export function installPrismGlobal() {
	const browserGlobals = globalThis as typeof globalThis & { Prism?: typeof Prism };
	browserGlobals.Prism ??= Prism;
}
