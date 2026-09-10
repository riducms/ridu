import { afterEach, expect, test } from "bun:test";
import { mkdtemp, mkdir, readFile, writeFile, rm, symlink, readdir } from "node:fs/promises";
import { resolve } from "node:path";

async function runBuild(root: string, environment: Record<string, string> = {}) {
	const child = Bun.spawn(["bun", "run", "build"], {
		cwd: root,
		env: { ...process.env, ...environment },
		stdout: "pipe",
		stderr: "pipe",
	});
	const [stdout, stderr, code] = await Promise.all([
		new Response(child.stdout).text(),
		new Response(child.stderr).text(),
		child.exited,
	]);
	return { code, diagnostic: stdout + stderr };
}

// Exercise the actual package script and the same receipt contract as the Go CLI.
async function checkAdminRegistrations({ root, schema }: { root: string; schema: string }) {
	const cache = resolve(root, "../.ridu");
	await mkdir(cache, { recursive: true });
	const run = await mkdtemp(resolve(cache, "admin-check-"));
	try {
		const receipt = resolve(run, "complete");
		const result = await runBuild(root, {
			RIDU_ADMIN_CHECK_SCHEMA: schema,
			RIDU_ADMIN_CHECK_RECEIPT: receipt,
		});
		if (result.code !== 0) throw new Error(result.diagnostic);
		if ((await readFile(receipt, "utf8")) !== "checked\n")
			throw new Error("Missing editor verification receipt");
	} finally {
		await rm(run, { recursive: true, force: true });
	}
}

async function buildScript(root: string, command = "vite build") {
	await writeFile(
		resolve(root, "package.json"),
		JSON.stringify({ type: "module", scripts: { build: command } })
	);
}

const temporary: string[] = [];
const originalNodeEnvironment = process.env.NODE_ENV;
const originalServerFlag = process.env.RIDU_EDITOR_TEST_SERVER_FLAG;
afterEach(async () => {
	if (originalNodeEnvironment === undefined) delete process.env.NODE_ENV;
	else process.env.NODE_ENV = originalNodeEnvironment;
	if (originalServerFlag === undefined) delete process.env.RIDU_EDITOR_TEST_SERVER_FLAG;
	else process.env.RIDU_EDITOR_TEST_SERVER_FLAG = originalServerFlag;
	for (const directory of temporary.splice(0))
		await rm(directory, { recursive: true, force: true });
});

test("browser-free checks execute the real registry and decoder while keeping component bodies inert", async () => {
	// Bun sets NODE_ENV=test; exercise the environment of an ordinary production CLI build.
	process.env.NODE_ENV = "production";
	process.env.RIDU_EDITOR_TEST_SERVER_FLAG = "must not reach the client";
	await mkdir(".ridu", { recursive: true });
	const project = await mkdtemp(resolve(".ridu/admin-check-"));
	temporary.push(project);
	const root = resolve(project, "admin");
	await mkdir(root);
	await buildScript(root);
	await symlink(resolve("admin/node_modules"), resolve(root, "node_modules"), "dir");
	await mkdir(resolve(root, "src"));
	const inventory = resolve(root, "inventory.json");
	await writeFile(inventory, "existing production inventory");
	await writeFile(
		resolve(root, "vite.config.ts"),
		`import {createAdminApplicationConfig} from "@riducms/build/vite";
const application = createAdminApplicationConfig({outDir:"./out",schemaReloadSignal:"./reload",dependencyInventory:${JSON.stringify(inventory)},dependencyInventoryIconSourcePackage:'@iconify/json'});
export default async (environment) => ({...await application(environment), define:{__EDITOR_COMMAND__:JSON.stringify(environment.command)}});`
	);
	await writeFile(resolve(root, ".env.production"), "VITE_EDITOR_REFERENCE=app:color\n");
	await writeFile(
		resolve(root, "src/color.svelte"),
		`<script>throw new Error("Component bodies must not execute during registry checks");</script>`
	);
	const schema = resolve(root, "schema.json");
	await writeFile(
		schema,
		JSON.stringify({
			collections: [
				{
					slug: "brands",
					fields: [
						{
							type: "text",
							path: "accent",
							admin: { editor: { reference: "app:color", config: { palette: ["red"] } } },
						},
					],
				},
			],
			globals: [],
		})
	);
	const configFile = resolve(root, "src/admin.config.ts");
	const header = `import { defineAdmin } from '@riducms/plugin/admin';
import { defineFieldEditor } from '@riducms/plugin/editor';\nimport Color from './color.svelte';\n`;
	const registration = `defineFieldEditor({type:'text',component:Color,decodeConfig(value){if (!Array.isArray(value.palette)) throw new Error('palette must be an array'); return value;}})`;
	await writeFile(
		configFile,
		`${header}
if (__EDITOR_COMMAND__ !== 'build' || import.meta.env.MODE !== 'production' || import.meta.env.SSR || !import.meta.env.PROD) throw new Error('check must match production client compilation');
if (process.env.RIDU_EDITOR_TEST_SERVER_FLAG) throw new Error('server environment must not change browser registrations');
export default defineAdmin({fields:{[import.meta.env.VITE_EDITOR_REFERENCE]:${registration}}});`
	);
	await checkAdminRegistrations({ root, schema });

	await writeFile(
		schema,
		JSON.stringify({
			collections: [
				{
					slug: "brands",
					fields: [
						{
							type: "text",
							path: "accent",
							admin: { editor: { reference: "app:color", config: { palette: false } } },
						},
					],
				},
			],
			globals: [],
		})
	);
	await expect(checkAdminRegistrations({ root, schema })).rejects.toThrow(
		"palette must be an array"
	);
	expect(await readFile(inventory, "utf8")).toBe("existing production inventory");
	expect(await readdir(resolve(root, "../.ridu"))).toEqual([]);
}, 30_000);

test("verification follows the build script's mode, config file, environment, aliases and final hooks", async () => {
	process.env.NODE_ENV = "production";
	await mkdir(".ridu", { recursive: true });
	const project = await mkdtemp(resolve(".ridu/admin-check-invocation-"));
	temporary.push(project);
	const root = resolve(project, "admin");
	await mkdir(resolve(root, "src"), { recursive: true });
	await symlink(resolve("admin/node_modules"), resolve(root, "node_modules"), "dir");
	await writeFile(
		resolve(root, "src/editor.svelte"),
		"<script>throw new Error('component body executed');</script>"
	);
	await writeFile(
		resolve(root, "src/admin.config.ts"),
		`import { defineAdmin } from '@riducms/plugin/admin';
import { defineFieldEditor } from '@riducms/plugin/editor';
import Editor from '@application-editor';
globalThis.riduApplicationGlobal = true;
export default defineAdmin({fields:{[import.meta.env.VITE_EDITOR_REFERENCE]:defineFieldEditor({type:'text',component:Editor})}});`
	);
	const schema = resolve(project, "schema.json");
	await writeFile(
		schema,
		JSON.stringify({
			collections: [
				{
					slug: "posts",
					fields: [{ type: "text", path: "title", admin: { editor: { reference: "app:text" } } }],
				},
			],
			globals: [],
		})
	);
	const config = `import {createAdminApplicationConfig} from '@riducms/build/vite';
import {writeFileSync} from 'node:fs';
const application = createAdminApplicationConfig({outDir:'./out',schemaReloadSignal:'./reload'});
export default async (environment) => {
  const config = await application(environment);
  return {...config, resolve:{...config.resolve,alias:{'@application-editor':${JSON.stringify(resolve(root, "src/editor.svelte"))}}}, plugins:[...config.plugins,{
    name:'application-build-hooks', enforce:'post',
    generateBundle:{order:'post',handler(_options,bundle){
      if(globalThis.riduApplicationGlobal) throw new Error('Application globals leaked into Vite');
      for(const chunk of Object.values(bundle)) if(chunk.type === 'chunk') chunk.code = chunk.code.replaceAll('app:hook', 'app:missing');
    }},
    writeBundle(){writeFileSync(${JSON.stringify(resolve(root, "write-hook"))},'production');}
  }]};
};`;
	await writeFile(resolve(root, "vite.config.ts"), config);
	await writeFile(resolve(root, ".env.production"), "VITE_EDITOR_REFERENCE=app:text\n");
	await writeFile(resolve(root, ".env.staging"), "VITE_EDITOR_REFERENCE=app:staging\n");
	await buildScript(root, "vite build");
	await checkAdminRegistrations({ root, schema });
	// The generated admin imports this public application alias, so the checker
	// must resolve the registry through it instead of pinning a different file.
	await writeFile(resolve(root, "src/alternate.config.ts"), "export default {fields:{}};");
	await writeFile(
		resolve(root, "vite.config.ts"),
		config.replace(
			"alias:{",
			`alias:{'@/admin.config':${JSON.stringify(resolve(root, "src/alternate.config.ts"))},`
		)
	);
	await expect(checkAdminRegistrations({ root, schema })).rejects.toThrow(
		"posts.title: Editor app:text is not registered"
	);
	const registrySource = await readFile(resolve(root, "src/admin.config.ts"), "utf8");
	await writeFile(resolve(root, "src/alternate.config.ts"), registrySource);
	await writeFile(resolve(root, "src/admin.config.ts"), "export default {fields:{}};");
	await checkAdminRegistrations({ root, schema });
	await writeFile(resolve(root, "src/admin.config.ts"), registrySource);
	await writeFile(resolve(root, "vite.config.ts"), config);
	// This was the false positive: verification used production while shipping staging.
	await buildScript(root, "vite build --mode staging");
	await expect(checkAdminRegistrations({ root, schema })).rejects.toThrow(
		"posts.title: Editor app:text is not registered"
	);
	await writeFile(resolve(root, "vite.shipped.ts"), config);
	await writeFile(
		resolve(root, "vite.config.ts"),
		"throw new Error('The default config must not be loaded');"
	);
	await buildScript(
		root,
		"VITE_EDITOR_REFERENCE=app:text vite build --config vite.shipped.ts --mode staging"
	);
	await checkAdminRegistrations({ root, schema });
	await expect(readFile(resolve(root, "write-hook"), "utf8")).rejects.toThrow();
	await expect(readdir(resolve(root, "out"))).rejects.toThrow();
	await writeFile(
		resolve(root, "vite.shipped.ts"),
		config.replace(
			"name:'application-build-hooks', enforce:'post',",
			`name:'application-build-hooks', enforce:'post',
    async buildApp(builder){builder.environments.client.config.build.write = true;},`
		)
	);
	await expect(checkAdminRegistrations({ root, schema })).rejects.toThrow(
		"a Vite environment or hook changed its settings"
	);
	await expect(readdir(resolve(root, "out"))).rejects.toThrow();
	await writeFile(resolve(root, "vite.shipped.ts"), config);
	await buildScript(
		root,
		"VITE_EDITOR_REFERENCE=app:hook vite build --config vite.shipped.ts --mode staging"
	);
	// The final application hook changes the otherwise valid registration before validation.
	await writeFile(
		schema,
		JSON.stringify({
			collections: [
				{
					slug: "posts",
					fields: [{ type: "text", path: "title", admin: { editor: { reference: "app:hook" } } }],
				},
			],
			globals: [],
		})
	);
	await expect(checkAdminRegistrations({ root, schema })).rejects.toThrow(
		"posts.title: Editor app:hook is not registered"
	);
	// Ordinary builds keep their real entry/output and still run writeBundle hooks.
	await buildScript(
		root,
		"VITE_EDITOR_REFERENCE=app:text vite build --config vite.shipped.ts --mode staging"
	);
	await writeFile(
		resolve(root, "index.html"),
		'<script type="module" src="/src/main.ts"></script>'
	);
	await writeFile(
		resolve(root, "src/main.ts"),
		"import config from '@/admin.config'; globalThis.selectedEditors = Object.keys(config.fields);"
	);
	const shipped = await runBuild(root);
	expect(shipped.code, shipped.diagnostic).toBe(0);
	expect(await readFile(resolve(root, "write-hook"), "utf8")).toBe("production");
	expect(await readFile(resolve(root, "out/index.html"), "utf8")).toContain("assets/");
	expect(await readdir(resolve(project, ".ridu"))).toEqual([]);
}, 30_000);

test("verification preserves production minification seen by application hooks", async () => {
	process.env.NODE_ENV = "production";
	await mkdir(".ridu", { recursive: true });
	const project = await mkdtemp(resolve(".ridu/admin-check-minification-"));
	temporary.push(project);
	const root = resolve(project, "admin");
	await mkdir(resolve(root, "src"), { recursive: true });
	await symlink(resolve("admin/node_modules"), resolve(root, "node_modules"), "dir");
	await buildScript(root);
	await writeFile(resolve(root, "src/editor.svelte"), "<input />");
	await writeFile(
		resolve(root, "src/admin.config.ts"),
		`import { defineAdmin } from '@riducms/plugin/admin';
import { defineFieldEditor } from '@riducms/plugin/editor';
import Editor from './editor.svelte';
export default defineAdmin({fields:{[__EDITOR_REFERENCE__]:defineFieldEditor({type:'text',component:Editor})}});`
	);
	const schema = resolve(project, "schema.json");
	await writeFile(
		schema,
		JSON.stringify({
			collections: [
				{
					slug: "posts",
					fields: [{ type: "text", path: "title", admin: { editor: { reference: "app:text" } } }],
				},
			],
		})
	);
	const config = `import {createAdminApplicationConfig} from '@riducms/build/vite';
const application = createAdminApplicationConfig({outDir:'./out',schemaReloadSignal:'./reload'});
export default async environment => {
  const config = await application(environment);
  return {...config,define:{},plugins:[...config.plugins,{
    name:'minification-dependent-registration',
    configResolved(config){
      config.define.__EDITOR_REFERENCE__ = JSON.stringify(config.build.minify === false ? 'app:text' : 'app:missing');
    }
  }]};
};`;
	await writeFile(resolve(root, "vite.config.ts"), config);
	// Previously the checker disabled minification and accepted app:text while the
	// shipped build selected app:missing through the same deterministic hook.
	await expect(checkAdminRegistrations({ root, schema })).rejects.toThrow(
		"posts.title: Editor app:text is not registered"
	);
	await writeFile(
		resolve(root, "vite.config.ts"),
		config.replace(
			"return {...config,define:",
			"return {...config,build:{...config.build,minify:false},define:"
		)
	);
	await checkAdminRegistrations({ root, schema });
	expect(await readdir(resolve(project, ".ridu"))).toEqual([]);
}, 30_000);

test("custom pre-resolvers cannot bypass inert component checking", async () => {
	process.env.NODE_ENV = "production";
	await mkdir(".ridu", { recursive: true });
	const project = await mkdtemp(resolve(".ridu/admin-check-resolver-"));
	temporary.push(project);
	const root = resolve(project, "admin");
	await mkdir(root);
	await buildScript(root);
	await symlink(resolve("admin/node_modules"), resolve(root, "node_modules"), "dir");
	await mkdir(resolve(root, "src"));
	await writeFile(
		resolve(root, "vite.config.ts"),
		`import {createAdminApplicationConfig} from '@riducms/build/vite';
export default createAdminApplicationConfig({outDir:'./out',schemaReloadSignal:'./reload',pluginsBeforeSvelte:[{
  name:'application-component-resolver',enforce:'pre',
  resolveId(source) { if(source === 'application-editor') return ${JSON.stringify(resolve(root, "src/editor.svelte"))}; }
}]});`
	);
	await writeFile(
		resolve(root, "src/editor.svelte"),
		`<script module>throw new Error('component module executed');</script><script>throw new Error('component body executed');</script>`
	);
	await writeFile(
		resolve(root, "src/admin.config.ts"),
		`import { defineAdmin } from '@riducms/plugin/admin';
import { defineFieldEditor } from '@riducms/plugin/editor';
import Editor from 'application-editor';
export default defineAdmin({fields:{'app:text':defineFieldEditor({type:'text',component:Editor})}});`
	);
	const schema = resolve(root, "schema.json");
	await writeFile(schema, JSON.stringify({ collections: [], globals: [] }));
	await checkAdminRegistrations({ root, schema });
}, 30_000);

test("production registry checks validate local surfaces and row labels without mounting them", async () => {
	process.env.NODE_ENV = "production";
	await mkdir(".ridu", { recursive: true });
	const project = await mkdtemp(resolve(".ridu/admin-check-surfaces-"));
	temporary.push(project);
	const root = resolve(project, "admin");
	await mkdir(resolve(root, "src"), { recursive: true });
	await buildScript(root);
	await symlink(resolve("admin/node_modules"), resolve(root, "node_modules"), "dir");
	await writeFile(
		resolve(root, "vite.config.ts"),
		`import {createAdminApplicationConfig} from '@riducms/build/vite'; export default createAdminApplicationConfig({outDir:'./out',schemaReloadSignal:'./reload',dependencyInventoryIconSourcePackage:'@iconify/json'});`
	);
	await writeFile(
		resolve(root, "src/component.svelte"),
		`<script>throw new Error('component bodies must stay inert');</script>`
	);
	const schema = resolve(root, "schema.json");
	await writeFile(
		schema,
		JSON.stringify({
			collections: [
				{
					slug: "posts",
					fields: [
						{
							name: "rows",
							path: "rows",
							type: "array",
							admin: {},
							nested: {
								fields: [],
								rowLabelComponent: { reference: "app:summary", config: { title: "Summary" } },
							},
						},
					],
				},
			],
			globals: [],
		})
	);
	const header = `import {defineAdmin,defineRowLabel} from '@riducms/plugin/admin'; import Component from './component.svelte';`;
	const label = `rowLabels:{'app:summary':defineRowLabel({component:Component,decodeConfig(value){if(value.title !== 'Summary') throw new Error('title required'); return value;}})}`;
	await writeFile(
		resolve(root, "src/admin.config.ts"),
		`${header} export default defineAdmin({${label}, dashboard:[{key:'summary',component:Component}],listCells:[{key:'rows',collection:'posts',field:'rows',label:'Rows',component:Component}],documentActions:[{key:'review',collection:'posts',requires:'update',component:Component}]});`
	);
	await checkAdminRegistrations({ root, schema });
	await writeFile(
		resolve(root, "src/admin.config.ts"),
		`${header} export default defineAdmin({dashboard:[{key:'summary',component:Component}]});`
	);
	await expect(checkAdminRegistrations({ root, schema })).rejects.toThrow(
		"row label app:summary is not registered"
	);
	expect(await readdir(resolve(root, "../.ridu"))).toEqual([]);
	expect(await readdir(root)).not.toContain("out");
});

test("plugin registration checks use canonical ownership, pairing, contracts and decoded config", async () => {
	process.env.NODE_ENV = "production";
	await mkdir(".ridu", { recursive: true });
	const project = await mkdtemp(resolve(".ridu/plugin-registration-check-"));
	temporary.push(project);
	const root = resolve(project, "admin");
	await mkdir(resolve(root, "src"), { recursive: true });
	await buildScript(root);
	await symlink(resolve("admin/node_modules"), resolve(root, "node_modules"), "dir");
	await writeFile(
		resolve(root, "vite.config.ts"),
		`import {createAdminApplicationConfig} from '@riducms/build/vite'; export default createAdminApplicationConfig({outDir:'./out',schemaReloadSignal:'./reload'});`
	);
	await writeFile(
		resolve(root, "src/field.svelte"),
		`<script>throw new Error('must not execute plugin component bodies');</script>`
	);
	const schema = resolve(root, "schema.json");
	const manifest = {
		plugins: [
			{
				key: "shapes",
				admin: { apiVersion: 1, pairingVersion: 3, package: "@example/shapes", export: "shapes" },
				fieldTypes: [{ key: "color" }, { key: "outline" }],
			},
		],
		collections: [
			{
				slug: "posts",
				fields: [
					{
						type: "plugin",
						path: "accent",
						name: "accent",
						admin: {},
						plugin: { key: "color", config: { limit: 5 } },
					},
				],
			},
		],
		globals: [],
	};
	await writeFile(schema, JSON.stringify(manifest));
	const source = `import {defineAdmin} from '@riducms/plugin/admin';
import {defineAdminPlugin,definePluginField} from '@riducms/plugin/authoring/v1';
import Field from './field.svelte';
const field = definePluginField({component:Field,decodeValue:(v)=>v,decodeConfig:(raw)=>{if(typeof raw.limit!=='number') throw new Error('limit must be numeric'); return raw;}});
const shapes=defineAdminPlugin({key:'shapes',pairingVersion:3,fields:{color:field,outline:field}});
export default defineAdmin({plugins:[shapes]});`;
	await writeFile(resolve(root, "src/admin.config.ts"), source);
	await checkAdminRegistrations({ root, schema });
	await writeFile(
		schema,
		JSON.stringify({
			...manifest,
			collections: [
				{
					slug: "posts",
					fields: [
						{
							type: "plugin",
							path: "accent",
							admin: {},
							plugin: { key: "color", config: { limit: "five" } },
						},
					],
				},
			],
		})
	);
	await expect(checkAdminRegistrations({ root, schema })).rejects.toThrow("limit must be numeric");

	expect((await readdir(root)).includes("out")).toBe(false);
}, 30_000);
