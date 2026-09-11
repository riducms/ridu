import { readFile, mkdtemp, writeFile, rm } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { spawn } from "node:child_process";
import type { Plugin } from "vite";

const entry = "\0ridu-admin-check-entry";

/**
 * Installed by the shared application config. The CLI invokes the application's
 * actual build script; these hooks only replace its output with registry validation.
 */
export function riduAdminCheckPlugins(): Plugin[] {
	const schemaPath = process.env.RIDU_ADMIN_CHECK_SCHEMA;
	const receipt = process.env.RIDU_ADMIN_CHECK_RECEIPT;
	if (schemaPath === undefined && receipt === undefined) return [];
	if (!schemaPath || !receipt)
		throw new Error(
			"Admin registration verification requires both a schema path and a per-run receipt."
		);
	let root = "";
	return [
		{
			name: "ridu-admin-check-output",
			enforce: "pre",
			config: {
				order: "post",
				handler(config, environment) {
					if (environment.command !== "build" || config.build?.watch || config.build?.ssr)
						throw new Error("Admin registration verification requires a static client build.");
					return {
						build: {
							write: false,
							emptyOutDir: false,
							sourcemap: false,
							manifest: false,
							lib: false,
							rolldownOptions: {
								input: entry,
								preserveEntrySignatures: "strict",
								output: { format: "es", codeSplitting: false },
							},
						},
					};
				},
			},
			configResolved: {
				order: "post",
				handler(config) {
					root = config.root;
					if (config.build.write || config.build.watch || config.build.ssr)
						throw new Error(
							"Admin registration verification requires an in-memory static client build; a Vite hook changed its output settings."
						);
					const output = config.build.rolldownOptions.output;
					if (Array.isArray(output) && output.length !== 1)
						throw new Error(
							"Admin registration verification requires a single static Vite output."
						);
				},
			},
			buildApp: {
				order: "pre",
				async handler(builder) {
					const build = builder.build.bind(builder);
					builder.build = async (environment) => {
						// Environment options and buildApp hooks can override the resolved
						// top-level settings. Check the actual build before it can write.
						const settings = environment.config.build;
						if (
							environment.config.consumer !== "client" ||
							settings.write ||
							settings.watch ||
							settings.ssr ||
							settings.rolldownOptions.input !== entry
						)
							throw new Error(
								"Admin registration verification requires an in-memory static registry build; a Vite environment or hook changed its settings."
							);
						// Inspect the returned output, after all generateBundle/closeBundle
						// hooks. Hook-local chunk objects can be detached bundler snapshots.
						const result = await build(environment);
						const output = Array.isArray(result) && result.length === 1 ? result[0]! : result;
						if (!("output" in output))
							throw new Error(
								"Admin registration verification requires a single static Vite build."
							);
						const chunks = output.output.filter((output) => output.type === "chunk");
						if (chunks.length !== 1 || !chunks[0]?.isEntry)
							throw new Error(
								"Editor checks require one bundled registry. Remove custom output splitting from the admin Vite configuration."
							);
						const schema: unknown = JSON.parse(await readFile(resolve(schemaPath), "utf8"));
						await executeRegistry(root, chunks[0].code, schema, dirname(receipt));
						await writeFile(receipt, "checked\n");
						return result;
					};
				},
			},
		},
		{
			name: "ridu-admin-check-components",
			enforce: "pre",
			async resolveId(source, importer) {
				if (source === entry) return entry;
				if (source.startsWith("\0")) return;
				const resolved = await this.resolve(source, importer, { skipSelf: true });
				if (resolved?.id.split("?", 1)[0]?.endsWith(".svelte")) {
					await readFile(resolved.id.split("?", 1)[0]!);
					return "\0ridu-checked-component:" + resolved.id + ".js";
				}
			},
			load(id) {
				if (id === entry)
					// Match the generated mount entry, including application Vite aliases.
					return `import config from '@/admin.config';
import {resolveAdminConfig, validateAdminManifest} from '@riducms/plugin/admin';
export default function check(schema) {
  if (typeof config !== 'object' || config === null) throw new Error('admin/src/admin.config.ts must default-export defineAdmin({...}).');
  validateAdminManifest(resolveAdminConfig(config), schema, {completeManifest: true});
}`;
				if (id.startsWith("\0ridu-checked-component:"))
					return "export default function RiduCheckedComponent() {}";
			},
			// Earlier application resolvers can bypass resolveId; erase those component
			// sources before Svelte preprocessing/compilation too.
			transform: {
				order: "pre",
				filter: { id: /\.svelte(?:\?|$)/ },
				handler() {
					return { code: "", map: null };
				},
			},
		},
	];
}

async function executeRegistry(root: string, code: string, schema: unknown, temporaryRoot: string) {
	// The CLI also owns this parent directory, so cancellation cannot strand build output.
	const temporary = await mkdtemp(resolve(temporaryRoot, "registry-"));
	try {
		await writeFile(resolve(temporary, "registry.mjs"), code);
		const runner = resolve(temporary, "run.mjs");
		await writeFile(
			runner,
			`import check from './registry.mjs';
try { check(JSON.parse(${JSON.stringify(JSON.stringify(schema))})); }
catch (error) { console.error(error instanceof Error ? error.message : String(error)); process.exitCode = 1; }
`
		);
		// Application initialization and globals stay outside the Vite/CLI process.
		await new Promise<void>((complete, reject) => {
			const child = spawn("node", [runner], { cwd: root, stdio: ["ignore", "pipe", "pipe"] });
			let diagnostic = "";
			child.stdout.on("data", (chunk: Buffer) => {
				diagnostic += chunk.toString();
			});
			child.stderr.on("data", (chunk: Buffer) => {
				diagnostic += chunk.toString();
			});
			child.once("error", reject);
			child.once("close", (code) =>
				code === 0
					? complete()
					: reject(
							new Error(
								diagnostic.trim() || `Admin registration validation exited with status ${code}.`
							)
						)
			);
		});
	} finally {
		await rm(temporary, { recursive: true, force: true });
	}
}
