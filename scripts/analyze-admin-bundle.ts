import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { visualizer } from "rollup-plugin-visualizer";
import { build, type PluginOption } from "vite";

const workspaceRoot = resolve(import.meta.dir, "..");
const fixture = process.argv.includes("--fixture");
const reportRoot = resolve(workspaceRoot, ".ridu/bundle-analysis", fixture ? "fixture" : "core");

process.env.RIDU_ADMIN_ASSET_CHECK = "true";
if (fixture) process.env.RIDU_FRAMEWORK_ADMIN_FIXTURE = "true";
else delete process.env.RIDU_FRAMEWORK_ADMIN_FIXTURE;
process.chdir(resolve(workspaceRoot, "admin"));

mkdirSync(reportRoot, { recursive: true });

await build({
	configFile: resolve(workspaceRoot, "admin/vite.config.ts"),
	build: {
		outDir: resolve(reportRoot, "assets"),
		emptyOutDir: true,
		manifest: true,
		sourcemap: "hidden",
	},
	plugins: [
		visualizer({
			filename: resolve(reportRoot, "stats.html"),
			template: "treemap",
			gzipSize: true,
			brotliSize: true,
			sourcemap: true,
		}) as PluginOption,
		visualizer({
			filename: resolve(reportRoot, "stats.json"),
			template: "raw-data",
			gzipSize: true,
			brotliSize: true,
			sourcemap: true,
		}) as PluginOption,
	],
});

console.log(`Admin bundle report: ${resolve(reportRoot, "stats.html")}`);
