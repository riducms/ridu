import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
	cpSync,
	createReadStream,
	existsSync,
	lstatSync,
	mkdirSync,
	readFileSync,
	readdirSync,
	readlinkSync,
	rmSync,
	statSync,
} from "node:fs";
import { hostname } from "node:os";
import { basename, join, relative, resolve } from "node:path";

type FrameworkName = "ridu" | "payload";

type Framework = {
	name: FrameworkName;
	baseURL: string;
	database: BenchmarkDatabase;
	command: string[];
	cwd: string;
	environment: Record<string, string>;
	readyPath: string;
};

type BenchmarkDatabase = "ridu_stress_benchmark" | "payload_stress_benchmark";

type Validation = (body: unknown) => string | null;

type RequestSpec = {
	operation: string;
	url: string;
	init?: RequestInit;
	expectedStatuses: number[];
	validate: Validation;
};

type RequestSample = {
	sequence: number;
	worker: number;
	workerSequence: number;
	operation: string;
	startedOffsetMs: number;
	latencyMs: number;
	status?: number;
	bytes: number;
	success: boolean;
	timeout: boolean;
	jsonValid: boolean;
	shapeValid: boolean;
	error?: string;
	detail?: string;
};

type RequestObservation = {
	sample: Omit<RequestSample, "sequence" | "worker" | "workerSequence" | "startedOffsetMs">;
	body?: unknown;
};

type NumericDistribution = {
	min: number;
	median: number;
	p95: number;
	max: number;
};

type LatencyDistribution = {
	p50: number;
	p90: number;
	p95: number;
	p99: number;
	"p99.9": number;
	max: number;
};

type ApplicationResourceSample = {
	elapsedMs: number;
	rssMiB?: number;
	cpuPercent?: number;
	processes?: number;
	error?: string;
};

type PostgresResourceSample = {
	elapsedMs: number;
	total?: number;
	active?: number;
	idle?: number;
	idleInTransaction?: number;
	waiting?: number;
	states?: Record<string, number>;
	error?: string;
};

type ResourceSeries = {
	applicationSamples: ApplicationResourceSample[];
	postgresSamples: PostgresResourceSample[];
	summary: {
		application: {
			rssMiB: NumericDistribution;
			cpuPercent: NumericDistribution;
			processes: NumericDistribution;
		};
		postgres: {
			total: NumericDistribution;
			active: NumericDistribution;
			idle: NumericDistribution;
			idleInTransaction: NumericDistribution;
			waiting: NumericDistribution;
		};
	};
};

type WorkloadResult = {
	name: string;
	mode: "fixed-count" | "duration";
	startedAt: string;
	concurrency: number;
	targetRequests?: number;
	targetDurationMs?: number;
	durationMs: number;
	requests: number;
	successes: number;
	failures: number;
	timeouts: number;
	invalidJSON: number;
	invalidShapes: number;
	requestsPerSecond: number;
	statuses: Record<string, number>;
	errors: Record<string, number>;
	latencyMs: LatencyDistribution;
	successfulLatencyMs: LatencyDistribution;
	responseBytes: {
		total: number;
		average: number;
	};
	operations: Record<string, Omit<WorkloadResult, "operations" | "resource" | "samples">>;
	resource: ResourceSeries;
	samples: RequestSample[];
	sampleLimitReached?: boolean;
	failureBudgetReached?: boolean;
};

type SeedReferences = {
	authorID: string;
	categoryID: string;
	relatedPostID: string;
	contributorID: string;
};

type SemanticCounts = {
	admin: number;
	public: number;
	contributor: number;
};

type HealthCheck = {
	label: string;
	at: string;
	latencyMs: number;
	status?: number;
	ok: boolean;
	error?: string;
};

type Artifact = {
	path: string;
	bytes: number;
	sha256: string;
	modifiedAt: string;
};

type TreeArtifact = {
	path: string;
	files: number;
	bytes: number;
	sha256: string;
};

const repository = resolve(import.meta.dir, "../..");
const payloadDirectory = join(repository, "tests/contracts/payload_server");
const payloadStandaloneDirectory = join(payloadDirectory, ".next/standalone");
const payloadServer = join(payloadStandaloneDirectory, "server.js");
const payloadStaticSource = join(payloadDirectory, ".next/static");
const payloadStaticDestination = join(payloadStandaloneDirectory, ".next/static");
const payloadRuntimeMedia = join(payloadStandaloneDirectory, "media");
const outputDirectory = join(repository, ".ridu/performance");
const riduBinary = join(outputDirectory, "ridu-server");
const riduAdminDirectory = join(repository, ".ridu/admin-fixture-build");
const harnessPath = join(repository, "tests/performance/stress.ts");
const payloadSecret = "ridu-performance-payload-secret";
const fixedRiduSchema = "ridu_admin_fixture_stress";
const allowedDatabases = new Set<BenchmarkDatabase>([
	"ridu_stress_benchmark",
	"payload_stress_benchmark",
]);

const databaseURLTemplate =
	Bun.env.RIDU_STRESS_POSTGRES_URL_TEMPLATE ??
	Bun.env.RIDU_PERF_POSTGRES_URL_TEMPLATE ??
	`postgres://${encodeURIComponent(Bun.env.USER ?? "postgres")}@127.0.0.1:5432/{database}?sslmode=disable`;
if (!databaseURLTemplate.includes("{database}")) {
	throw new Error("RIDU_STRESS_POSTGRES_URL_TEMPLATE must contain {database}");
}

const databaseURL = (database: BenchmarkDatabase | "postgres"): string => {
	const value = databaseURLTemplate.replace("{database}", database);
	const parsed = new URL(value);
	if (parsed.protocol !== "postgres:" && parsed.protocol !== "postgresql:") {
		throw new Error("the stress PostgreSQL URL template must use postgres:// or postgresql://");
	}
	if (decodeURIComponent(basename(parsed.pathname)) !== database) {
		throw new Error(`the stress PostgreSQL URL template did not resolve to database ${database}`);
	}
	return value;
};

const postgresConnection = new URL(databaseURL("postgres"));
const datasetSize = positiveInteger("RIDU_STRESS_DATASET", 10_000);
const seedConcurrency = positiveInteger("RIDU_STRESS_SEED_CONCURRENCY", 12);
const matchedReadRequests = positiveInteger("RIDU_STRESS_MATCHED_READ_REQUESTS", 1_000);
const matchedMutationRequests = positiveInteger("RIDU_STRESS_MATCHED_MUTATION_REQUESTS", 250);
const matchedConcurrency = positiveInteger("RIDU_STRESS_MATCHED_CONCURRENCY", 16);
const readConcurrencies = positiveIntegerList("RIDU_STRESS_READ_CONCURRENCIES", [1, 8, 32, 128]);
const writeConcurrencies = positiveIntegerList(
	"RIDU_STRESS_WRITE_CONCURRENCIES",
	[1, 4, 8, 16, 32]
);
const saturationDurationMs = positiveNumber("RIDU_STRESS_DURATION_SECONDS", 10) * 1_000;
const soakDurationMs = positiveNumber("RIDU_STRESS_SOAK_SECONDS", 120) * 1_000;
const soakConcurrency = positiveInteger("RIDU_STRESS_SOAK_CONCURRENCY", 8);
const idleDurationMs = positiveNumber("RIDU_STRESS_IDLE_SECONDS", 3) * 1_000;
const recoveryDurationMs = positiveNumber("RIDU_STRESS_RECOVERY_SECONDS", 10) * 1_000;
const requestTimeoutMs = positiveInteger("RIDU_STRESS_REQUEST_TIMEOUT_MS", 30_000);
const holdDurationMs = nonNegativeNumber("RIDU_STRESS_HOLD_SECONDS", 0) * 1_000;
const maximumSamplesPerWorkload = positiveInteger("RIDU_STRESS_MAX_SAMPLES", 1_000_000);
const failureBudget = positiveInteger("RIDU_STRESS_FAILURE_BUDGET", 64);
const selectedFrameworks = frameworkFilter();

mkdirSync(outputDirectory, { recursive: true });
if (selectedFrameworks.includes("ridu")) {
	assertFile(riduBinary, "Ridu production binary");
	assertDirectory(riduAdminDirectory, "Ridu application-specific production admin");
}
if (selectedFrameworks.includes("payload")) {
	assertFile(payloadServer, "Payload standalone server");
	if (!existsSync(payloadStaticSource)) {
		throw new Error(`Payload static assets do not exist: ${payloadStaticSource}`);
	}
	if (resolve(payloadStaticDestination) !== resolve(payloadStandaloneDirectory, ".next/static")) {
		throw new Error("refusing to replace an unexpected Payload static directory");
	}
	if (resolve(payloadRuntimeMedia) !== resolve(payloadStandaloneDirectory, "media")) {
		throw new Error("refusing to clear an unexpected Payload runtime media directory");
	}
	rmSync(payloadStaticDestination, { force: true, recursive: true });
	rmSync(payloadRuntimeMedia, { force: true, recursive: true });
	cpSync(payloadStaticSource, payloadStaticDestination, { force: true, recursive: true });
}

const allFrameworks: Framework[] = [
	{
		name: "ridu",
		baseURL: "http://127.0.0.1:18181",
		database: "ridu_stress_benchmark",
		command: [riduBinary],
		cwd: repository,
		environment: {
			RIDU_BROWSER_ADDRESS: "127.0.0.1:18181",
			RIDU_BROWSER_PREVIEW_ADDRESS: "127.0.0.1:18182",
			RIDU_BROWSER_ADMIN_DIR: riduAdminDirectory,
			RIDU_POSTGRES_FIXTURE_SCHEMA: fixedRiduSchema,
		},
		readyPath: "/readyz",
	},
	{
		name: "payload",
		baseURL: "http://127.0.0.1:3010",
		database: "payload_stress_benchmark",
		command: ["node", payloadServer],
		cwd: payloadStandaloneDirectory,
		environment: {
			HOSTNAME: "127.0.0.1",
			NODE_ENV: "production",
			PAYLOAD_SECRET: payloadSecret,
			PORT: "3010",
		},
		readyPath:
			"/api/posts?limit=1&depth=0&select[title]=true&select[summary]=true&select[status]=true",
	},
];
const frameworks = allFrameworks.filter((framework) => selectedFrameworks.includes(framework.name));

const startedAt = new Date();
const stamp = startedAt.toISOString().replaceAll(":", "-");
const resultPath = join(outputDirectory, `stress-${stamp}.json`);
const latestPath = join(outputDirectory, "latest-stress.json");
let activeServer: Bun.Subprocess | undefined;
let interruptedSignal: string | undefined;

const report: Record<string, unknown> = {
	version: 1,
	status: "running",
	startedAt: startedAt.toISOString(),
	invocation: {
		argv: Bun.argv,
		inheritedServerTuning: inheritedServerTuning(),
	},
	environment: await environmentReport(),
	artifacts: await artifactReport(),
	configuration: {
		frameworks: selectedFrameworks,
		databaseURLTemplate: redactDatabaseURL(databaseURLTemplate),
		databases: frameworks.map(({ name, database }) => ({ name, database })),
		fixedRiduSchema,
		datasetSize,
		seedConcurrency,
		matchedReadRequests,
		matchedMutationRequests,
		matchedConcurrency,
		readConcurrencies,
		writeConcurrencies,
		saturationDurationMs,
		soakDurationMs,
		soakConcurrency,
		idleDurationMs,
		recoveryDurationMs,
		requestTimeoutMs,
		holdDurationMs,
		maximumSamplesPerWorkload,
		failureBudget,
		applicationSamplingIntervalMs: 100,
		postgresSamplingIntervalMs: 1_000,
		postgresIncludedInApplicationRSS: false,
	},
	results: [] as Record<string, unknown>[],
};

const handleSignal = (signal: string) => {
	interruptedSignal = signal;
	if (activeServer !== undefined && activeServer.exitCode === null) {
		void terminateProcessTree(activeServer);
	}
};
process.once("SIGINT", () => handleSignal("SIGINT"));
process.once("SIGTERM", () => handleSignal("SIGTERM"));

try {
	await writeReport();
	for (const framework of frameworks) {
		if (interruptedSignal !== undefined) break;
		const result = await runFramework(framework);
		(report.results as Record<string, unknown>[]).push(result);
		await writeReport();
	}
	report.status = interruptedSignal === undefined ? "completed" : "interrupted";
} catch (error) {
	report.status = "failed";
	report.fatalError = errorText(error);
	throw error;
} finally {
	if (activeServer !== undefined) await terminateProcessTree(activeServer);
	report.finishedAt = new Date().toISOString();
	if (interruptedSignal !== undefined) report.interruptedSignal = interruptedSignal;
	await writeReport();
}

console.log(`\nRaw stress results: ${resultPath}`);

async function runFramework(framework: Framework): Promise<Record<string, unknown>> {
	console.log(`\n=== ${framework.name} production stress benchmark ===`);
	const result: Record<string, unknown> = {
		framework: framework.name,
		status: "running",
		startedAt: new Date().toISOString(),
		database: framework.database,
		baseURL: framework.baseURL,
		command: framework.command,
		serverEnvironment: framework.environment,
		inheritedServerTuning: inheritedServerTuning(),
		healthChecks: [] as HealthCheck[],
		readSaturation: [] as WorkloadResult[],
		matchedReads: [] as WorkloadResult[],
		matchedMutations: [] as WorkloadResult[],
		writeSaturation: [] as Record<string, unknown>[],
		errors: [] as string[],
	};
	let server: Bun.Subprocess | undefined;

	try {
		assertPortsAvailable(framework.name === "ridu" ? [18181, 18182] : [3010]);
		recreateDatabase(framework.database);
		if (framework.name === "payload") migratePayload(framework.database);

		const launchedAt = performance.now();
		server = Bun.spawn(framework.command, {
			cwd: framework.cwd,
			env: {
				...Bun.env,
				...framework.environment,
				...(framework.name === "ridu"
					? { RIDU_POSTGRES_URL: databaseURL(framework.database) }
					: { PAYLOAD_DATABASE_URL: databaseURL(framework.database) }),
			},
			stderr: "inherit",
			stdout: "inherit",
		});
		activeServer = server;
		result.serverPID = server.pid;
		await waitUntilReady(framework, server, 120_000);
		result.startupMs = round(performance.now() - launchedAt);

		const adminCookie = await login(framework, "admin@riducms.test", "ridu-admin");
		const contributorCookie = await login(framework, "demo@riducms.local", "ridu-demo");
		const references = await fetchSeedReferences(framework, adminCookie);
		result.seedReferences = references;
		const before = await semanticCounts(framework, adminCookie, contributorCookie);
		result.countsBeforeDataset = before;

		console.log(`seeding ${datasetSize} relationship-bearing published posts`);
		const seedingStarted = performance.now();
		const publishedIDs = await parallelMap(datasetSize, seedConcurrency, async (index) => {
			throwIfInterrupted();
			const id = await createPublishedPost(framework, adminCookie, references, index);
			if ((index + 1) % 1_000 === 0 || index + 1 === datasetSize) {
				console.log(`${framework.name}: seeded ${index + 1}/${datasetSize}`);
			}
			return id;
		});
		const seedingDurationMs = performance.now() - seedingStarted;
		result.dataset = {
			requested: datasetSize,
			created: publishedIDs.length,
			durationMs: round(seedingDurationMs),
			documentsPerSecond: round((publishedIDs.length * 1_000) / seedingDurationMs),
			idSHA256: createHash("sha256").update(publishedIDs.join("\n")).digest("hex"),
			firstIDs: publishedIDs.slice(0, 10),
			lastIDs: publishedIDs.slice(-10),
			relationships: {
				author: references.authorID,
				category: references.categoryID,
				relatedPosts: [references.relatedPostID],
			},
		};

		const expectedCounts: SemanticCounts = {
			admin: before.admin + datasetSize,
			public: before.public + datasetSize,
			contributor: before.contributor + datasetSize,
		};
		const after = await semanticCounts(framework, adminCookie, contributorCookie);
		assertSemanticCounts(framework.name, "after dataset seed", after, expectedCounts);
		result.countsAfterDataset = { actual: after, expected: expectedCounts, valid: true };

		await warm(framework, adminCookie, contributorCookie, publishedIDs, references, expectedCounts);
		result.preIdle = await sampleIdle("pre-idle", server.pid, framework.database, idleDurationMs);

		for (const concurrency of readConcurrencies) {
			const workload = await runDurationWorkload(
				`read-saturation-c${concurrency}`,
				saturationDurationMs,
				concurrency,
				server.pid,
				framework.database,
				(worker, workerSequence) => {
					if ((worker + workerSequence) % 2 === 0) {
						return requestJSON({
							operation: "list",
							url: publicListURL(framework),
							expectedStatuses: [200],
							validate: selectedListValidator(expectedCounts!.public),
						});
					}
					const id = publishedIDs[(worker + workerSequence) % publishedIDs.length]!;
					return requestJSON({
						operation: "find",
						url: findURL(framework, id, false),
						expectedStatuses: [200],
						validate: selectedDocumentValidator(id),
					});
				}
			);
			(result.readSaturation as WorkloadResult[]).push(workload);
			await recordHealth(framework, result, `after ${workload.name}`);
		}

		const matchedReads: Array<{
			name: string;
			request: (index: number) => Promise<RequestObservation>;
		}> = [
			{
				name: "matched-list",
				request: () =>
					requestJSON({
						operation: "list",
						url: publicListURL(framework),
						expectedStatuses: [200],
						validate: selectedListValidator(expectedCounts.public),
					}),
			},
			{
				name: "matched-find",
				request: (index) => {
					const id = publishedIDs[index % publishedIDs.length]!;
					return requestJSON({
						operation: "find",
						url: findURL(framework, id, false),
						expectedStatuses: [200],
						validate: selectedDocumentValidator(id),
					});
				},
			},
			{
				name: "matched-populated-find",
				request: (index) => {
					const id = publishedIDs[index % publishedIDs.length]!;
					return requestJSON({
						operation: "populated-find",
						url: findURL(framework, id, true),
						init: { headers: { Cookie: adminCookie } },
						expectedStatuses: [200],
						validate: populatedDocumentValidator(id, references),
					});
				},
			},
			{
				name: "matched-access-filtered-list",
				request: () =>
					requestJSON({
						operation: "access-filtered-list",
						url: accessFilteredListURL(framework),
						init: { headers: { Cookie: contributorCookie } },
						expectedStatuses: [200],
						validate: accessFilteredListValidator(
							expectedCounts.contributor,
							references.contributorID
						),
					}),
			},
		];
		for (const definition of matchedReads) {
			const workload = await runFixedWorkload(
				definition.name,
				matchedReadRequests,
				matchedConcurrency,
				server.pid,
				framework.database,
				(_worker, _workerSequence, index) => definition.request(index)
			);
			(result.matchedReads as WorkloadResult[]).push(workload);
			await recordHealth(framework, result, `after ${workload.name}`);
		}

		const mutationIDs = new Array<string | undefined>(matchedMutationRequests);
		const createWorkload = await runFixedWorkload(
			"matched-create-draft",
			matchedMutationRequests,
			matchedConcurrency,
			server.pid,
			framework.database,
			async (_worker, _workerSequence, index) => {
				const observation = await requestJSON(
					createDraftSpec(framework, adminCookie, references, `matched-${index}`)
				);
				if (observation.sample.success) {
					const document = extractDocument(observation.body);
					if (document !== null) mutationIDs[index] = String(document.id);
				}
				return observation;
			}
		);
		(result.matchedMutations as WorkloadResult[]).push(createWorkload);
		await recordHealth(framework, result, `after ${createWorkload.name}`);

		const usableMutationIDs = mutationIDs.filter((id): id is string => id !== undefined);
		const updateWorkload = await runFixedWorkload(
			"matched-update-draft",
			matchedMutationRequests,
			matchedConcurrency,
			server.pid,
			framework.database,
			(_worker, _workerSequence, index) => {
				const id = usableMutationIDs[index % usableMutationIDs.length] ?? "missing-draft";
				return requestJSON(updateDraftSpec(framework, adminCookie, id, `Matched update ${index}`));
			}
		);
		(result.matchedMutations as WorkloadResult[]).push(updateWorkload);
		await recordHealth(framework, result, `after ${updateWorkload.name}`);

		const deleteWorkload = await runFixedWorkload(
			"matched-delete-draft",
			matchedMutationRequests,
			matchedConcurrency,
			server.pid,
			framework.database,
			(_worker, _workerSequence, index) => {
				const id = mutationIDs[index] ?? "missing-draft";
				return requestJSON(deleteDraftSpec(framework, adminCookie, id));
			}
		);
		(result.matchedMutations as WorkloadResult[]).push(deleteWorkload);
		await recordHealth(framework, result, `after ${deleteWorkload.name}`);
		result.reconciliationAfterMatchedMutations = await reconcileCounts(
			framework,
			adminCookie,
			contributorCookie,
			expectedCounts
		);

		for (const concurrency of writeConcurrencies) {
			const phase: Record<string, unknown> = { concurrency };
			try {
				const workerDrafts = await createWorkerDrafts(
					framework,
					adminCookie,
					references,
					`write-c${concurrency}`,
					concurrency
				);
				phase.workerDraftIDs = workerDrafts;
				phase.workload = await runDurationWorkload(
					`versioned-update-saturation-c${concurrency}`,
					saturationDurationMs,
					concurrency,
					server.pid,
					framework.database,
					(worker, workerSequence) =>
						requestJSON(
							updateDraftSpec(
								framework,
								adminCookie,
								workerDrafts[worker]!,
								`Version saturation ${concurrency}-${worker}-${workerSequence}`
							)
						)
				);
				phase.cleanup = await deleteDraftsBestEffort(framework, adminCookie, workerDrafts);
			} catch (error) {
				phase.error = errorText(error);
				(result.errors as string[]).push(
					`versioned write saturation c${concurrency}: ${errorText(error)}`
				);
			}
			phase.health = await recordHealth(
				framework,
				result,
				`after versioned-update-saturation-c${concurrency}`
			);
			phase.reconciliation = await reconcileCounts(
				framework,
				adminCookie,
				contributorCookie,
				expectedCounts
			);
			(result.writeSaturation as Record<string, unknown>[]).push(phase);
		}

		const soak: Record<string, unknown> = {
			durationMs: soakDurationMs,
			concurrency: soakConcurrency,
		};
		try {
			const workerDrafts = await createWorkerDrafts(
				framework,
				adminCookie,
				references,
				"mixed-soak",
				soakConcurrency
			);
			soak.workerDraftIDs = workerDrafts;
			soak.workload = await runDurationWorkload(
				`mixed-soak-c${soakConcurrency}`,
				soakDurationMs,
				soakConcurrency,
				server.pid,
				framework.database,
				(worker, workerSequence) => {
					const operation = (worker + workerSequence) % 3;
					if (operation === 0) {
						const id = publishedIDs[(worker + workerSequence) % publishedIDs.length]!;
						return requestJSON({
							operation: "point",
							url: findURL(framework, id, false),
							expectedStatuses: [200],
							validate: selectedDocumentValidator(id),
						});
					}
					if (operation === 1) {
						return requestJSON({
							operation: "list",
							url: publicListURL(framework),
							expectedStatuses: [200],
							validate: selectedListValidator(expectedCounts!.public),
						});
					}
					return requestJSON(
						updateDraftSpec(
							framework,
							adminCookie,
							workerDrafts[worker]!,
							`Soak update ${worker}-${workerSequence}`
						)
					);
				}
			);
			soak.cleanup = await deleteDraftsBestEffort(framework, adminCookie, workerDrafts);
		} catch (error) {
			soak.error = errorText(error);
			(result.errors as string[]).push(`mixed soak: ${errorText(error)}`);
		}
		soak.health = await recordHealth(framework, result, "after mixed soak");
		soak.reconciliation = await reconcileCounts(
			framework,
			adminCookie,
			contributorCookie,
			expectedCounts
		);
		result.soak = soak;

		result.postIdle = await sampleIdle("post-idle", server.pid, framework.database, idleDurationMs);
		result.recovery = await sampleRecovery(
			framework,
			server.pid,
			framework.database,
			recoveryDurationMs
		);
		result.finalReconciliation = await reconcileCounts(
			framework,
			adminCookie,
			contributorCookie,
			expectedCounts
		);

		if (holdDurationMs > 0) {
			console.log(
				`${framework.name} is held at ${framework.baseURL}/admin/login for ${round(holdDurationMs / 1_000)} seconds`
			);
			await interruptibleDelay(holdDurationMs);
			throwIfInterrupted();
		}

		result.status =
			(result.errors as string[]).length === 0 ? "completed" : "completed-with-errors";
	} catch (error) {
		result.status = "failed";
		result.error = errorText(error);
		(result.errors as string[]).push(errorText(error));
	} finally {
		if (server !== undefined) {
			await terminateProcessTree(server);
			result.serverExitCode = server.exitCode;
		}
		activeServer = undefined;
		result.finishedAt = new Date().toISOString();
	}

	return result;
}

async function runDurationWorkload(
	name: string,
	targetDurationMs: number,
	concurrency: number,
	pid: number,
	database: BenchmarkDatabase,
	request: (
		worker: number,
		workerSequence: number,
		globalSequence: number
	) => Promise<RequestObservation>
): Promise<WorkloadResult> {
	throwIfInterrupted();
	console.log(`${name}: ${round(targetDurationMs / 1_000)}s at concurrency ${concurrency}`);
	const wallStarted = new Date();
	const started = performance.now();
	const deadline = started + targetDurationMs;
	const samples: RequestSample[] = [];
	const resourceSampler = startResourceSampler(pid, database);
	let globalSequence = 0;
	let sampleLimitReached = false;
	let failures = 0;
	let failureBudgetReached = false;

	await Promise.all(
		Array.from({ length: concurrency }, async (_, worker) => {
			let workerSequence = 0;
			while (interruptedSignal === undefined && performance.now() < deadline) {
				if (failures >= failureBudget) {
					failureBudgetReached = true;
					return;
				}
				const sequence = globalSequence;
				globalSequence += 1;
				if (sequence >= maximumSamplesPerWorkload) {
					sampleLimitReached = true;
					return;
				}
				const requestStarted = performance.now();
				const observation = await safeObservation(() => request(worker, workerSequence, sequence));
				samples.push({
					...observation.sample,
					sequence,
					worker,
					workerSequence,
					startedOffsetMs: round(requestStarted - started),
				});
				if (!observation.sample.success) failures += 1;
				workerSequence += 1;
			}
		})
	);

	const durationMs = performance.now() - started;
	const resource = await resourceSampler.stop();
	throwIfInterrupted();
	samples.sort((left, right) => left.sequence - right.sequence);
	return summarizeWorkload(
		name,
		"duration",
		wallStarted,
		concurrency,
		durationMs,
		samples,
		resource,
		undefined,
		targetDurationMs,
		sampleLimitReached,
		failureBudgetReached
	);
}

async function runFixedWorkload(
	name: string,
	targetRequests: number,
	concurrency: number,
	pid: number,
	database: BenchmarkDatabase,
	request: (
		worker: number,
		workerSequence: number,
		globalSequence: number
	) => Promise<RequestObservation>
): Promise<WorkloadResult> {
	throwIfInterrupted();
	console.log(`${name}: ${targetRequests} requests at concurrency ${concurrency}`);
	const wallStarted = new Date();
	const started = performance.now();
	const samples = new Array<RequestSample>(targetRequests);
	const resourceSampler = startResourceSampler(pid, database);
	let next = 0;
	let failures = 0;
	let failureBudgetReached = false;

	await Promise.all(
		Array.from({ length: Math.min(concurrency, targetRequests) }, async (_, worker) => {
			let workerSequence = 0;
			while (true) {
				if (failures >= failureBudget) {
					failureBudgetReached = true;
					return;
				}
				const sequence = next;
				next += 1;
				if (sequence >= targetRequests) return;
				const requestStarted = performance.now();
				const observation = await safeObservation(() => request(worker, workerSequence, sequence));
				samples[sequence] = {
					...observation.sample,
					sequence,
					worker,
					workerSequence,
					startedOffsetMs: round(requestStarted - started),
				};
				if (!observation.sample.success) failures += 1;
				workerSequence += 1;
			}
		})
	);

	const durationMs = performance.now() - started;
	const resource = await resourceSampler.stop();
	throwIfInterrupted();
	const completedSamples = samples.filter(
		(sample): sample is RequestSample => sample !== undefined
	);
	return summarizeWorkload(
		name,
		"fixed-count",
		wallStarted,
		concurrency,
		durationMs,
		completedSamples,
		resource,
		targetRequests,
		undefined,
		undefined,
		failureBudgetReached
	);
}

function summarizeWorkload(
	name: string,
	mode: "fixed-count" | "duration",
	startedAt: Date,
	concurrency: number,
	durationMs: number,
	samples: RequestSample[],
	resource: ResourceSeries,
	targetRequests?: number,
	targetDurationMs?: number,
	sampleLimitReached?: boolean,
	failureBudgetReached?: boolean
): WorkloadResult {
	const base = summarizeSamples(samples, durationMs);
	const grouped = new Map<string, RequestSample[]>();
	for (const sample of samples) {
		const operationSamples = grouped.get(sample.operation);
		if (operationSamples === undefined) grouped.set(sample.operation, [sample]);
		else operationSamples.push(sample);
	}
	const operations: WorkloadResult["operations"] = {};
	for (const [operation, operationSamples] of grouped) {
		operations[operation] = {
			name: operation,
			mode,
			startedAt: startedAt.toISOString(),
			concurrency,
			durationMs: round(durationMs),
			...summarizeSamples(operationSamples, durationMs),
			operations: {},
		} as Omit<WorkloadResult, "operations" | "resource" | "samples">;
	}
	return {
		name,
		mode,
		startedAt: startedAt.toISOString(),
		concurrency,
		...(targetRequests === undefined ? {} : { targetRequests }),
		...(targetDurationMs === undefined ? {} : { targetDurationMs }),
		durationMs: round(durationMs),
		...base,
		operations,
		resource,
		samples,
		...(sampleLimitReached ? { sampleLimitReached: true } : {}),
		...(failureBudgetReached ? { failureBudgetReached: true } : {}),
	};
}

function summarizeSamples(samples: RequestSample[], durationMs: number) {
	const statuses: Record<string, number> = {};
	const errors: Record<string, number> = {};
	let successes = 0;
	let timeouts = 0;
	let invalidJSON = 0;
	let invalidShapes = 0;
	let totalBytes = 0;
	for (const sample of samples) {
		if (sample.status !== undefined) {
			statuses[String(sample.status)] = (statuses[String(sample.status)] ?? 0) + 1;
		}
		if (sample.error !== undefined) {
			errors[sample.error] = (errors[sample.error] ?? 0) + 1;
		}
		if (sample.success) successes += 1;
		if (sample.timeout) timeouts += 1;
		if (sample.status !== undefined && !sample.jsonValid) invalidJSON += 1;
		if (sample.error === "invalid-success-shape") invalidShapes += 1;
		totalBytes += sample.bytes;
	}
	return {
		requests: samples.length,
		successes,
		failures: samples.length - successes,
		timeouts,
		invalidJSON,
		invalidShapes,
		requestsPerSecond: round((samples.length * 1_000) / Math.max(durationMs, 0.001)),
		statuses,
		errors,
		latencyMs: latencyDistribution(samples.map(({ latencyMs }) => latencyMs)),
		successfulLatencyMs: latencyDistribution(
			samples.filter(({ success }) => success).map(({ latencyMs }) => latencyMs)
		),
		responseBytes: {
			total: totalBytes,
			average: round(totalBytes / Math.max(samples.length, 1)),
		},
	};
}

async function requestJSON(spec: RequestSpec): Promise<RequestObservation> {
	const started = performance.now();
	const controller = new AbortController();
	let timedOut = false;
	const timer = setTimeout(() => {
		timedOut = true;
		controller.abort(new DOMException("request timed out", "TimeoutError"));
	}, requestTimeoutMs);
	try {
		const response = await fetch(spec.url, {
			...spec.init,
			signal: controller.signal,
		});
		const text = await response.text();
		const latencyMs = performance.now() - started;
		const bytes = Buffer.byteLength(text);
		let body: unknown;
		try {
			body = JSON.parse(text) as unknown;
		} catch {
			return {
				sample: {
					operation: spec.operation,
					latencyMs: round(latencyMs),
					status: response.status,
					bytes,
					success: false,
					timeout: false,
					jsonValid: false,
					shapeValid: false,
					error: "invalid-json",
					detail: text.slice(0, 300),
				},
			};
		}
		if (!spec.expectedStatuses.includes(response.status)) {
			return {
				body,
				sample: {
					operation: spec.operation,
					latencyMs: round(latencyMs),
					status: response.status,
					bytes,
					success: false,
					timeout: false,
					jsonValid: true,
					shapeValid: false,
					error: "unexpected-status",
					detail: text.slice(0, 300),
				},
			};
		}
		const shapeError = spec.validate(body);
		return {
			body,
			sample: {
				operation: spec.operation,
				latencyMs: round(latencyMs),
				status: response.status,
				bytes,
				success: shapeError === null,
				timeout: false,
				jsonValid: true,
				shapeValid: shapeError === null,
				...(shapeError === null ? {} : { error: "invalid-success-shape", detail: shapeError }),
			},
		};
	} catch (error) {
		const timeout = timedOut || isTimeout(error);
		return {
			sample: {
				operation: spec.operation,
				latencyMs: round(performance.now() - started),
				bytes: 0,
				success: false,
				timeout,
				jsonValid: false,
				shapeValid: false,
				error: timeout ? "timeout" : "network-error",
				detail: errorText(error).slice(0, 300),
			},
		};
	} finally {
		clearTimeout(timer);
	}
}

async function safeObservation(
	work: () => Promise<RequestObservation>
): Promise<RequestObservation> {
	try {
		return await work();
	} catch (error) {
		return {
			sample: {
				operation: "harness-error",
				latencyMs: 0,
				bytes: 0,
				success: false,
				timeout: false,
				jsonValid: false,
				shapeValid: false,
				error: "harness-error",
				detail: errorText(error).slice(0, 300),
			},
		};
	}
}

function startResourceSampler(pid: number, database: BenchmarkDatabase) {
	const started = performance.now();
	const applicationSamples: ApplicationResourceSample[] = [];
	const postgresSamples: PostgresResourceSample[] = [];
	let running = true;
	const applicationLoop = (async () => {
		while (running) {
			const sampleStarted = performance.now();
			try {
				applicationSamples.push({
					elapsedMs: round(sampleStarted - started),
					...(await processTreeMetrics(pid)),
				});
			} catch (error) {
				applicationSamples.push({
					elapsedMs: round(sampleStarted - started),
					error: errorText(error),
				});
			}
			await delay(Math.max(0, 100 - (performance.now() - sampleStarted)));
		}
	})();
	const postgresLoop = (async () => {
		while (running) {
			const sampleStarted = performance.now();
			try {
				postgresSamples.push({
					elapsedMs: round(sampleStarted - started),
					...(await postgresMetrics(database)),
				});
			} catch (error) {
				postgresSamples.push({
					elapsedMs: round(sampleStarted - started),
					error: errorText(error),
				});
			}
			await delay(Math.max(0, 1_000 - (performance.now() - sampleStarted)));
		}
	})();

	return {
		stop: async (): Promise<ResourceSeries> => {
			running = false;
			await Promise.all([applicationLoop, postgresLoop]);
			return resourceSeries(applicationSamples, postgresSamples);
		},
	};
}

function resourceSeries(
	applicationSamples: ApplicationResourceSample[],
	postgresSamples: PostgresResourceSample[]
): ResourceSeries {
	return {
		applicationSamples,
		postgresSamples,
		summary: {
			application: {
				rssMiB: numericDistribution(
					applicationSamples.flatMap(({ rssMiB }) => (rssMiB === undefined ? [] : [rssMiB]))
				),
				cpuPercent: numericDistribution(
					applicationSamples.flatMap(({ cpuPercent }) =>
						cpuPercent === undefined ? [] : [cpuPercent]
					)
				),
				processes: numericDistribution(
					applicationSamples.flatMap(({ processes }) =>
						processes === undefined ? [] : [processes]
					)
				),
			},
			postgres: {
				total: numericDistribution(
					postgresSamples.flatMap(({ total }) => (total === undefined ? [] : [total]))
				),
				active: numericDistribution(
					postgresSamples.flatMap(({ active }) => (active === undefined ? [] : [active]))
				),
				idle: numericDistribution(
					postgresSamples.flatMap(({ idle }) => (idle === undefined ? [] : [idle]))
				),
				idleInTransaction: numericDistribution(
					postgresSamples.flatMap(({ idleInTransaction }) =>
						idleInTransaction === undefined ? [] : [idleInTransaction]
					)
				),
				waiting: numericDistribution(
					postgresSamples.flatMap(({ waiting }) => (waiting === undefined ? [] : [waiting]))
				),
			},
		},
	};
}

async function processTreeMetrics(
	rootPID: number
): Promise<{ rssMiB: number; cpuPercent: number; processes: number }> {
	const process = Bun.spawn(["ps", "-axo", "pid=,ppid=,rss=,%cpu="], {
		stdout: "pipe",
		stderr: "pipe",
	});
	const [output, error, exitCode] = await Promise.all([
		new Response(process.stdout).text(),
		new Response(process.stderr).text(),
		process.exited,
	]);
	if (exitCode !== 0) throw new Error(`ps failed: ${error.trim()}`);
	const rows = output
		.split("\n")
		.map((line) => line.trim().split(/\s+/).map(Number))
		.filter((row) => row.length === 4 && row.every(Number.isFinite));
	const children = new Map<number, number[]>();
	const resident = new Map<number, number>();
	const cpu = new Map<number, number>();
	for (const [pid, parent, rss, percent] of rows) {
		resident.set(pid!, rss!);
		cpu.set(pid!, percent!);
		children.set(parent!, [...(children.get(parent!) ?? []), pid!]);
	}
	if (!resident.has(rootPID)) throw new Error(`server process ${rootPID} is not running`);
	let rssKiB = 0;
	let cpuPercent = 0;
	const visited = new Set<number>();
	const stack = [rootPID];
	while (stack.length > 0) {
		const pid = stack.pop()!;
		if (visited.has(pid)) continue;
		visited.add(pid);
		rssKiB += resident.get(pid) ?? 0;
		cpuPercent += cpu.get(pid) ?? 0;
		stack.push(...(children.get(pid) ?? []));
	}
	return { rssMiB: round(rssKiB / 1_024), cpuPercent: round(cpuPercent), processes: visited.size };
}

async function postgresMetrics(database: BenchmarkDatabase): Promise<{
	total: number;
	active: number;
	idle: number;
	idleInTransaction: number;
	waiting: number;
	states: Record<string, number>;
}> {
	assertBenchmarkDatabase(database);
	const escapedDatabase = database.replaceAll("'", "''");
	const query =
		"SELECT COALESCE(state, 'unknown'), count(*), " +
		"count(*) FILTER (WHERE wait_event IS NOT NULL) " +
		`FROM pg_stat_activity WHERE datname = '${escapedDatabase}' GROUP BY state ORDER BY state`;
	const process = Bun.spawn(["psql", "-AtF", "\t", "-c", query], {
		env: { ...Bun.env, ...postgresEnvironment("postgres") },
		stdout: "pipe",
		stderr: "pipe",
	});
	const [output, error, exitCode] = await Promise.all([
		new Response(process.stdout).text(),
		new Response(process.stderr).text(),
		process.exited,
	]);
	if (exitCode !== 0) throw new Error(`psql pg_stat_activity failed: ${error.trim()}`);
	const states: Record<string, number> = {};
	let waiting = 0;
	for (const line of output.trim().split("\n")) {
		if (line === "") continue;
		const [state, countText, waitingText] = line.split("\t");
		const count = Number(countText);
		const waitingCount = Number(waitingText);
		if (state === undefined || !Number.isFinite(count) || !Number.isFinite(waitingCount)) {
			throw new Error(`unexpected pg_stat_activity row: ${line}`);
		}
		states[state] = count;
		waiting += waitingCount;
	}
	const total = Object.values(states).reduce((sum, count) => sum + count, 0);
	return {
		total,
		active: states.active ?? 0,
		idle: states.idle ?? 0,
		idleInTransaction:
			(states["idle in transaction"] ?? 0) + (states["idle in transaction (aborted)"] ?? 0),
		waiting,
		states,
	};
}

async function sampleIdle(
	name: string,
	pid: number,
	database: BenchmarkDatabase,
	durationMs: number
): Promise<Record<string, unknown>> {
	throwIfInterrupted();
	console.log(`${name}: sampling ${round(durationMs / 1_000)}s`);
	const sampler = startResourceSampler(pid, database);
	const started = performance.now();
	await interruptibleDelay(durationMs);
	const resource = await sampler.stop();
	throwIfInterrupted();
	return {
		name,
		durationMs: round(performance.now() - started),
		resource,
	};
}

async function sampleRecovery(
	framework: Framework,
	pid: number,
	database: BenchmarkDatabase,
	durationMs: number
): Promise<Record<string, unknown>> {
	throwIfInterrupted();
	console.log(`recovery: sampling ${round(durationMs / 1_000)}s`);
	const sampler = startResourceSampler(pid, database);
	const started = performance.now();
	const healthChecks: HealthCheck[] = [];
	while (interruptedSignal === undefined && performance.now() - started < durationMs) {
		healthChecks.push(await healthCheck(framework, "recovery"));
		await delay(Math.min(1_000, Math.max(0, durationMs - (performance.now() - started))));
	}
	const resource = await sampler.stop();
	throwIfInterrupted();
	return {
		name: "recovery",
		durationMs: round(performance.now() - started),
		healthChecks,
		resource,
	};
}

async function warm(
	framework: Framework,
	adminCookie: string,
	contributorCookie: string,
	publishedIDs: string[],
	references: SeedReferences,
	expected: SemanticCounts
): Promise<void> {
	await parallelMap(200, Math.min(matchedConcurrency, 16), async (index) => {
		const id = publishedIDs[index % publishedIDs.length]!;
		const spec =
			index % 4 === 0
				? {
						operation: "warm-list",
						url: publicListURL(framework),
						expectedStatuses: [200],
						validate: selectedListValidator(expected.public),
					}
				: index % 4 === 1
					? {
							operation: "warm-find",
							url: findURL(framework, id, false),
							expectedStatuses: [200],
							validate: selectedDocumentValidator(id),
						}
					: index % 4 === 2
						? {
								operation: "warm-populated",
								url: findURL(framework, id, true),
								init: { headers: { Cookie: adminCookie } },
								expectedStatuses: [200],
								validate: populatedDocumentValidator(id, references),
							}
						: {
								operation: "warm-access",
								url: accessFilteredListURL(framework),
								init: { headers: { Cookie: contributorCookie } },
								expectedStatuses: [200],
								validate: listValidator(expected.contributor),
							};
		await requiredJSON(spec);
	});
}

async function login(framework: Framework, email: string, password: string): Promise<string> {
	const path = framework.name === "ridu" ? "/api/auth/users/login" : "/api/users/login";
	const response = await fetch(`${framework.baseURL}${path}`, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ email, password }),
		signal: AbortSignal.timeout(requestTimeoutMs),
	});
	const text = await response.text();
	if (response.status !== 200) {
		throw new Error(`${framework.name} login for ${email} returned ${response.status}: ${text}`);
	}
	JSON.parse(text);
	const cookie = response.headers.get("set-cookie")?.split(";", 1)[0];
	if (cookie === undefined || cookie === "") {
		throw new Error(`${framework.name} login for ${email} did not set a cookie`);
	}
	return cookie;
}

async function fetchSeedReferences(
	framework: Framework,
	adminCookie: string
): Promise<SeedReferences> {
	const users = await authenticatedDocuments(framework, adminCookie, "users");
	const categories = await authenticatedDocuments(framework, adminCookie, "categories");
	const posts = await authenticatedDocuments(framework, adminCookie, "posts");
	const author =
		users.find((document) => document.role === "editor") ??
		users.find((document) => document.email === "editor@riducms.test");
	const contributor =
		users.find((document) => document.role === "contributor") ??
		users.find((document) => document.email === "demo@riducms.local");
	const category = categories.find((document) => document.slug === "news") ?? categories.at(0);
	const related =
		posts.find((document) => document.status === "published" || document._status === "published") ??
		posts.at(0);
	if (
		author === undefined ||
		contributor === undefined ||
		category === undefined ||
		related === undefined
	) {
		throw new Error(`${framework.name} authenticated APIs omitted required seed relationships`);
	}
	return {
		authorID: String(author.id),
		contributorID: String(contributor.id),
		categoryID: String(category.id),
		relatedPostID: String(related.id),
	};
}

async function authenticatedDocuments(
	framework: Framework,
	cookie: string,
	collection: "users" | "categories" | "posts"
): Promise<Record<string, unknown>[]> {
	const path =
		framework.name === "ridu"
			? `/api/collections/${collection}?limit=100`
			: `/api/${collection}?limit=100&depth=0`;
	const body = await requiredJSON({
		operation: `seed-${collection}`,
		url: `${framework.baseURL}${path}`,
		init: { headers: { Cookie: cookie } },
		expectedStatuses: [200],
		validate: listValidator(),
	});
	if (!isRecord(body) || !Array.isArray(body.docs)) return [];
	return body.docs.filter(isRecord);
}

async function createPublishedPost(
	framework: Framework,
	cookie: string,
	references: SeedReferences,
	index: number
): Promise<string> {
	const data = postData(framework, references, `dataset-${index}`, true);
	const validate = postDocumentValidator(references, data);
	const path =
		framework.name === "ridu" ? "/api/collections/posts" : "/api/posts?draft=false&depth=0";
	const body = await requiredJSON({
		operation: "seed-create-published",
		url: `${framework.baseURL}${path}`,
		init: {
			method: "POST",
			headers: { "Content-Type": "application/json", Cookie: cookie },
			body: JSON.stringify(data),
		},
		expectedStatuses: [201],
		validate,
	});
	const document = extractDocument(body);
	if (document === null) throw new Error(`${framework.name} seed create omitted a document`);
	const id = String(document.id);
	if (framework.name === "ridu") {
		await requiredJSON({
			operation: "seed-publish",
			url: `${framework.baseURL}/api/collections/posts/${encodeURIComponent(id)}/publish`,
			init: { method: "POST", headers: { Cookie: cookie } },
			expectedStatuses: [200],
			validate: postDocumentValidator(references, data, id),
		});
	}
	return id;
}

function createDraftSpec(
	framework: Framework,
	cookie: string,
	references: SeedReferences,
	key: string
): RequestSpec {
	const data = postData(framework, references, key, false);
	const path =
		framework.name === "ridu"
			? "/api/collections/posts?draft=true"
			: "/api/posts?draft=true&depth=0";
	return {
		operation: "create-draft",
		url: `${framework.baseURL}${path}`,
		init: {
			method: "POST",
			headers: { "Content-Type": "application/json", Cookie: cookie },
			body: JSON.stringify(data),
		},
		expectedStatuses: [201],
		validate: postDocumentValidator(references, data),
	};
}

function updateDraftSpec(
	framework: Framework,
	cookie: string,
	id: string,
	summary: string
): RequestSpec {
	const path =
		framework.name === "ridu"
			? `/api/collections/posts/${encodeURIComponent(id)}?draft=true`
			: `/api/posts/${encodeURIComponent(id)}?draft=true&depth=0`;
	return {
		operation: "update-draft",
		url: `${framework.baseURL}${path}`,
		init: {
			method: "PATCH",
			headers: { "Content-Type": "application/json", Cookie: cookie },
			body: JSON.stringify({ summary }),
		},
		expectedStatuses: [200],
		validate: documentValidator(id, { summary }),
	};
}

function deleteDraftSpec(framework: Framework, cookie: string, id: string): RequestSpec {
	const path =
		framework.name === "ridu"
			? `/api/collections/posts/${encodeURIComponent(id)}`
			: `/api/posts/${encodeURIComponent(id)}?depth=0`;
	return {
		operation: "delete-draft",
		url: `${framework.baseURL}${path}`,
		init: { method: "DELETE", headers: { Cookie: cookie } },
		expectedStatuses: [200],
		validate: documentValidator(id),
	};
}

function postData(
	framework: Framework,
	references: SeedReferences,
	key: string,
	published: boolean
): Record<string, unknown> {
	const normalized = key.replaceAll(/[^a-zA-Z0-9-]/g, "-");
	const author = relationshipWireID(framework, references.authorID);
	const category = relationshipWireID(framework, references.categoryID);
	const relatedPost = relationshipWireID(framework, references.relatedPostID);
	const data: Record<string, unknown> = {
		title: `Stress post ${key}`,
		slug: `stress-${framework.name}-${normalized}`,
		summary: `Relationship-bearing production stress document ${key}`,
		status: published ? "published" : "draft",
		author,
		category,
		collaborators: [author],
		relatedPosts: [relatedPost],
		readingMinutes: (key.length % 12) + 1,
		featured: key.length % 5 === 0,
	};
	if (framework.name === "payload") data._status = published ? "published" : "draft";
	return data;
}

function relationshipWireID(framework: Framework, id: string): string | number {
	if (framework.name === "ridu") return id;
	const numeric = Number(id);
	if (!Number.isSafeInteger(numeric)) {
		throw new Error(`Payload relationship ID ${id} is not a safe integer`);
	}
	return numeric;
}

async function createWorkerDrafts(
	framework: Framework,
	cookie: string,
	references: SeedReferences,
	prefix: string,
	count: number
): Promise<string[]> {
	const created: string[] = [];
	try {
		return await parallelMap(count, Math.min(count, 16), async (index) => {
			const body = await requiredJSON(
				createDraftSpec(framework, cookie, references, `${prefix}-${index}`)
			);
			const document = extractDocument(body);
			if (document === null) throw new Error(`${framework.name} worker draft omitted a document`);
			const id = String(document.id);
			created.push(id);
			return id;
		});
	} catch (error) {
		await deleteDraftsBestEffort(framework, cookie, created);
		throw error;
	}
}

async function deleteDraftsBestEffort(
	framework: Framework,
	cookie: string,
	ids: string[]
): Promise<Record<string, unknown>> {
	let deleted = 0;
	let alreadyAbsent = 0;
	const errors: string[] = [];
	await parallelMap(ids.length, Math.min(ids.length, 16), async (index) => {
		const observation = await requestJSON(deleteDraftSpec(framework, cookie, ids[index]!));
		if (observation.sample.success) {
			deleted += 1;
		} else if (observation.sample.status === 404) {
			alreadyAbsent += 1;
		} else {
			errors.push(
				`${ids[index]}: ${observation.sample.error ?? "request failed"} ${observation.sample.detail ?? ""}`.trim()
			);
		}
	});
	return { requested: ids.length, deleted, alreadyAbsent, errors };
}

function publicListURL(framework: Framework): string {
	if (framework.name === "ridu") {
		const query = new URLSearchParams({
			limit: "10",
			select: JSON.stringify({ title: true, summary: true, status: true }),
		});
		return `${framework.baseURL}/api/collections/posts?${query}`;
	}
	return `${framework.baseURL}/api/posts?limit=10&depth=0&select[title]=true&select[summary]=true&select[status]=true`;
}

function accessFilteredListURL(framework: Framework): string {
	if (framework.name === "ridu") {
		const query = new URLSearchParams({
			limit: "10",
			select: JSON.stringify({ title: true, status: true, author: true }),
		});
		return `${framework.baseURL}/api/collections/posts?${query}`;
	}
	return `${framework.baseURL}/api/posts?limit=10&depth=0&select[title]=true&select[status]=true&select[author]=true`;
}

function findURL(framework: Framework, id: string, populated: boolean): string {
	if (framework.name === "ridu") {
		if (populated) {
			return `${framework.baseURL}/api/collections/posts/${encodeURIComponent(id)}?depth=1`;
		}
		const query = new URLSearchParams({
			select: JSON.stringify({ title: true, summary: true, status: true }),
		});
		return `${framework.baseURL}/api/collections/posts/${encodeURIComponent(id)}?${query}`;
	}
	return populated
		? `${framework.baseURL}/api/posts/${encodeURIComponent(id)}?depth=1`
		: `${framework.baseURL}/api/posts/${encodeURIComponent(id)}?depth=0&select[title]=true&select[summary]=true&select[status]=true`;
}

function listValidator(expectedTotal?: number): Validation {
	return (body) => {
		if (!isRecord(body) || !Array.isArray(body.docs)) return "response must contain docs[]";
		const total = listTotal(body);
		if (total === undefined) return "response must contain a numeric total document count";
		if (expectedTotal !== undefined && total !== expectedTotal) {
			return `total document count ${total} did not equal ${expectedTotal}`;
		}
		if (!body.docs.every((document) => isRecord(document) && document.id !== undefined)) {
			return "every list document must contain id";
		}
		return null;
	};
}

function selectedListValidator(expectedTotal: number): Validation {
	return (body) => {
		const listError = listValidator(expectedTotal)(body);
		if (listError !== null) return listError;
		if (!isRecord(body) || !Array.isArray(body.docs)) return "response must contain docs[]";
		for (const candidate of body.docs) {
			if (!isRecord(candidate)) return "selected list document must be an object";
			if (typeof candidate.title !== "string") return "selected list document must contain title";
			if (typeof candidate.summary !== "string") {
				return "selected list document must contain summary";
			}
			if (typeof (candidate.status ?? candidate._status) !== "string") {
				return "selected list document must contain status";
			}
		}
		return null;
	};
}

function accessFilteredListValidator(expectedTotal: number, contributorID: string): Validation {
	return (body) => {
		const listError = listValidator(expectedTotal)(body);
		if (listError !== null) return listError;
		if (!isRecord(body) || !Array.isArray(body.docs)) return "response must contain docs[]";
		for (const candidate of body.docs) {
			if (!isRecord(candidate)) return "access-filtered document must be an object";
			if (typeof candidate.title !== "string") {
				return "access-filtered document must contain title";
			}
			const status = candidate.status ?? candidate._status;
			const author = relationshipID(candidate.author);
			if (status !== "published" && author !== contributorID) {
				return `access-filtered response exposed non-owned draft ${String(candidate.id)}`;
			}
		}
		return null;
	};
}

function documentValidator(
	expectedID?: string,
	expectedFields: Record<string, string | number | boolean> = {}
): Validation {
	return (body) => {
		const document = extractDocument(body);
		if (document === null) return "response must contain a document with id";
		if (expectedID !== undefined && String(document.id) !== expectedID) {
			return `document id ${String(document.id)} did not equal ${expectedID}`;
		}
		for (const [field, expected] of Object.entries(expectedFields)) {
			const actual = field === "status" ? (document.status ?? document._status) : document[field];
			if (actual !== expected) {
				return `document ${field} ${JSON.stringify(actual)} did not equal ${JSON.stringify(expected)}`;
			}
		}
		return null;
	};
}

function selectedDocumentValidator(expectedID: string): Validation {
	return (body) => {
		const documentError = documentValidator(expectedID)(body);
		if (documentError !== null) return documentError;
		const document = extractDocument(body)!;
		if (typeof document.title !== "string") return "selected document must contain title";
		if (typeof document.summary !== "string") return "selected document must contain summary";
		if (typeof (document.status ?? document._status) !== "string") {
			return "selected document must contain status";
		}
		return null;
	};
}

function postDocumentValidator(
	references: SeedReferences,
	expected: Record<string, unknown>,
	expectedID?: string
): Validation {
	return (body) => {
		const scalarError = documentValidator(expectedID, {
			title: String(expected.title),
			summary: String(expected.summary),
			status: String(expected.status),
			readingMinutes: Number(expected.readingMinutes),
			featured: Boolean(expected.featured),
		})(body);
		if (scalarError !== null) return scalarError;
		const document = extractDocument(body)!;
		if (relationshipID(document.author) !== references.authorID) {
			return "document author did not equal the expected relationship";
		}
		if (relationshipID(document.category) !== references.categoryID) {
			return "document category did not equal the expected relationship";
		}
		if (!relationshipIDs(document.collaborators).includes(references.authorID)) {
			return "document collaborators omitted the expected relationship";
		}
		if (!relationshipIDs(document.relatedPosts).includes(references.relatedPostID)) {
			return "document relatedPosts omitted the expected relationship";
		}
		return null;
	};
}

function populatedDocumentValidator(expectedID: string, references: SeedReferences): Validation {
	return (body) => {
		const documentError = documentValidator(expectedID)(body);
		if (documentError !== null) return documentError;
		const document = extractDocument(body)!;
		if (
			!isRecord(document.author) ||
			document.author.id === undefined ||
			String(document.author.id) !== references.authorID
		) {
			return "populated author must be a document";
		}
		if (
			!isRecord(document.category) ||
			document.category.id === undefined ||
			String(document.category.id) !== references.categoryID
		) {
			return "populated category must be a document";
		}
		if (
			!Array.isArray(document.relatedPosts) ||
			!document.relatedPosts.some(
				(related) => isRecord(related) && String(related.id) === references.relatedPostID
			)
		) {
			return "populated relatedPosts must contain the expected document";
		}
		if (
			!Array.isArray(document.collaborators) ||
			!document.collaborators.some(
				(collaborator) => isRecord(collaborator) && String(collaborator.id) === references.authorID
			)
		) {
			return "populated collaborators must contain the expected document";
		}
		return null;
	};
}

function extractDocument(body: unknown): Record<string, unknown> | null {
	if (!isRecord(body)) return null;
	if (isRecord(body.doc) && body.doc.id !== undefined) return body.doc;
	if (body.id !== undefined) return body;
	return null;
}

function listTotal(body: Record<string, unknown>): number | undefined {
	if (typeof body.totalDocs === "number") return body.totalDocs;
	if (isRecord(body.pagination) && typeof body.pagination.totalDocs === "number") {
		return body.pagination.totalDocs;
	}
	return undefined;
}

function relationshipID(value: unknown): string | undefined {
	if (typeof value === "string" || typeof value === "number") return String(value);
	if (isRecord(value) && value.id !== undefined) return String(value.id);
	return undefined;
}

function relationshipIDs(value: unknown): string[] {
	if (!Array.isArray(value)) return [];
	return value.flatMap((candidate) => {
		const id = relationshipID(candidate);
		return id === undefined ? [] : [id];
	});
}

async function requiredJSON(spec: RequestSpec): Promise<unknown> {
	const observation = await requestJSON(spec);
	if (!observation.sample.success) {
		throw new Error(
			`${spec.operation} failed: ${observation.sample.error ?? "unknown error"}${observation.sample.status === undefined ? "" : ` (${observation.sample.status})`}: ${observation.sample.detail ?? ""}`
		);
	}
	return observation.body;
}

async function semanticCounts(
	framework: Framework,
	adminCookie: string,
	contributorCookie: string
): Promise<SemanticCounts> {
	const [admin, publicCount, contributor] = await Promise.all([
		postCount(framework, adminCookie),
		postCount(framework),
		postCount(framework, contributorCookie),
	]);
	return { admin, public: publicCount, contributor };
}

async function postCount(framework: Framework, cookie?: string): Promise<number> {
	const path =
		framework.name === "ridu" ? "/api/collections/posts?limit=1" : "/api/posts?limit=1&depth=0";
	const body = await requiredJSON({
		operation: "semantic-count",
		url: `${framework.baseURL}${path}`,
		...(cookie === undefined ? {} : { init: { headers: { Cookie: cookie } } }),
		expectedStatuses: [200],
		validate: listValidator(),
	});
	return listTotal(body as Record<string, unknown>)!;
}

function assertSemanticCounts(
	framework: FrameworkName,
	label: string,
	actual: SemanticCounts,
	expected: SemanticCounts
): void {
	for (const audience of ["admin", "public", "contributor"] as const) {
		if (actual[audience] !== expected[audience]) {
			throw new Error(
				`${framework} ${label} ${audience} count was ${actual[audience]}; expected ${expected[audience]}`
			);
		}
	}
}

async function reconcileCounts(
	framework: Framework,
	adminCookie: string,
	contributorCookie: string,
	expected: SemanticCounts
): Promise<Record<string, unknown>> {
	try {
		const actual = await semanticCounts(framework, adminCookie, contributorCookie);
		return {
			actual,
			expected,
			valid:
				actual.admin === expected.admin &&
				actual.public === expected.public &&
				actual.contributor === expected.contributor,
		};
	} catch (error) {
		return { expected, valid: false, error: errorText(error) };
	}
}

async function recordHealth(
	framework: Framework,
	result: Record<string, unknown>,
	label: string
): Promise<HealthCheck> {
	const check = await healthCheck(framework, label);
	(result.healthChecks as HealthCheck[]).push(check);
	if (!check.ok) (result.errors as string[]).push(`${label}: ${check.error ?? check.status}`);
	return check;
}

async function healthCheck(framework: Framework, label: string): Promise<HealthCheck> {
	const started = performance.now();
	try {
		const response = await fetch(`${framework.baseURL}${framework.readyPath}`, {
			signal: AbortSignal.timeout(Math.min(requestTimeoutMs, 5_000)),
		});
		await response.arrayBuffer();
		return {
			label,
			at: new Date().toISOString(),
			latencyMs: round(performance.now() - started),
			status: response.status,
			ok: response.status === 200,
			...(response.status === 200 ? {} : { error: `health returned ${response.status}` }),
		};
	} catch (error) {
		return {
			label,
			at: new Date().toISOString(),
			latencyMs: round(performance.now() - started),
			ok: false,
			error: errorText(error),
		};
	}
}

async function waitUntilReady(
	framework: Framework,
	process: Bun.Subprocess,
	timeoutMs: number
): Promise<void> {
	const deadline = Date.now() + timeoutMs;
	let lastError = "not ready";
	while (Date.now() < deadline) {
		if (process.exitCode !== null) {
			throw new Error(`${framework.name} server exited with ${process.exitCode}: ${lastError}`);
		}
		const check = await healthCheck(framework, "startup");
		if (check.ok) return;
		lastError = check.error ?? `status ${check.status}`;
		await delay(250);
	}
	throw new Error(`${framework.name} timed out during startup: ${lastError}`);
}

async function terminateProcessTree(process: Bun.Subprocess): Promise<void> {
	if (process.exitCode !== null) return;
	const descendants = await descendantPIDs(process.pid);
	for (const pid of descendants.reverse()) safeKill(pid, "SIGTERM");
	process.kill("SIGTERM");
	await Promise.race([process.exited, delay(10_000)]);
	if (process.exitCode !== null) return;
	for (const pid of await descendantPIDs(process.pid)) safeKill(pid, "SIGKILL");
	process.kill("SIGKILL");
	await Promise.race([process.exited, delay(5_000)]);
}

async function descendantPIDs(rootPID: number): Promise<number[]> {
	const process = Bun.spawn(["ps", "-axo", "pid=,ppid="], {
		stdout: "pipe",
		stderr: "ignore",
	});
	const output = await new Response(process.stdout).text();
	await process.exited;
	const children = new Map<number, number[]>();
	for (const line of output.split("\n")) {
		const [pid, parent] = line.trim().split(/\s+/).map(Number);
		if (!Number.isFinite(pid) || !Number.isFinite(parent)) continue;
		children.set(parent!, [...(children.get(parent!) ?? []), pid!]);
	}
	const descendants: number[] = [];
	const stack = [...(children.get(rootPID) ?? [])];
	while (stack.length > 0) {
		const pid = stack.pop()!;
		descendants.push(pid);
		stack.push(...(children.get(pid) ?? []));
	}
	return descendants;
}

function safeKill(pid: number, signal: NodeJS.Signals): void {
	try {
		process.kill(pid, signal);
	} catch {
		// The process may have exited between process-tree discovery and signalling.
	}
}

function migratePayload(database: BenchmarkDatabase): void {
	if (database !== "payload_stress_benchmark") {
		throw new Error(`refusing to migrate unexpected Payload database ${database}`);
	}
	runCommand([join(payloadDirectory, "node_modules/.bin/payload"), "migrate"], payloadDirectory, {
		PAYLOAD_DATABASE_URL: databaseURL(database),
		PAYLOAD_SECRET: payloadSecret,
	});
}

function recreateDatabase(database: BenchmarkDatabase): void {
	assertBenchmarkDatabase(database);
	runCommand(["dropdb", "--if-exists", "--force", database], repository, postgresEnvironment());
	runCommand(["createdb", database], repository, postgresEnvironment());
}

function assertBenchmarkDatabase(database: string): asserts database is BenchmarkDatabase {
	if (!allowedDatabases.has(database as BenchmarkDatabase)) {
		throw new Error(
			`refusing destructive database operation outside benchmark allowlist: ${database}`
		);
	}
}

function postgresEnvironment(database = "postgres"): Record<string, string> {
	return {
		PGHOST: postgresConnection.hostname,
		PGPORT: postgresConnection.port || "5432",
		PGUSER: decodeURIComponent(postgresConnection.username),
		PGPASSWORD: decodeURIComponent(postgresConnection.password),
		PGDATABASE: database,
		PGSSLMODE: postgresConnection.searchParams.get("sslmode") ?? "prefer",
	};
}

function assertPortsAvailable(ports: number[]): void {
	for (const port of ports) {
		const result = Bun.spawnSync(["lsof", "-nP", `-iTCP:${port}`, "-sTCP:LISTEN", "-t"], {
			stdout: "pipe",
			stderr: "pipe",
		});
		const owner = result.stdout.toString().trim();
		if (owner !== "") {
			throw new Error(`stress benchmark port ${port} is already owned by process ${owner}`);
		}
	}
}

function runCommand(
	command: string[],
	cwd: string,
	environment: Record<string, string> = {}
): void {
	const result = Bun.spawnSync(command, {
		cwd,
		env: { ...Bun.env, ...environment },
		stdout: "inherit",
		stderr: "inherit",
	});
	if (result.exitCode !== 0) throw new Error(`${command.join(" ")} exited with ${result.exitCode}`);
}

async function environmentReport(): Promise<Record<string, unknown>> {
	const status = commandOutputMaybe("git", ["status", "--porcelain=v1", "--untracked-files=all"]);
	return {
		hostname: hostname(),
		platform: commandOutputMaybe("sw_vers", ["-productVersion"]),
		kernel: commandOutputMaybe("uname", ["-srvmp"]),
		architecture: process.arch,
		cpu: commandOutputMaybe("sysctl", ["-n", "machdep.cpu.brand_string"]),
		logicalCPUs: numberOutputMaybe("sysctl", ["-n", "hw.logicalcpu"]),
		memoryBytes: numberOutputMaybe("sysctl", ["-n", "hw.memsize"]),
		postgres: commandOutputMaybe("psql", ["-Atqc", "show server_version"], undefined, {
			...postgresEnvironment("postgres"),
		}),
		go: commandOutputMaybe("go", ["version"]),
		bun: commandOutputMaybe("bun", ["--version"]),
		node: commandOutputMaybe("node", ["--version"]),
		riduRevision: commandOutputMaybe("git", ["rev-parse", "HEAD"]),
		git: {
			dirty: status !== "",
			status: status === "" ? [] : status.split("\n"),
			diffStat: commandOutputMaybe("git", ["diff", "--stat", "HEAD"]),
		},
		payloadVersion: packageVersion(join(payloadDirectory, "node_modules/payload/package.json")),
		nextVersion: packageVersion(join(payloadDirectory, "node_modules/next/package.json")),
	};
}

async function artifactReport(): Promise<Record<string, unknown>> {
	const artifacts: Record<string, unknown> = {
		harness: await fileArtifact(harnessPath),
	};
	if (selectedFrameworks.includes("ridu")) {
		artifacts.riduBinary = await fileArtifact(riduBinary);
		artifacts.riduAdmin = await treeArtifact(riduAdminDirectory);
	}
	if (selectedFrameworks.includes("payload")) {
		artifacts.payloadServer = await fileArtifact(payloadServer);
		artifacts.payloadStandalone = await treeArtifact(payloadStandaloneDirectory);
		artifacts.payloadStaticSource = await treeArtifact(payloadStaticSource);
		artifacts.payloadStaticDestination = await treeArtifact(payloadStaticDestination);
	}
	return artifacts;
}

function inheritedServerTuning(): Record<string, string> {
	const tuning: Record<string, string> = {};
	for (const name of ["PAYLOAD_POOL_MAX", "RIDU_POSTGRES_UPLOAD_LOCK_CONNECTIONS"] as const) {
		const value = Bun.env[name];
		if (value !== undefined && value !== "") tuning[name] = value;
	}
	return tuning;
}

async function fileArtifact(path: string): Promise<Artifact> {
	const stat = statSync(path);
	return {
		path,
		bytes: stat.size,
		sha256: await hashFile(path),
		modifiedAt: stat.mtime.toISOString(),
	};
}

async function treeArtifact(root: string): Promise<TreeArtifact> {
	const paths = walkFiles(root);
	let bytes = 0;
	const hash = createHash("sha256");
	for (const path of paths) {
		const entry = lstatSync(path);
		const relativePath = relative(root, path);
		hash.update(relativePath);
		hash.update("\0");
		if (entry.isSymbolicLink()) {
			const target = readlinkSync(path);
			bytes += Buffer.byteLength(target);
			hash.update("symlink\0");
			hash.update(target);
		} else {
			bytes += entry.size;
			hash.update(String(entry.size));
			hash.update("\0");
			hash.update(await hashFile(path));
		}
		hash.update("\0");
	}
	return { path: root, files: paths.length, bytes, sha256: hash.digest("hex") };
}

function walkFiles(root: string): string[] {
	const paths: string[] = [];
	for (const entry of readdirSync(root, { withFileTypes: true })) {
		const path = join(root, entry.name);
		if (entry.isDirectory()) paths.push(...walkFiles(path));
		else if (entry.isFile() || entry.isSymbolicLink()) paths.push(path);
	}
	return paths.sort();
}

async function hashFile(path: string): Promise<string> {
	const hash = createHash("sha256");
	for await (const chunk of createReadStream(path)) hash.update(chunk);
	return hash.digest("hex");
}

function packageVersion(path: string): string {
	try {
		const parsed = JSON.parse(readFileSync(path, "utf8")) as {
			version?: string;
		};
		return parsed.version ?? "unknown";
	} catch {
		return "unknown";
	}
}

function commandOutputMaybe(
	command: string,
	args: string[],
	cwd = repository,
	environment?: Record<string, string>
): string {
	try {
		return execFileSync(command, args, {
			cwd,
			env: { ...process.env, ...environment },
			encoding: "utf8",
			stdio: ["ignore", "pipe", "pipe"],
		}).trim();
	} catch (error) {
		return `unavailable: ${errorText(error)}`;
	}
}

function numberOutputMaybe(command: string, args: string[]): number | string {
	const output = commandOutputMaybe(command, args);
	const value = Number(output);
	return Number.isFinite(value) ? value : output;
}

function redactDatabaseURL(template: string): string {
	const parsed = new URL(template.replace("{database}", "placeholder"));
	if (parsed.password !== "") parsed.password = "[redacted]";
	return parsed.toString().replace("placeholder", "{database}");
}

function assertFile(path: string, label: string): void {
	if (!existsSync(path) || !statSync(path).isFile()) {
		throw new Error(`${label} does not exist: ${path}`);
	}
}

function assertDirectory(path: string, label: string): void {
	if (!existsSync(path) || !statSync(path).isDirectory()) {
		throw new Error(`${label} does not exist: ${path}`);
	}
}

function frameworkFilter(): FrameworkName[] {
	const value = Bun.env.RIDU_STRESS_FRAMEWORKS;
	if (value === undefined || value.trim() === "") return ["ridu", "payload"];
	const frameworks = [...new Set(value.split(",").map((item) => item.trim().toLowerCase()))];
	if (frameworks.length === 0 || frameworks.some((item) => item !== "ridu" && item !== "payload")) {
		throw new Error(
			"RIDU_STRESS_FRAMEWORKS must be ridu, payload, or a comma-separated combination"
		);
	}
	return frameworks as FrameworkName[];
}

function positiveIntegerList(name: string, fallback: number[]): number[] {
	const value = Bun.env[name];
	if (value === undefined || value.trim() === "") return fallback;
	const result = value.split(",").map((item) => Number(item.trim()));
	if (result.length === 0 || result.some((item) => !Number.isSafeInteger(item) || item <= 0)) {
		throw new Error(`${name} must be a comma-separated list of positive integers`);
	}
	return result;
}

function positiveInteger(name: string, fallback: number): number {
	const value = Number(Bun.env[name] ?? fallback);
	if (!Number.isSafeInteger(value) || value <= 0)
		throw new Error(`${name} must be a positive integer`);
	return value;
}

function positiveNumber(name: string, fallback: number): number {
	const value = Number(Bun.env[name] ?? fallback);
	if (!Number.isFinite(value) || value <= 0) throw new Error(`${name} must be a positive number`);
	return value;
}

function nonNegativeNumber(name: string, fallback: number): number {
	const value = Number(Bun.env[name] ?? fallback);
	if (!Number.isFinite(value) || value < 0) throw new Error(`${name} must be non-negative`);
	return value;
}

function latencyDistribution(values: number[]): LatencyDistribution {
	const sorted = [...values].sort((left, right) => left - right);
	return {
		p50: round(percentile(sorted, 0.5)),
		p90: round(percentile(sorted, 0.9)),
		p95: round(percentile(sorted, 0.95)),
		p99: round(percentile(sorted, 0.99)),
		"p99.9": round(percentile(sorted, 0.999)),
		max: round(sorted.at(-1) ?? 0),
	};
}

function numericDistribution(values: number[]): NumericDistribution {
	const sorted = [...values].sort((left, right) => left - right);
	return {
		min: round(sorted.at(0) ?? 0),
		median: round(percentile(sorted, 0.5)),
		p95: round(percentile(sorted, 0.95)),
		max: round(sorted.at(-1) ?? 0),
	};
}

function percentile(sorted: number[], quantile: number): number {
	if (sorted.length === 0) return 0;
	return sorted[Math.min(sorted.length - 1, Math.ceil(sorted.length * quantile) - 1)]!;
}

async function parallelMap<T>(
	count: number,
	workerCount: number,
	work: (index: number) => Promise<T>
): Promise<T[]> {
	if (count === 0) return [];
	const result = new Array<T>(count);
	let next = 0;
	await Promise.all(
		Array.from({ length: Math.min(count, workerCount) }, async () => {
			while (true) {
				const index = next;
				next += 1;
				if (index >= count) return;
				result[index] = await work(index);
			}
		})
	);
	return result;
}

function isTimeout(error: unknown): boolean {
	return (
		(isRecord(error) && (error.name === "TimeoutError" || error.code === "ETIMEDOUT")) ||
		errorText(error).toLowerCase().includes("timed out")
	);
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function errorText(error: unknown): string {
	return error instanceof Error ? `${error.name}: ${error.message}` : String(error);
}

function throwIfInterrupted(): void {
	if (interruptedSignal !== undefined) {
		throw new Error(`stress benchmark interrupted by ${interruptedSignal}`);
	}
}

function round(value: number): number {
	return Math.round(value * 100) / 100;
}

function delay(milliseconds: number): Promise<void> {
	return new Promise((resolveDelay) => setTimeout(resolveDelay, milliseconds));
}

async function interruptibleDelay(milliseconds: number): Promise<void> {
	const deadline = performance.now() + milliseconds;
	while (interruptedSignal === undefined && performance.now() < deadline) {
		await delay(Math.min(100, Math.max(0, deadline - performance.now())));
	}
}

async function writeReport(): Promise<void> {
	const encoded = `${JSON.stringify(report, null, 2)}\n`;
	await Promise.all([Bun.write(resultPath, encoded), Bun.write(latestPath, encoded)]);
}
