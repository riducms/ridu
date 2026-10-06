// Block-heavy Ridu/Payload benchmark runner. See README.md in this directory for the method,
// commands and every environment variable. Raw results go to .ridu/performance/blocks/.
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
	cpSync,
	existsSync,
	mkdirSync,
	readdirSync,
	readFileSync,
	rmSync,
	statSync,
	writeFileSync,
} from "node:fs";
import { join } from "node:path";

import {
	buildSpec,
	canonicalLayout,
	countInstances,
	generateLayout,
	resolveMedia,
	scenarioNames,
	specStatistics,
	type FieldSpec,
	type ScenarioName,
	type Spec,
} from "./spec.ts";

type FrameworkName = "ridu" | "payload";
type DatabaseName = "postgres" | "mongodb";
// `hooks` is `references` with executable behavior on every leaf block's first text field. It
// measures Ridu's executable field bindings and runs for Ridu only: Payload is not rerun.
type Variant = "references" | "inline" | "hooks";
const riduOnlyVariants: ReadonlySet<Variant> = new Set(["hooks"]);
const variantFrameworks = (variant: Variant, frameworks: FrameworkName[]) =>
	riduOnlyVariants.has(variant)
		? frameworks.filter((framework) => framework === "ridu")
		: frameworks;
type LoadName = "find" | "list-select" | "create-draft" | "update-draft" | "hot-target";
const loadNames: LoadName[] = ["find", "list-select", "create-draft", "update-draft", "hot-target"];
// `hot-target` edits the documents every page save references while those saves run. It measures
// Ridu's store locking, runs for Ridu only (Payload is not run for it) and only when selected.
const riduOnlyLoads: ReadonlySet<LoadName> = new Set(["hot-target"]);
const defaultLoads = loadNames.filter((name) => name !== "hot-target");
type Sample = { status: number; bytes: number; elapsedMs: number; validated: boolean };
type LoadResult = {
	name: string;
	/**
	 * Set when a request failed; the load stops scheduling requests at the first failure. The
	 * hot-target loads instead count failed requests in `failures` and `timeouts` and set this only
	 * when the harness itself failed, such as on a content mismatch.
	 */
	error?: string;
	/** Hot-target loads: requests answered with an unexpected status, by status in `statuses`. */
	failures?: number;
	/** Hot-target loads: requests that exceeded `BLOCKS_REQUEST_TIMEOUT_MS`. */
	timeouts?: number;
	/** Hot-target loads: the first failed or timed-out request. */
	firstFailure?: string;
	/** Set when the same load already failed in an earlier trial of this combination. */
	skipped?: string;
	requests: number;
	concurrency: number;
	durationMs: number;
	requestsPerSecond: number;
	latencyMs: { p50: number; p95: number; p99: number; max: number };
	responseBytes: { average: number };
	statuses: Record<string, number>;
	validatedResponses: number;
	peakTreeRSSMiB: number;
};

const repository = join(import.meta.dir, "../../..");
const harness = import.meta.dir;
const payloadDirectory = join(harness, "payload");
const standaloneDirectory = join(payloadDirectory, ".next/standalone");
const outputRoot = join(repository, ".ridu/performance/blocks");
const startedAt = new Date();
const stamp = startedAt.toISOString().replaceAll(":", "-");
const runDirectory = join(outputRoot, "runs", stamp);
const binaries = join(outputRoot, "bin");

const list = <T extends string>(name: string, fallback: T[]): T[] =>
	(Bun.env[name]
		?.split(",")
		.map((value) => value.trim())
		.filter(Boolean) as T[]) ?? fallback;
const scenarios = list<ScenarioName>("BLOCKS_SCENARIOS", [...scenarioNames]);
const variants = list<Variant>("BLOCKS_VARIANTS", ["references", "inline", "hooks"]);
const databases = list<DatabaseName>("BLOCKS_DATABASES", ["postgres", "mongodb"]);
const frameworkNames = list<FrameworkName>("BLOCKS_FRAMEWORKS", ["ridu", "payload"]);
const trials = positiveInteger("BLOCKS_TRIALS", 3);
const seedDocuments = positiveInteger("BLOCKS_SEED_DOCUMENTS", 20);
const findRequests = positiveInteger("BLOCKS_FIND_REQUESTS", 400);
const listRequests = positiveInteger("BLOCKS_LIST_REQUESTS", 100);
const createRequests = positiveInteger("BLOCKS_CREATE_REQUESTS", 100);
const readConcurrency = positiveInteger("BLOCKS_READ_CONCURRENCY", 8);
const mutationConcurrency = positiveInteger("BLOCKS_MUTATION_CONCURRENCY", 4);
const requestTimeoutMs = positiveInteger("BLOCKS_REQUEST_TIMEOUT_MS", 120_000);
const startupTimeoutMs = positiveInteger("BLOCKS_STARTUP_TIMEOUT_MS", 300_000);
const validateEvery = positiveInteger("BLOCKS_VALIDATE_EVERY", 5);
const loadSelection = list<LoadName>("BLOCKS_LOADS", defaultLoads);
for (const name of loadSelection)
	if (!loadNames.includes(name)) throw new Error(`BLOCKS_LOADS names unknown load ${name}`);
if (loadSelection.includes("update-draft") && !loadSelection.includes("create-draft"))
	throw new Error("BLOCKS_LOADS update-draft updates the create-draft documents");
const hotTargetSeconds = positiveInteger("BLOCKS_HOT_TARGET_SECONDS", 20);
const hotTargetPauseMs = nonNegativeInteger("BLOCKS_HOT_TARGET_PAUSE_MS", 0);
const hotTargetProcesses = positiveInteger("BLOCKS_HOT_TARGET_PROCESSES", 1);
if (hotTargetProcesses > 2) throw new Error("BLOCKS_HOT_TARGET_PROCESSES must be 1 or 2");
const graphQLScenario = (Bun.env.BLOCKS_GRAPHQL_SCENARIO ?? "A") as ScenarioName | "none";
const skipPayloadBuild = Bun.env.BLOCKS_SKIP_PAYLOAD_BUILD === "true";
const keepDatabases = Bun.env.BLOCKS_KEEP_DATABASES === "true";
// Diagnostic only: CPU-profile every Ridu load in trial 1. Profiling perturbs timings.
const profileLoads = Bun.env.BLOCKS_PROFILE === "true";
const mediaCount = 10;
const password = "ridu-blocks-benchmark-password";
const email = "admin@riducms.test";
const secret = "ridu-blocks-benchmark-secret";
const riduPort = 18191;
const payloadPort = 3091;
const postgresContainer = "ridu-blocks-bench-postgres";
const mongoProject = "ridu-blocks-bench";
const mongoCompose = join(repository, "adapters/mongodb/testdata/compose.yaml");
const listLimit = 10;
// Ridu executable migration history digests by scenario-variant-database.
const historyDigests = new Map<string, string>();
// Loads that failed once are not repeated in later trials of the same combination.
const failedLoads = new Map<string, string>();

mkdirSync(runDirectory, { recursive: true });
mkdirSync(binaries, { recursive: true });
mkdirSync(join(outputRoot, "specs"), { recursive: true });

const report: Record<string, unknown> = {
	version: 1,
	startedAt: startedAt.toISOString(),
	configuration: {
		scenarios,
		variants,
		riduOnlyVariants: variants.filter((variant) => riduOnlyVariants.has(variant)),
		databases,
		frameworks: frameworkNames,
		trials,
		seedDocuments,
		findRequests,
		listRequests,
		createRequests,
		updateRequests: createRequests,
		readConcurrency,
		mutationConcurrency,
		loads: loadSelection,
		riduOnlyLoads: loadSelection.filter((name) => riduOnlyLoads.has(name)),
		hotTargetSeconds,
		hotTargetPauseMs,
		hotTargetProcesses,
		listLimit,
		requestTimeoutMs,
		validateEvery,
		mediaCount,
		inheritedRuntimeTuning: inheritedRuntimeTuning(),
	},
};
const results: Array<Record<string, unknown>> = [];
report.trials = results;

// ---------------------------------------------------------------------------------------------
// Databases: disposable containers owned by this run.

const postgresPort = startPostgres();
const mongoPort = databases.includes("mongodb") ? startMongo() : 0;
const databaseURL = (database: DatabaseName, name: string) =>
	database === "postgres"
		? `postgres://bench:bench@127.0.0.1:${postgresPort}/${name}?sslmode=disable`
		: `mongodb://127.0.0.1:${mongoPort}/${name}?directConnection=true&replicaSet=ridu-rs0`;

try {
	report.environment = environment();
	report.specs = writeSpecs();
	report.builds = buildArtifacts();
	report.probes = runProbes();
	report.generation = runGeneration();
	for (const scenario of scenarios) {
		for (const variant of variants) {
			for (const database of databases) {
				const frameworks = variantFrameworks(variant, frameworkNames);
				for (let trial = 1; trial <= trials; trial += 1) {
					const order = trial % 2 === 1 ? frameworks : [...frameworks].reverse();
					for (const framework of order) {
						results.push(
							await runTrial({ framework, scenario, variant, database, trial, full: true })
						);
						await persist();
					}
				}
			}
		}
	}
	if (graphQLScenario !== "none") report.graphql = await runGraphQLMatrix(graphQLScenario);
	report.finishedAt = new Date().toISOString();
	await persist();
	console.log(`\nRaw results: ${join(runDirectory, "results.json")}`);
} finally {
	if (!keepDatabases) stopDatabases();
}
// Abandoned keep-alive sockets from failed loads can otherwise keep Bun's event loop alive.
process.exit(0);

async function persist(): Promise<void> {
	const text = `${JSON.stringify(report, null, 2)}\n`;
	await Bun.write(join(runDirectory, "results.json"), text);
	await Bun.write(join(outputRoot, "latest.json"), text);
}

// ---------------------------------------------------------------------------------------------
// Preparation: specs, builds, probes and schema-dependent generation.

function specPath(scenario: ScenarioName, variant: Variant): string {
	return join(outputRoot, "specs", `${scenario}-${variant}.json`);
}

function writeSpecs(): Record<string, unknown> {
	const summary: Record<string, unknown> = {};
	for (const scenario of new Set([
		...scenarios,
		...(graphQLScenario === "none" ? [] : [graphQLScenario]),
	])) {
		for (const variant of ["references", "inline", "hooks"] as Variant[]) {
			const spec = buildSpec(scenario, variant !== "inline", variant === "hooks");
			writeFileSync(specPath(scenario, variant), `${JSON.stringify(spec, null, "\t")}\n`);
			if (variant === "references") {
				const statistics = specStatistics(spec);
				const layout = generateLayout(spec, 1, mediaCount);
				summary[scenario] = {
					description: spec.description,
					...statistics,
					perResource: undefined,
					workloadCollection: spec.workload.collection,
					workloadTopLevelBlocks: spec.workload.topLevelBlocks,
					workloadInstances: countInstances(layout),
					workloadJSONBytes: JSON.stringify(layout).length,
				};
			}
		}
	}
	return summary;
}

function buildArtifacts(): Record<string, unknown> {
	const builds: Record<string, unknown> = {};
	const ridu = (name: string, tags: string[]) => {
		const output = join(binaries, name);
		const command = [
			"go",
			"build",
			...(tags.length ? ["-tags", tags.join(",")] : []),
			"-trimpath",
			"-ldflags=-s -w",
			"-o",
			output,
			"./tests/performance/blocks/ridu",
		];
		const started = performance.now();
		run(command, repository);
		builds[name] = {
			command: command.join(" "),
			wallMs: round(performance.now() - started),
			bytes: statSync(output).size,
			sha256: sha256(output),
		};
	};
	ridu("ridu-postgres", []);
	ridu("ridu-mongodb", ["blocksmongodb"]);
	ridu("ridu-postgres-graphql", ["blocksgraphql"]);
	ridu("ridu-mongodb-graphql", ["blocksmongodb", "blocksgraphql"]);
	if (!frameworkNames.includes("payload")) return builds;
	if (!skipPayloadBuild) {
		rmSync(join(payloadDirectory, ".next"), { recursive: true, force: true });
		const started = performance.now();
		run(["bun", "run", "build"], payloadDirectory, {
			NODE_ENV: "production",
			PAYLOAD_SECRET: secret,
		});
		builds.payload = {
			command: "rm -rf .next && next build (standalone, Turbopack)",
			wallMs: round(performance.now() - started),
		};
	}
	cpSync(join(payloadDirectory, ".next/static"), join(standaloneDirectory, ".next/static"), {
		recursive: true,
		force: true,
	});
	builds.payloadStandaloneKiB = Number(output("du", ["-sk", standaloneDirectory]).split(/\s+/)[0]);
	builds.note =
		"Both servers read the scenario spec at startup, so one build serves every scenario; schema-dependent work is under generation.";
	return builds;
}

function runProbes(): Record<string, unknown> {
	const probes: Record<string, unknown> = {};
	for (const scenario of scenarios) {
		for (const variant of variants) {
			const key = `${scenario}-${variant}`;
			const directory = join(runDirectory, "probe", key);
			const ridu = timed(() =>
				output(
					"/usr/bin/time",
					["-l", join(binaries, "ridu-postgres"), "probe", directory],
					repository,
					{ RIDU_BLOCKS_SPEC: specPath(scenario, variant) },
					true
				)
			);
			if (!frameworkNames.includes("payload")) {
				probes[key] = { ridu: parseProbe(ridu.value) };
				console.log(`probe ${key}: ${JSON.stringify(probes[key]).slice(0, 400)}`);
				continue;
			}
			if (riduOnlyVariants.has(variant)) {
				probes[key] = {
					ridu: parseProbe(ridu.value),
					payloadMongoose: { skipped: `the ${variant} variant runs for Ridu only` },
				};
				console.log(`probe ${key} (Ridu only): ${JSON.stringify(probes[key]).slice(0, 400)}`);
				continue;
			}
			const payload = timed(() =>
				output(
					"/usr/bin/time",
					["-l", "node", "--expose-gc", join(payloadDirectory, "src/probe.ts")],
					payloadDirectory,
					{
						BLOCKS_SPEC: specPath(scenario, variant),
						BLOCKS_SCENARIO: key,
						PAYLOAD_DATABASE_URL: "mongodb://127.0.0.1:1/probe",
						NODE_ENV: "production",
					},
					true
				)
			);
			probes[key] = { ridu: parseProbe(ridu.value), payloadMongoose: parseProbe(payload.value) };
			console.log(`probe ${key}: ${JSON.stringify(probes[key]).slice(0, 400)}`);
		}
	}
	return probes;
}

function parseProbe(text: string): Record<string, unknown> {
	const line = text.split("\n").find((candidate) => candidate.startsWith("{"));
	const peak = /(\d+)\s+maximum resident set size/.exec(text);
	return {
		...(line ? JSON.parse(line) : { error: text.slice(-800) }),
		peakRSSMiB: peak ? round(Number(peak[1]) / 1048576) : null,
	};
}

// The GraphQL plugin is part of the manifest, so a GraphQL build has its own migration history.
function migrationDirectory(
	framework: FrameworkName,
	scenario: ScenarioName,
	variant: Variant,
	database: DatabaseName,
	graphql = false
): string {
	return join(
		runDirectory,
		"migrations",
		`${framework}-${scenario}-${variant}-${database}${graphql ? "-graphql" : ""}`
	);
}

function riduBinary(database: DatabaseName, graphql = false): string {
	return join(binaries, `ridu-${database}${graphql ? "-graphql" : ""}`);
}

function runGeneration(): Record<string, unknown> {
	const generation: Record<string, unknown> = {};
	for (const scenario of new Set([
		...scenarios,
		...(graphQLScenario === "none" ? [] : [graphQLScenario]),
	])) {
		for (const variant of variants) {
			const entry: Record<string, unknown> = {};
			const frameworks = variantFrameworks(variant, frameworkNames);
			for (const database of databases) {
				const graphQLBuilds =
					scenario === graphQLScenario && database === "postgres" && variant === "references"
						? [false, true]
						: [false];
				for (const graphql of frameworks.includes("ridu") ? graphQLBuilds : []) {
					const suffix = graphql ? "-graphql" : "";
					guarded(entry, `ridu-${database}${suffix}-migrations`, () => {
						const directory = migrationDirectory("ridu", scenario, variant, database, graphql);
						rmSync(directory, { recursive: true, force: true });
						const result = timed(() =>
							runCaptured([riduBinary(database, graphql), "migrations", directory], repository, {
								RIDU_BLOCKS_SPEC: specPath(scenario, variant),
							})
						);
						historyDigests.set(`${scenario}-${variant}-${database}${suffix}`, result.value.trim());
						return { wallMs: result.ms, bytes: directoryBytes(directory) };
					});
				}
				if (database === "postgres" && frameworks.includes("payload")) {
					guarded(entry, "payload-postgres-migrate-create", () => {
						const directory = migrationDirectory("payload", scenario, variant, database);
						rmSync(directory, { recursive: true, force: true });
						const name = `blocks_generate_${scenario.replace("-", "_").toLowerCase()}_${variant}`;
						recreateDatabase("postgres", name);
						try {
							const result = timed(() =>
								runCaptured(
									[join(payloadDirectory, "node_modules/.bin/payload"), "migrate:create", "blocks"],
									payloadDirectory,
									payloadCLIEnvironment(scenario, variant, databaseURL("postgres", name), directory)
								)
							);
							return { wallMs: result.ms, bytes: directoryBytes(directory) };
						} finally {
							dropDatabase("postgres", name);
						}
					});
				}
			}
			if (frameworks.includes("payload")) {
				guarded(entry, "payload-generate-types", () => {
					const typesFile = join(runDirectory, "types", `payload-${scenario}-${variant}.ts`);
					mkdirSync(join(runDirectory, "types"), { recursive: true });
					const result = timed(() =>
						runCaptured(
							[join(payloadDirectory, "node_modules/.bin/payload"), "generate:types"],
							payloadDirectory,
							{
								...payloadCLIEnvironment(scenario, variant, "postgres://127.0.0.1:1/types", ""),
								BLOCKS_TYPES_OUTPUT: typesFile,
							}
						)
					);
					return {
						wallMs: result.ms,
						bytes: existsSync(typesFile) ? statSync(typesFile).size : null,
					};
				});
			}
			generation[`${scenario}-${variant}`] = entry;
			console.log(`generation ${scenario}-${variant}: ${JSON.stringify(entry)}`);
		}
	}
	return generation;
}

function guarded(
	entry: Record<string, unknown>,
	key: string,
	work: () => Record<string, unknown>
): void {
	try {
		entry[key] = work();
	} catch (error) {
		entry[key] = { error: String(error).slice(0, 3_000) };
		console.log(`${key} failed: ${String(error).slice(0, 600)}`);
	}
}

function payloadCLIEnvironment(
	scenario: ScenarioName,
	variant: Variant,
	url: string,
	migrations: string
): Record<string, string> {
	return {
		NODE_PATH: join(payloadDirectory, "node_modules"),
		PAYLOAD_CONFIG_PATH: "src/payload.config.ts",
		PAYLOAD_DATABASE_URL: url,
		PAYLOAD_SECRET: secret,
		BLOCKS_SPEC: specPath(scenario, variant),
		...(migrations ? { BLOCKS_MIGRATION_DIR: migrations } : {}),
	};
}

// ---------------------------------------------------------------------------------------------
// One server lifetime.

type TrialOptions = {
	framework: FrameworkName;
	scenario: ScenarioName;
	variant: Variant;
	database: DatabaseName;
	trial: number;
	/** false: startup, memory and admin only, without seeding every document or the timed loads. */
	full: boolean;
	graphql?: boolean;
};

async function runTrial(options: TrialOptions): Promise<Record<string, unknown>> {
	const { framework, scenario, variant, database, trial } = options;
	const label = `${framework} ${scenario}/${variant}/${database}${options.graphql === undefined ? "" : ` graphql=${options.graphql}`} trial ${trial}`;
	console.log(`\n=== ${label} ===`);
	const spec = JSON.parse(readFileSync(specPath(scenario, variant), "utf8")) as Spec;
	const api = endpoints(framework, spec);
	const databaseName = `${framework}_blocks`;
	const url = databaseURL(database, databaseName);
	const result: Record<string, unknown> = {
		framework,
		scenario,
		variant,
		database,
		trial,
		graphql: options.graphql ?? framework === "payload",
	};
	assertPortFree(framework === "ridu" ? riduPort : payloadPort);
	try {
		recreateDatabase(database, databaseName);
		const migrationStarted = performance.now();
		await migrate(framework, scenario, variant, database, url, options.graphql === true);
		result.migrationMs = round(performance.now() - migrationStarted);
	} catch (error) {
		result.migrationError = String(error).slice(0, 3_000);
		console.log(`migration failed: ${String(result.migrationError).slice(0, 600)}`);
		return result;
	}
	const command =
		framework === "ridu"
			? [join(binaries, `ridu-${database}${options.graphql ? "-graphql" : ""}`)]
			: ["node", join(standaloneDirectory, "server.js")];
	const environmentVariables: Record<string, string> =
		framework === "ridu"
			? {
					RIDU_BLOCKS_SPEC: specPath(scenario, variant),
					RIDU_BLOCKS_DATABASE_URL: url,
					RIDU_BLOCKS_ADDRESS: `127.0.0.1:${riduPort}`,
					RIDU_BLOCKS_HISTORY_DIGEST:
						historyDigests.get(
							`${scenario}-${variant}-${database}${options.graphql ? "-graphql" : ""}`
						) ?? "",
				}
			: {
					NODE_ENV: "production",
					HOSTNAME: "127.0.0.1",
					PORT: String(payloadPort),
					BLOCKS_SPEC: specPath(scenario, variant),
					PAYLOAD_DATABASE_URL: url,
					PAYLOAD_SECRET: secret,
					...(options.graphql === false ? { BLOCKS_GRAPHQL: "false" } : {}),
				};
	const logPath = join(runDirectory, "logs", `${label.replaceAll(/[ /=]/g, "_")}.log`);
	mkdirSync(join(runDirectory, "logs"), { recursive: true });
	const launchedAt = performance.now();
	const server = Bun.spawn(command, {
		cwd: framework === "ridu" ? repository : standaloneDirectory,
		env: { ...Bun.env, ...environmentVariables },
		stdout: Bun.file(logPath),
		stderr: Bun.file(`${logPath}.stderr`),
	});
	let peakDuringStartup = 0;
	const startupSampler = sampleUntil(
		server.pid,
		(value) => (peakDuringStartup = Math.max(peakDuringStartup, value))
	);
	try {
		try {
			await waitUntilReady(api.base + api.ready, server);
		} catch (error) {
			startupSampler.stop();
			result.startupError = String(error).slice(0, 2_000);
			result.startupPeakTreeRSSMiB = round(peakDuringStartup);
			result.stderrTail = tail(`${logPath}.stderr`);
			console.log(`startup failed: ${result.startupError}`);
			return result;
		}
		result.startupMs = round(performance.now() - launchedAt);
		startupSampler.stop();
		result.startupPeakTreeRSSMiB = round(Math.max(peakDuringStartup, await treeRSSMiB(server.pid)));
		await bootstrapUser(framework, api.base);
		const cookie = await login(framework, api.base);
		await delay(2_000);
		result.idleTreeRSSMiB = round(await stableRSS(server.pid));
		const loginAdmin = await timedFetch(`${api.base}/admin/login`, {}, [200]);
		result.adminLogin = { ms: round(loginAdmin.elapsedMs), bytes: loginAdmin.bytes };
		await delay(1_000);
		result.afterAdminLoginTreeRSSMiB = round(await stableRSS(server.pid));
		result.idleHeap = await memory(api);
		if (framework === "ridu" && trial === 1 && variant === "references")
			await saveHeapProfile(
				api.base,
				`${scenario}-${database}${options.graphql ? "-graphql" : ""}-idle`
			);

		if (options.graphql !== undefined) {
			const graphQLStarted = performance.now();
			const response = await fetch(`${api.base}${api.graphql}`, {
				method: "POST",
				headers: { "Content-Type": "application/json", Cookie: cookie },
				body: JSON.stringify({ query: api.graphQLQuery }),
				signal: AbortSignal.timeout(requestTimeoutMs),
			});
			const text = await response.text();
			result.firstGraphQL = {
				status: response.status,
				ms: round(performance.now() - graphQLStarted),
				bytes: text.length,
				error:
					response.status === 200 && !text.includes('"errors"') ? undefined : text.slice(0, 300),
			};
			await delay(1_000);
			result.afterGraphQLTreeRSSMiB = round(await stableRSS(server.pid));
			result.afterGraphQLHeap = await memory(api);
			return result;
		}

		const mediaIDs: Array<string | number> = [];
		for (let index = 0; index < mediaCount; index += 1) {
			const created = await createDocument(
				api,
				cookie,
				"media",
				{ title: `Media ${index}`, alt: `Alt ${index}` },
				true
			);
			mediaIDs.push(created.doc.id as string | number);
		}
		const seeded: Array<{ id: string; expected: unknown }> = [];
		const expectedByTitle = new Map<string, unknown>();
		const seedStarted = performance.now();
		for (let index = 0; index < (options.full ? seedDocuments : 3); index += 1) {
			const layout = resolveMedia(generateLayout(spec, index + 1, mediaCount) as never, mediaIDs);
			const title = `Seed ${index}`;
			const created = await createDocument(
				api,
				cookie,
				spec.workload.collection,
				{ title, [spec.workload.layoutField]: layout },
				true
			);
			const expected = canonicalLayout(spec, layout);
			assertEquivalent(spec, created.doc, expected, `${label} seed ${index}`);
			seeded.push({ id: String(created.doc.id), expected });
			expectedByTitle.set(title, expected);
		}
		result.seed = {
			documents: seeded.length,
			ms: round(performance.now() - seedStarted),
			instancesPerDocument: countInstances(generateLayout(spec, 1, mediaCount)),
		};
		// After seeding, so Payload's MongoDB collections created at runtime are included.
		result.physicalSchema = physicalSchema(database, databaseName);
		await delay(2_000);
		result.afterDocumentsTreeRSSMiB = round(await stableRSS(server.pid));
		const editAdmin = await timedFetch(
			`${api.base}/admin/collections/${spec.workload.collection}/${seeded[0]!.id}`,
			{ headers: { Cookie: cookie } },
			[200]
		);
		result.adminEdit = { coldMs: round(editAdmin.elapsedMs), bytes: editAdmin.bytes };
		const warmEdits: number[] = [];
		for (let index = 0; index < 5; index += 1) {
			warmEdits.push(
				(
					await timedFetch(
						`${api.base}/admin/collections/${spec.workload.collection}/${seeded[index % seeded.length]!.id}`,
						{ headers: { Cookie: cookie } },
						[200]
					)
				).elapsedMs
			);
		}
		(result.adminEdit as Record<string, unknown>).warmMedianMs = round(
			warmEdits.sort((a, b) => a - b)[2]!
		);
		await delay(1_000);
		result.afterAdminEditTreeRSSMiB = round(await stableRSS(server.pid));
		if (!options.full) {
			result.finalHeap = await memory(api);
			return result;
		}

		const loads: LoadResult[] = [];
		const profiled = profileLoads && framework === "ridu" && trial === 1;
		const profile = async (
			name: LoadName,
			load: () => Promise<LoadResult | LoadResult[]>
		): Promise<LoadResult[]> => {
			if (!loadSelection.includes(name)) return [];
			if (riduOnlyLoads.has(name) && framework !== "ridu")
				return [failedLoad(name, { skipped: `the ${name} load runs for Ridu only` })];
			const key = `${framework}|${scenario}|${variant}|${database}|${name}`;
			const earlier = failedLoads.get(key);
			if (earlier) return [failedLoad(name, { skipped: earlier })];
			if (profiled) await fetch(`${api.base}/__bench/cpu/start`, { method: "POST" });
			try {
				const value = await load();
				return Array.isArray(value) ? value : [value];
			} catch (error) {
				const message = `trial ${trial}: ${String(error).slice(0, 1_500)}`;
				failedLoads.set(key, message);
				console.log(`${name} failed: ${message.slice(0, 400)}`);
				return [failedLoad(name, { error: message })];
			} finally {
				if (profiled) {
					const response = await fetch(`${api.base}/__bench/cpu/stop`, { method: "POST" });
					mkdirSync(join(runDirectory, "pprof"), { recursive: true });
					await Bun.write(
						join(runDirectory, "pprof", `cpu-${scenario}-${variant}-${database}-${name}.pprof`),
						await response.arrayBuffer()
					);
				}
			}
		};
		loads.push(
			...(await profile("find", () =>
				runLoad("find", findRequests, readConcurrency, server.pid, async (index) => {
					const target = seeded[index % seeded.length]!;
					const response = await timedFetch(`${api.base}${api.find(target.id)}`, {}, [200]);
					const validated = shouldValidate(index);
					if (validated)
						assertEquivalent(
							spec,
							unwrap(response.json()),
							target.expected,
							`${label} find ${index}`
						);
					return { ...response, validated };
				})
			))
		);
		loads.push(
			...(await profile("list-select", () =>
				runLoad("list-select", listRequests, readConcurrency, server.pid, async (index) => {
					const response = await timedFetch(`${api.base}${api.list}`, {}, [200]);
					const validated = shouldValidate(index);
					if (validated) {
						const body = response.json() as { docs: Array<Record<string, unknown>> };
						if (body.docs.length !== Math.min(listLimit, seeded.length))
							throw new Error(`${label} list returned ${body.docs.length} documents`);
						for (const doc of body.docs)
							assertEquivalent(
								spec,
								doc,
								expectedByTitle.get(String(doc.title)),
								`${label} list ${index}`
							);
					}
					return { ...response, validated };
				})
			))
		);
		const drafts: Array<{ id: string; layout: unknown }> = [];
		loads.push(
			...(await profile("create-draft", () =>
				runLoad("create-draft", createRequests, mutationConcurrency, server.pid, async (index) => {
					const layout = resolveMedia(
						generateLayout(spec, 1_000 + index, mediaCount) as never,
						mediaIDs
					);
					const started = performance.now();
					const created = await createDocument(
						api,
						cookie,
						spec.workload.collection,
						{ title: `Draft ${trial}-${index}`, [spec.workload.layoutField]: layout },
						false
					);
					const elapsedMs = performance.now() - started;
					const validated = shouldValidate(index);
					if (validated)
						assertEquivalent(
							spec,
							created.doc,
							canonicalLayout(spec, layout),
							`${label} create ${index}`
						);
					drafts[index] = {
						id: String(created.doc.id),
						layout: created.doc[spec.workload.layoutField],
					};
					return { status: created.status, bytes: created.bytes, elapsedMs, validated };
				})
			))
		);
		loads.push(
			...(await profile("update-draft", () =>
				runLoad("update-draft", createRequests, mutationConcurrency, server.pid, async (index) => {
					const edited = editLayout(spec, drafts[index]!.layout, `edited ${index}`);
					const response = await timedFetch(
						`${api.base}${api.update(drafts[index]!.id)}`,
						{
							method: "PATCH",
							headers: { "Content-Type": "application/json", Cookie: cookie },
							body: JSON.stringify(
								api.draftBody(
									{ title: `Draft ${trial}-${index} edited`, [spec.workload.layoutField]: edited },
									false
								)
							),
						},
						[200]
					);
					const validated = shouldValidate(index);
					if (validated)
						assertEquivalent(
							spec,
							unwrap(response.json()),
							canonicalLayout(spec, edited),
							`${label} update ${index}`
						);
					return { ...response, validated };
				})
			))
		);
		loads.push(
			...(await profile("hot-target", () =>
				runHotTarget({
					label,
					spec,
					api,
					cookie,
					mediaIDs,
					trial,
					pid: server.pid,
					replica: { command, environment: environmentVariables, logPath: `${logPath}.replica` },
				})
			))
		);
		result.loads = loads;
		await delay(1_000);
		result.finalTreeRSSMiB = round(await stableRSS(server.pid));
		result.finalHeap = await memory(api);
		if (framework === "ridu" && trial === 1 && variant === "references")
			await saveHeapProfile(api.base, `${scenario}-${database}-final`);
		return result;
	} catch (error) {
		result.error = String(error).slice(0, 4_000);
		result.stderrTail = tail(`${logPath}.stderr`);
		console.log(`trial failed: ${String(result.error).slice(0, 600)}`);
		return result;
	} finally {
		startupSampler.stop();
		server.kill("SIGTERM");
		await Promise.race([server.exited, delay(10_000)]);
		if (server.exitCode === null) server.kill("SIGKILL");
		await server.exited;
		// MongoDB keeps executing an abandoned aggregate; stop it before the next trial.
		if (database === "mongodb") killMongoOperations(databaseName);
	}
}

function failedLoad(name: string, reason: { error?: string; skipped?: string }): LoadResult {
	return {
		name,
		...reason,
		requests: 0,
		concurrency: 0,
		durationMs: 0,
		requestsPerSecond: 0,
		latencyMs: { p50: 0, p95: 0, p99: 0, max: 0 },
		responseBytes: { average: 0 },
		statuses: {},
		validatedResponses: 0,
		peakTreeRSSMiB: 0,
	};
}

async function runGraphQLMatrix(scenario: ScenarioName): Promise<unknown[]> {
	const rows: unknown[] = [];
	for (let trial = 1; trial <= trials; trial += 1) {
		for (const graphql of trial % 2 === 1 ? [false, true] : [true, false]) {
			for (const framework of frameworkNames) {
				rows.push(
					await runTrial({
						framework,
						scenario,
						variant: "references",
						database: "postgres",
						trial,
						full: false,
						graphql,
					})
				);
				await persist();
			}
		}
	}
	return rows;
}

// ---------------------------------------------------------------------------------------------
// Framework wire differences. Everything else is shared.

function endpoints(framework: FrameworkName, spec: Spec) {
	const collection = spec.workload.collection;
	const layout = spec.workload.layoutField;
	if (framework === "ridu") {
		const base = `http://127.0.0.1:${riduPort}`;
		return {
			framework,
			base,
			// Readiness lists the small media collection on both sides (see README).
			ready: "/api/collections/media?limit=1",
			memory: "/__bench/memory",
			find: (id: string) => `/api/collections/${collection}/${id}`,
			list: `/api/collections/${collection}?limit=${listLimit}&select=${encodeURIComponent(JSON.stringify({ title: true, [layout]: true }))}`,
			create: (slug: string, published: boolean) =>
				`/api/collections/${slug}?draft=${published ? "false" : "true"}`,
			update: (id: string) => `/api/collections/${collection}/${id}?draft=true`,
			updateMedia: (id: string) => `/api/collections/media/${id}`,
			draftBody: (body: Record<string, unknown>, _published: boolean) => body,
			graphql: "/api/graphql",
			graphQLQuery: `{ ${graphQLListName(collection)}(limit: 1) { totalDocs } }`,
		};
	}
	const base = `http://127.0.0.1:${payloadPort}`;
	return {
		framework,
		base,
		ready: "/api/media?limit=1&depth=0",
		memory: "/api/__bench/memory",
		find: (id: string) => `/api/${collection}/${id}?depth=0`,
		list: `/api/${collection}?limit=${listLimit}&depth=0&select[title]=true&select[${layout}]=true`,
		create: (slug: string, published: boolean) =>
			`/api/${slug}?depth=0${published ? "" : "&draft=true"}`,
		update: (id: string) => `/api/${collection}/${id}?draft=true&depth=0`,
		updateMedia: (id: string) => `/api/media/${id}?depth=0`,
		draftBody: (body: Record<string, unknown>, published: boolean) => ({
			...body,
			_status: published ? "published" : "draft",
		}),
		graphql: "/api/graphql",
		graphQLQuery: `{ ${graphQLListName(collection)}(limit: 1) { totalDocs } }`,
	};
}

function graphQLListName(slug: string): string {
	return slug
		.split("-")
		.map((part) => part.charAt(0).toUpperCase() + part.slice(1))
		.join("");
}

async function createDocument(
	api: ReturnType<typeof endpoints>,
	cookie: string,
	slug: string,
	body: Record<string, unknown>,
	published: boolean
): Promise<{ doc: Record<string, unknown>; status: number; bytes: number }> {
	const versioned = slug !== "media";
	const payloadBody =
		api.framework === "payload" && versioned ? api.draftBody(body, published) : body;
	const response = await timedFetch(
		`${api.base}${versioned ? api.create(slug, published) : api.framework === "ridu" ? `/api/collections/${slug}` : `/api/${slug}?depth=0`}`,
		{
			method: "POST",
			headers: { "Content-Type": "application/json", Cookie: cookie },
			body: JSON.stringify(payloadBody),
		},
		[200, 201]
	);
	const envelope = response.json() as { doc?: Record<string, unknown> };
	if (!envelope.doc?.id) throw new Error(`${api.framework} create ${slug} omitted doc.id`);
	return { doc: envelope.doc, status: response.status, bytes: response.bytes };
}

async function bootstrapUser(framework: FrameworkName, base: string): Promise<void> {
	for (let attempt = 1; ; attempt += 1) {
		try {
			await bootstrapUserOnce(framework, base);
			return;
		} catch (error) {
			if (attempt >= 5) throw error;
			console.log(`bootstrap attempt ${attempt} failed; retrying: ${String(error).slice(0, 200)}`);
			await delay(2_000);
		}
	}
}

async function bootstrapUserOnce(framework: FrameworkName, base: string): Promise<void> {
	const response = await fetch(
		framework === "ridu"
			? `${base}/api/auth/users/create-user`
			: `${base}/api/users/first-register`,
		{
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify(
				framework === "ridu" ? { data: { email }, password } : { email, password }
			),
			signal: AbortSignal.timeout(requestTimeoutMs),
		}
	);
	const text = await response.text();
	if (response.status !== 200 && response.status !== 201)
		throw new Error(`${framework} bootstrap returned ${response.status}: ${text.slice(0, 300)}`);
}

async function login(framework: FrameworkName, base: string): Promise<string> {
	const response = await fetch(
		framework === "ridu" ? `${base}/api/auth/users/login` : `${base}/api/users/login`,
		{
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ email, password }),
			signal: AbortSignal.timeout(requestTimeoutMs),
		}
	);
	await response.arrayBuffer();
	const cookie = response.headers.get("set-cookie");
	if (response.status !== 200 || !cookie)
		throw new Error(`${framework} login returned ${response.status}`);
	return cookie.split(";", 1)[0]!;
}

async function memory(api: ReturnType<typeof endpoints>): Promise<unknown> {
	const response = await timedFetch(`${api.base}${api.memory}`, {}, [200]);
	return response.json();
}

async function saveHeapProfile(base: string, name: string): Promise<void> {
	const response = await fetch(`${base}/__bench/heap`, {
		signal: AbortSignal.timeout(requestTimeoutMs),
	});
	mkdirSync(join(runDirectory, "pprof"), { recursive: true });
	await Bun.write(join(runDirectory, "pprof", `${name}.pprof`), await response.arrayBuffer());
}

async function migrate(
	framework: FrameworkName,
	scenario: ScenarioName,
	variant: Variant,
	database: DatabaseName,
	url: string,
	graphql: boolean
): Promise<void> {
	if (framework === "ridu") {
		runCaptured(
			[
				riduBinary(database, graphql),
				"migrate",
				migrationDirectory("ridu", scenario, variant, database, graphql),
			],
			repository,
			{
				RIDU_BLOCKS_SPEC: specPath(scenario, variant),
				RIDU_BLOCKS_DATABASE_URL: url,
			}
		);
		return;
	}
	if (database === "postgres") {
		runCaptured(
			[join(payloadDirectory, "node_modules/.bin/payload"), "migrate"],
			payloadDirectory,
			payloadCLIEnvironment(
				scenario,
				variant,
				url,
				migrationDirectory("payload", scenario, variant, database)
			)
		);
	}
	if (database === "mongodb") await preparePayloadMongo(scenario, variant, url);
}

// Payload's MongoDB adapter has no migrate step: Mongoose creates collections and builds every
// model's indexes in the background after the first connection, and writes made meanwhile can
// fail with LockTimeout. Like Ridu's separate migrate step, one preparatory server start lets
// the build finish, so the measured server starts against an indexed database.
async function preparePayloadMongo(
	scenario: ScenarioName,
	variant: Variant,
	url: string
): Promise<void> {
	const server = Bun.spawn(["node", join(standaloneDirectory, "server.js")], {
		cwd: standaloneDirectory,
		env: {
			...Bun.env,
			NODE_ENV: "production",
			HOSTNAME: "127.0.0.1",
			PORT: String(payloadPort),
			BLOCKS_SPEC: specPath(scenario, variant),
			PAYLOAD_DATABASE_URL: url,
			PAYLOAD_SECRET: secret,
		},
		stdout: "ignore",
		stderr: "ignore",
	});
	try {
		await waitUntilReady(`http://127.0.0.1:${payloadPort}/api/media?limit=1&depth=0`, server);
		const name = new URL(url).pathname.slice(1);
		const deadline = Date.now() + startupTimeoutMs;
		for (let quiet = 0; quiet < 3 && Date.now() < deadline;) {
			quiet = activeMongoIndexBuilds(name) === 0 ? quiet + 1 : 0;
			await delay(1_000);
		}
	} finally {
		server.kill("SIGTERM");
		await Promise.race([server.exited, delay(10_000)]);
		if (server.exitCode === null) server.kill("SIGKILL");
		await server.exited;
	}
}

function activeMongoIndexBuilds(name: string): number {
	const script = `print(db.getSiblingDB("admin").aggregate([{ $currentOp: { allUsers: true } }, { $match: { ns: { $regex: "^" + ${JSON.stringify(name)} + "\\\\." }, $or: [{ "command.createIndexes": { $exists: true } }, { msg: /Index Build/ }] } }]).toArray().length)`;
	return Number(
		output("docker", [
			"compose",
			"-p",
			mongoProject,
			"-f",
			mongoCompose,
			"exec",
			"-T",
			"mongodb",
			"mongosh",
			"--quiet",
			"--eval",
			script,
		])
	);
}

// ---------------------------------------------------------------------------------------------
// Content equivalence.

function unwrap(body: unknown): Record<string, unknown> {
	const record = body as Record<string, unknown>;
	return (record.doc as Record<string, unknown>) ?? record;
}

function shouldValidate(index: number): boolean {
	return index < 10 || index % validateEvery === 0;
}

function assertEquivalent(
	spec: Spec,
	doc: Record<string, unknown>,
	expected: unknown,
	label: string
): void {
	const actual = canonicalLayout(spec, doc[spec.workload.layoutField]);
	const left = JSON.stringify(actual);
	const right = JSON.stringify(expected);
	if (left !== right) {
		let position = 0;
		while (position < left.length && left[position] === right[position]) position += 1;
		throw new Error(
			`${label}: returned layout differs at ${position}: got ${left.slice(Math.max(0, position - 120), position + 200)} expected ${right.slice(Math.max(0, position - 120), position + 200)}`
		);
	}
}

/**
 * Appends a suffix to the first direct text/textarea field of every block instance at any depth,
 * keeping each block's identity (`id` or `_key`) so the update edits existing rows.
 */
function editLayout(spec: Spec, layout: unknown, suffix: string): unknown {
	const copy = structuredClone(layout);
	const blocks = new Map(spec.blocks.map((block) => [block.slug, block]));
	const visit = (items: unknown) => {
		if (!Array.isArray(items)) return;
		for (const item of items as Array<Record<string, unknown>>) {
			const definition = blocks.get(String(item.blockType));
			if (!definition) continue;
			const field = definition.fields.find(
				(candidate: FieldSpec) => candidate.type === "text" || candidate.type === "textarea"
			);
			if (field) item[field.name] = `${String(item[field.name] ?? "")} ${suffix}`;
			for (const candidate of definition.fields)
				if (candidate.type === "blocks") visit(item[candidate.name]);
		}
	};
	visit(copy);
	return copy;
}

// ---------------------------------------------------------------------------------------------
// Hot-target contention: edits of documents that concurrent saves reference.

type HotTargetOptions = {
	label: string;
	spec: Spec;
	api: ReturnType<typeof endpoints>;
	cookie: string;
	mediaIDs: Array<string | number>;
	trial: number;
	pid: number;
	/** How to start a second Ridu process on the same database for BLOCKS_HOT_TARGET_PROCESSES=2. */
	replica: { command: string[]; environment: Record<string, string>; logPath: string };
};

/**
 * For `BLOCKS_HOT_TARGET_SECONDS`, `mutationConcurrency` workers each create a draft page and then
 * update it, again and again, while one separate client edits the media documents those pages
 * reference, round robin and `BLOCKS_HOT_TARGET_PAUSE_MS` apart. Every relationship of
 * scenarios B and C targets media, so each save holds a reference lock on the media it uses and
 * each edit needs the exclusive lock on one of them. Scenario A's layouts have no relationships,
 * so its edits are uncontended. Neither side stops at a failed request: failures and timeouts are
 * part of the result. A request started before the window closes is waited for.
 *
 * With BLOCKS_HOT_TARGET_PROCESSES=2, a second Ridu process serves the same database: the save
 * workers alternate between the two processes and the editor uses the first. Peak RSS is still
 * the first process's.
 */
async function runHotTarget(options: HotTargetOptions): Promise<LoadResult[]> {
	if (hotTargetProcesses === 1)
		return runHotTargetOn(options, [options.api.base], [options.cookie]);
	const replicaPort = riduPort + 1;
	assertPortFree(replicaPort);
	const replicaBase = `http://127.0.0.1:${replicaPort}`;
	const replica = Bun.spawn(options.replica.command, {
		cwd: repository,
		env: {
			...Bun.env,
			...options.replica.environment,
			RIDU_BLOCKS_ADDRESS: `127.0.0.1:${replicaPort}`,
		},
		stdout: Bun.file(options.replica.logPath),
		stderr: Bun.file(`${options.replica.logPath}.stderr`),
	});
	try {
		await waitUntilReady(`${replicaBase}${options.api.ready}`, replica);
		const replicaCookie = await login("ridu", replicaBase);
		return await runHotTargetOn(
			options,
			[options.api.base, replicaBase],
			[options.cookie, replicaCookie]
		);
	} finally {
		replica.kill("SIGTERM");
		await Promise.race([replica.exited, delay(10_000)]);
		if (replica.exitCode === null) replica.kill("SIGKILL");
		await replica.exited;
	}
}

/** Runs the hot-target load; save worker `n` uses `bases[n % bases.length]`, the editor the first. */
async function runHotTargetOn(
	options: HotTargetOptions,
	bases: string[],
	cookies: string[]
): Promise<LoadResult[]> {
	const { label, spec, api, mediaIDs, trial, pid } = options;
	console.log(
		`hot-target: ${hotTargetSeconds} s of page saves at concurrency ${mutationConcurrency} on ${bases.length} process(es) while one client edits ${mediaIDs.length} media documents`
	);
	const saves = new HotTargetRecorder("hot-target-save", mutationConcurrency);
	const edits = new HotTargetRecorder("hot-target-edit", 1);
	let peak = await treeRSSMiB(pid);
	const sampler = sampleUntil(pid, (value) => (peak = Math.max(peak, value)));
	const started = performance.now();
	const deadline = started + hotTargetSeconds * 1_000;
	const headersFor = (process: number) => ({
		"Content-Type": "application/json",
		Cookie: cookies[process]!,
	});
	const collection = spec.workload.collection;
	const layoutField = spec.workload.layoutField;
	let saveIndex = 0;
	const saveWorker = async (worker: number) => {
		const base = bases[worker % bases.length]!;
		const headers = headersFor(worker % bases.length);
		for (let round = 0; performance.now() < deadline; round += 1) {
			const layout = resolveMedia(
				generateLayout(spec, 100_000 + worker * 10_000 + round, mediaCount) as never,
				mediaIDs
			);
			const title = `Hot ${trial}-${worker}-${round}`;
			const createIndex = saveIndex++;
			const created = await hotRequest(`${base}${api.create(collection, false)}`, {
				method: "POST",
				headers,
				body: JSON.stringify(api.draftBody({ title, [layoutField]: layout }, false)),
			});
			if (!saves.record(created, [200, 201])) continue;
			const doc = unwrap(JSON.parse(created.body));
			if (shouldValidate(createIndex)) {
				assertEquivalent(
					spec,
					doc,
					canonicalLayout(spec, layout),
					`${label} hot-target create ${createIndex}`
				);
				saves.validated += 1;
			}
			if (performance.now() >= deadline) return;
			const edited = editLayout(spec, doc[layoutField], `hot ${round}`);
			const updateIndex = saveIndex++;
			const updated = await hotRequest(`${base}${api.update(String(doc.id))}`, {
				method: "PATCH",
				headers,
				body: JSON.stringify(
					api.draftBody({ title: `${title} edited`, [layoutField]: edited }, false)
				),
			});
			if (saves.record(updated, [200]) && shouldValidate(updateIndex)) {
				assertEquivalent(
					spec,
					unwrap(JSON.parse(updated.body)),
					canonicalLayout(spec, edited),
					`${label} hot-target update ${updateIndex}`
				);
				saves.validated += 1;
			}
		}
	};
	const editor = async () => {
		const headers = headersFor(0);
		for (let index = 0; performance.now() < deadline; index += 1) {
			const alt = `Hot edit ${trial}-${index}`;
			const response = await hotRequest(
				`${bases[0]}${api.updateMedia(String(mediaIDs[index % mediaIDs.length]))}`,
				{ method: "PATCH", headers, body: JSON.stringify({ alt }) }
			);
			if (edits.record(response, [200])) {
				const stored = unwrap(JSON.parse(response.body)).alt;
				if (stored !== alt)
					throw new Error(`${label} hot-target edit ${index} stored alt ${String(stored)}`);
				edits.validated += 1;
			}
			if (hotTargetPauseMs > 0) await delay(hotTargetPauseMs);
		}
	};
	const settled = await Promise.allSettled([
		editor(),
		...Array.from({ length: mutationConcurrency }, (_, worker) => saveWorker(worker)),
	]);
	sampler.stop();
	await sampler.done;
	for (const outcome of settled) if (outcome.status === "rejected") throw outcome.reason;
	const durationMs = performance.now() - started;
	return [saves.result(durationMs, peak), edits.result(durationMs, peak)];
}

type HotTargetResponse = { status: number | "timeout"; elapsedMs: number; body: string };

async function hotRequest(url: string, init: RequestInit): Promise<HotTargetResponse> {
	const started = performance.now();
	try {
		const response = await fetch(url, { ...init, signal: AbortSignal.timeout(requestTimeoutMs) });
		const body = await response.text();
		return { status: response.status, elapsedMs: performance.now() - started, body };
	} catch (error) {
		if ((error as Error | undefined)?.name !== "TimeoutError") throw error;
		return { status: "timeout", elapsedMs: performance.now() - started, body: "" };
	}
}

/** Collects one side of the hot-target load; every request's duration counts, failed or not. */
class HotTargetRecorder {
	private readonly latencies: number[] = [];
	private readonly statuses: Record<string, number> = {};
	/** Responses whose content was checked against the request. */
	validated = 0;
	private bytes = 0;
	private failures = 0;
	private timeouts = 0;
	private firstFailure: string | undefined;

	constructor(
		private readonly name: string,
		private readonly concurrency: number
	) {}

	/** Records a response and reports whether it had an expected status. */
	record(response: HotTargetResponse, expected: number[]): boolean {
		const status = String(response.status);
		this.statuses[status] = (this.statuses[status] ?? 0) + 1;
		this.latencies.push(response.elapsedMs);
		this.bytes += response.body.length;
		if (response.status === "timeout") {
			this.timeouts += 1;
			this.firstFailure ??= `timed out after ${requestTimeoutMs} ms`;
			return false;
		}
		if (expected.includes(response.status)) return true;
		this.failures += 1;
		this.firstFailure ??= `${response.status} after ${round(response.elapsedMs)} ms: ${response.body.slice(0, 400)}`;
		return false;
	}

	result(durationMs: number, peakTreeRSSMiB: number): LoadResult {
		const latencies = [...this.latencies].sort((a, b) => a - b);
		const requests = latencies.length;
		if (this.firstFailure)
			console.log(
				`${this.name}: ${this.failures} failed, ${this.timeouts} timed out; first: ${this.firstFailure.slice(0, 300)}`
			);
		return {
			name: this.name,
			requests,
			concurrency: this.concurrency,
			durationMs: round(durationMs),
			requestsPerSecond: round((requests * 1_000) / durationMs),
			latencyMs: {
				p50: round(percentile(latencies, 0.5)),
				p95: round(percentile(latencies, 0.95)),
				p99: round(percentile(latencies, 0.99)),
				max: round(latencies.at(-1) ?? 0),
			},
			responseBytes: { average: requests === 0 ? 0 : round(this.bytes / requests) },
			statuses: this.statuses,
			validatedResponses: this.validated,
			peakTreeRSSMiB: round(peakTreeRSSMiB),
			failures: this.failures,
			timeouts: this.timeouts,
			...(this.firstFailure ? { firstFailure: this.firstFailure } : {}),
		};
	}
}

// ---------------------------------------------------------------------------------------------
// Load generation and process measurement (same method as tests/performance/compare.ts).

async function runLoad(
	name: string,
	requests: number,
	concurrency: number,
	pid: number,
	request: (index: number) => Promise<Sample>
): Promise<LoadResult> {
	console.log(`${name}: ${requests} requests at concurrency ${concurrency}`);
	let peak = await treeRSSMiB(pid);
	const sampler = sampleUntil(pid, (value) => (peak = Math.max(peak, value)));
	const started = performance.now();
	const samples = await parallelMap(requests, concurrency, request);
	const durationMs = performance.now() - started;
	sampler.stop();
	await sampler.done;
	const latencies = samples.map((sample) => sample.elapsedMs).sort((a, b) => a - b);
	const statuses: Record<string, number> = {};
	let bytes = 0;
	for (const sample of samples) {
		statuses[String(sample.status)] = (statuses[String(sample.status)] ?? 0) + 1;
		bytes += sample.bytes;
	}
	return {
		name,
		requests,
		concurrency,
		durationMs: round(durationMs),
		requestsPerSecond: round((requests * 1_000) / durationMs),
		latencyMs: {
			p50: round(percentile(latencies, 0.5)),
			p95: round(percentile(latencies, 0.95)),
			p99: round(percentile(latencies, 0.99)),
			max: round(latencies.at(-1) ?? 0),
		},
		responseBytes: { average: round(bytes / requests) },
		statuses,
		validatedResponses: samples.filter((sample) => sample.validated).length,
		peakTreeRSSMiB: round(peak),
	};
}

function sampleUntil(
	pid: number,
	observe: (value: number) => void
): { stop: () => void; done: Promise<void> } {
	let running = true;
	const done = (async () => {
		while (running) {
			try {
				observe(await treeRSSMiB(pid));
			} catch {
				// The process may exit between samples.
			}
			await delay(100);
		}
	})();
	return { stop: () => (running = false), done };
}

async function timedFetch(
	url: string,
	init: RequestInit,
	expected: number[]
): Promise<{ status: number; bytes: number; elapsedMs: number; json: () => unknown }> {
	const started = performance.now();
	const response = await fetch(url, {
		...init,
		signal: init.signal ?? AbortSignal.timeout(requestTimeoutMs),
	});
	const body = await response.arrayBuffer();
	const elapsedMs = performance.now() - started;
	if (!expected.includes(response.status)) {
		throw new Error(
			`${init.method ?? "GET"} ${url} returned ${response.status}: ${new TextDecoder().decode(body).slice(0, 600)}`
		);
	}
	return {
		status: response.status,
		bytes: body.byteLength,
		elapsedMs,
		json: () => JSON.parse(new TextDecoder().decode(body)),
	};
}

/** Runs `count` jobs on `workers` workers; after the first failure no new job starts. */
async function parallelMap<T>(
	count: number,
	workers: number,
	work: (index: number) => Promise<T>
): Promise<T[]> {
	const out = new Array<T>(count);
	let next = 0;
	let failure: unknown;
	const settled = await Promise.allSettled(
		Array.from({ length: Math.min(count, workers) }, async () => {
			while (failure === undefined) {
				const index = next;
				next += 1;
				if (index >= count) return;
				try {
					out[index] = await work(index);
				} catch (error) {
					failure ??= error;
					throw error;
				}
			}
		})
	);
	if (failure !== undefined || settled.some((result) => result.status === "rejected"))
		throw failure;
	return out;
}

async function waitUntilReady(url: string, server: Bun.Subprocess): Promise<void> {
	const deadline = Date.now() + startupTimeoutMs;
	let last = "not ready";
	while (Date.now() < deadline) {
		if (server.exitCode !== null) throw new Error(`server exited with ${server.exitCode}: ${last}`);
		try {
			const response = await fetch(url, { signal: AbortSignal.timeout(10_000) });
			const body = await response.text();
			if (response.status === 200) return;
			last = `${response.status}: ${body.slice(0, 300)}`;
		} catch (error) {
			last = String(error);
		}
		await delay(100);
	}
	throw new Error(`timed out waiting for ${url}: ${last}`);
}

async function stableRSS(pid: number): Promise<number> {
	const samples: number[] = [];
	for (let index = 0; index < 10; index += 1) {
		samples.push(await treeRSSMiB(pid));
		await delay(200);
	}
	return samples.sort((a, b) => a - b)[5]!;
}

async function treeRSSMiB(root: number): Promise<number> {
	const process = Bun.spawn(["ps", "-axo", "pid=,ppid=,rss="], { stdout: "pipe", stderr: "pipe" });
	const text = await new Response(process.stdout).text();
	await process.exited;
	const children = new Map<number, number[]>();
	const rss = new Map<number, number>();
	for (const line of text.split("\n")) {
		const [pid = NaN, parent = NaN, resident = NaN] = line.trim().split(/\s+/).map(Number);
		if (!Number.isFinite(pid) || !Number.isFinite(resident)) continue;
		rss.set(pid, resident);
		children.set(parent, [...(children.get(parent) ?? []), pid]);
	}
	let total = 0;
	const stack = [root];
	const seen = new Set<number>();
	while (stack.length) {
		const pid = stack.pop()!;
		if (seen.has(pid)) continue;
		seen.add(pid);
		total += rss.get(pid) ?? 0;
		stack.push(...(children.get(pid) ?? []));
	}
	return total / 1024;
}

// ---------------------------------------------------------------------------------------------
// Disposable database containers and schema inspection.

function startPostgres(): number {
	Bun.spawnSync(["docker", "rm", "-f", postgresContainer], { stdout: "ignore", stderr: "ignore" });
	run(
		[
			"docker",
			"run",
			"-d",
			"--name",
			postgresContainer,
			"--label",
			"ridu-blocks-benchmark=true",
			"-e",
			"POSTGRES_USER=bench",
			"-e",
			"POSTGRES_PASSWORD=bench",
			"-e",
			"POSTGRES_DB=bench",
			"-p",
			"127.0.0.1::5432",
			"postgres:17-alpine",
		],
		repository
	);
	const port = Number(output("docker", ["port", postgresContainer, "5432/tcp"]).split(":").at(-1));
	for (let attempt = 0; attempt < 120; attempt += 1) {
		if (
			Bun.spawnSync([
				"docker",
				"exec",
				postgresContainer,
				"pg_isready",
				"-U",
				"bench",
				"-d",
				"bench",
			]).exitCode === 0 &&
			Bun.spawnSync(["psql", `postgres://bench:bench@127.0.0.1:${port}/bench`, "-Atqc", "select 1"])
				.exitCode === 0
		)
			return port;
		Bun.sleepSync(500);
	}
	throw new Error("PostgreSQL container did not become ready");
}

function startMongo(): number {
	run(
		[
			"docker",
			"compose",
			"-p",
			mongoProject,
			"-f",
			mongoCompose,
			"down",
			"--volumes",
			"--remove-orphans",
		],
		repository
	);
	run(
		["docker", "compose", "-p", mongoProject, "-f", mongoCompose, "up", "-d", "--wait", "mongodb"],
		repository
	);
	return Number(
		output("docker", [
			"compose",
			"-p",
			mongoProject,
			"-f",
			mongoCompose,
			"port",
			"mongodb",
			"27017",
		])
			.split(":")
			.at(-1)
	);
}

function stopDatabases(): void {
	Bun.spawnSync(["docker", "rm", "-f", "-v", postgresContainer], {
		stdout: "ignore",
		stderr: "ignore",
	});
	Bun.spawnSync(
		[
			"docker",
			"compose",
			"-p",
			mongoProject,
			"-f",
			mongoCompose,
			"down",
			"--volumes",
			"--remove-orphans",
		],
		{ stdout: "ignore", stderr: "ignore" }
	);
}

function recreateDatabase(database: DatabaseName, name: string): void {
	if (!/^[a-z0-9_]+$/.test(name)) throw new Error(`refusing database name ${name}`);
	if (database === "postgres") {
		dropDatabase(database, name);
		run(
			[
				"psql",
				`postgres://bench:bench@127.0.0.1:${postgresPort}/bench`,
				"-qc",
				`CREATE DATABASE ${name}`,
			],
			repository
		);
		return;
	}
	dropDatabase(database, name);
}

function dropDatabase(database: DatabaseName, name: string): void {
	if (database === "postgres") {
		run(
			[
				"psql",
				`postgres://bench:bench@127.0.0.1:${postgresPort}/bench`,
				"-qc",
				`DROP DATABASE IF EXISTS ${name} WITH (FORCE)`,
			],
			repository
		);
		return;
	}
	run(
		[
			"docker",
			"compose",
			"-p",
			mongoProject,
			"-f",
			mongoCompose,
			"exec",
			"-T",
			"mongodb",
			"mongosh",
			"--quiet",
			"--eval",
			`db.getSiblingDB(${JSON.stringify(name)}).dropDatabase()`,
		],
		repository
	);
}

function killMongoOperations(name: string): void {
	const script = `for (const o of db.currentOp({ active: true, ns: new RegExp("^" + ${JSON.stringify(name)} + "\\\\.") }).inprog) db.killOp(o.opid)`;
	Bun.spawnSync(
		[
			"docker",
			"compose",
			"-p",
			mongoProject,
			"-f",
			mongoCompose,
			"exec",
			"-T",
			"mongodb",
			"mongosh",
			"--quiet",
			"--eval",
			script,
		],
		{ stdout: "ignore", stderr: "inherit" }
	);
}

function physicalSchema(database: DatabaseName, name: string): Record<string, unknown> {
	if (database === "postgres") {
		const query = `SELECT json_build_object(
			'tables', (SELECT count(*) FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog','information_schema')),
			'columns', (SELECT count(*) FROM information_schema.columns WHERE table_schema NOT IN ('pg_catalog','information_schema')),
			'indexes', (SELECT count(*) FROM pg_indexes WHERE schemaname NOT IN ('pg_catalog','information_schema')),
			'enumTypes', (SELECT count(*) FROM pg_type WHERE typtype = 'e'))`;
		return JSON.parse(
			output("psql", [`postgres://bench:bench@127.0.0.1:${postgresPort}/${name}`, "-Atqc", query])
		);
	}
	const script = `const d=db.getSiblingDB(${JSON.stringify(name)});let c=0,i=0;for (const n of d.getCollectionNames()){c++;i+=d.getCollection(n).getIndexes().length};print(JSON.stringify({collections:c,indexes:i}))`;
	return JSON.parse(
		output("docker", [
			"compose",
			"-p",
			mongoProject,
			"-f",
			mongoCompose,
			"exec",
			"-T",
			"mongodb",
			"mongosh",
			"--quiet",
			"--eval",
			script,
		])
	);
}

// ---------------------------------------------------------------------------------------------
// Utilities.

function environment(): Record<string, unknown> {
	const gitStatus = output("git", ["status", "--porcelain=v1"]);
	return {
		platform: output("sw_vers", ["-productVersion"]),
		cpu: output("sysctl", ["-n", "machdep.cpu.brand_string"]),
		memoryBytes: Number(output("sysctl", ["-n", "hw.memsize"])),
		go: output("go", ["version"]),
		bun: output("bun", ["--version"]),
		node: output("node", ["--version"]),
		postgresImage: "postgres:17-alpine",
		postgresVersion: output("psql", [
			`postgres://bench:bench@127.0.0.1:${postgresPort}/bench`,
			"-Atqc",
			"show server_version",
		]),
		mongoImage:
			"mongo:8.2.9-noble (adapters/mongodb/testdata/compose.yaml, single-member replica set)",
		docker: output("docker", [
			"info",
			"--format",
			"{{.ServerVersion}} {{.NCPU}} CPUs {{.MemTotal}} bytes",
		]),
		...(frameworkNames.includes("payload") ? payloadVersions() : {}),
		riduRevision: output("git", ["rev-parse", "HEAD"]),
		gitDirty: gitStatus !== "",
		gitStatus: gitStatus === "" ? [] : gitStatus.split("\n"),
		harness: Object.fromEntries(
			[
				"run.ts",
				"spec.ts",
				"ridu/main.go",
				"blockspec/blockspec.go",
				"payload/src/payload.config.ts",
			].map((file) => [file, sha256(join(harness, file))])
		),
	};
}

function payloadVersions(): Record<string, string> {
	const version = (name: string) =>
		JSON.parse(readFileSync(join(payloadDirectory, `node_modules/${name}/package.json`), "utf8"))
			.version as string;
	return { payload: version("payload"), mongoose: version("mongoose"), next: version("next") };
}

function inheritedRuntimeTuning(): Record<string, string> {
	const tuning: Record<string, string> = {};
	for (const name of ["GOGC", "GOMEMLIMIT", "GOMAXPROCS", "GODEBUG", "NODE_OPTIONS"]) {
		const value = Bun.env[name];
		if (value) tuning[name] = name === "NODE_OPTIONS" ? "(set)" : value;
	}
	return tuning;
}

function run(
	command: string[],
	cwd: string,
	environmentVariables: Record<string, string> = {}
): void {
	const result = Bun.spawnSync(command, {
		cwd,
		env: { ...Bun.env, ...environmentVariables },
		stdout: "inherit",
		stderr: "inherit",
	});
	if (result.exitCode !== 0) throw new Error(`${command.join(" ")} exited with ${result.exitCode}`);
}

/** Runs a command, returning stdout and throwing with the stderr tail on failure. */
function runCaptured(
	command: string[],
	cwd: string,
	environmentVariables: Record<string, string> = {}
): string {
	const result = Bun.spawnSync(command, {
		cwd,
		env: { ...Bun.env, ...environmentVariables },
		stdout: "pipe",
		stderr: "pipe",
	});
	if (result.exitCode !== 0) {
		throw new Error(
			`${command.join(" ")} exited with ${result.exitCode}: ${result.stderr.toString().slice(-1_500)}`
		);
	}
	return result.stdout.toString();
}

function output(
	command: string,
	args: string[],
	cwd = repository,
	environmentVariables: Record<string, string> = {},
	mergeStderr = false
): string {
	if (mergeStderr) {
		const result = Bun.spawnSync([command, ...args], {
			cwd,
			env: { ...Bun.env, ...environmentVariables },
			stdout: "pipe",
			stderr: "pipe",
		});
		return `${result.stdout.toString()}\n${result.stderr.toString()}`;
	}
	return execFileSync(command, args, {
		cwd,
		env: { ...Bun.env, ...environmentVariables },
		encoding: "utf8",
	}).trim();
}

function timed<T>(work: () => T): { value: T; ms: number } {
	const started = performance.now();
	const value = work();
	return { value, ms: round(performance.now() - started) };
}

function directoryBytes(directory: string): number {
	if (!existsSync(directory)) return 0;
	let total = 0;
	for (const entry of readdirSync(directory, { withFileTypes: true, recursive: true })) {
		if (entry.isFile()) total += statSync(join(entry.parentPath, entry.name)).size;
	}
	return total;
}

function tail(path: string): string {
	return existsSync(path) ? readFileSync(path, "utf8").slice(-2_000) : "";
}

function sha256(path: string): string {
	return createHash("sha256").update(readFileSync(path)).digest("hex");
}

function assertPortFree(port: number): void {
	const owner = Bun.spawnSync(["lsof", "-nP", `-iTCP:${port}`, "-sTCP:LISTEN", "-t"], {
		stdout: "pipe",
	})
		.stdout.toString()
		.trim();
	if (owner) throw new Error(`benchmark port ${port} is owned by process ${owner}`);
}

function percentile(sorted: number[], quantile: number): number {
	return sorted.length === 0
		? 0
		: sorted[Math.min(sorted.length - 1, Math.ceil(sorted.length * quantile) - 1)]!;
}

function positiveInteger(name: string, fallback: number): number {
	const value = Number(Bun.env[name] ?? fallback);
	if (!Number.isSafeInteger(value) || value <= 0)
		throw new Error(`${name} must be a positive integer`);
	return value;
}

function nonNegativeInteger(name: string, fallback: number): number {
	const value = Number(Bun.env[name] ?? fallback);
	if (!Number.isSafeInteger(value) || value < 0)
		throw new Error(`${name} must be a non-negative integer`);
	return value;
}

function round(value: number): number {
	return Math.round(value * 100) / 100;
}

function delay(milliseconds: number): Promise<void> {
	return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

void scenarioNames;
