// Ridu versus PocketBase on SQLite. Both servers are pure-Go single binaries built with the same
// toolchain and flags, each with its own pinned modernc SQLite driver and default settings. See
// README.md for the method, the durability-matched PocketBase variant and the limits.
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
	closeSync,
	mkdirSync,
	openSync,
	readdirSync,
	readFileSync,
	rmSync,
	statSync,
} from "node:fs";
import { join, relative } from "node:path";

type FrameworkName = "ridu" | "pocketbase" | "pocketbase-full";

type Framework = {
	name: FrameworkName;
	baseURL: string;
	port: number;
	migrateCommand: (dataDirectory: string) => string[];
	serveCommand: (dataDirectory: string) => string[];
	environment: (dataDirectory: string) => Record<string, string>;
	bootstrap?: () => Promise<void>;
	login: () => Promise<Record<string, string>>;
	readyPath: string;
	adminPath: string;
	listPath: string;
	findPath: (id: string) => string;
	createPath: string;
	documentPath: (id: string) => string;
	createStatus: number;
	deleteStatus: number;
	listTotal: (body: any) => unknown;
	listItems: (body: any) => unknown;
	document: (body: any) => any;
};

type Response = { status: number; bytes: number; elapsedMs: number; encoding: string };

type LoadResult = {
	name: string;
	requests: number;
	concurrency: number;
	durationMs: number;
	requestsPerSecond: number;
	latencyMs: { p50: number; p95: number; p99: number; max: number };
	responseBytes: { average: number; total: number };
	statuses: Record<string, number>;
	contentEncodings: Record<string, number>;
	serverCPUSeconds: number;
	serverCPUMsPerRequest: number;
	clientCPUPercent: number;
	peakRSSMiB: number;
};

const repository = join(import.meta.dir, "../../..");
const outputDirectory = join(repository, ".ridu/performance/pocketbase");
const riduBinary = join(outputDirectory, "ridu-server");
const pocketBaseBinary = join(outputDirectory, "pocketbase-server");
const pocketBaseModule = join(import.meta.dir, "server");
const riduMigrationDirectory = join(outputDirectory, "ridu-migrations");
const trials = positiveInteger("RIDU_PB_TRIALS", 3);
const datasetSize = positiveInteger("RIDU_PB_DATASET", 1_000);
const readRequests = positiveInteger("RIDU_PB_READ_REQUESTS", 5_000);
const mutationRequests = positiveInteger("RIDU_PB_MUTATION_REQUESTS", 1_000);
const adminRequests = positiveInteger("RIDU_PB_ADMIN_REQUESTS", 300);
const concurrency = positiveInteger("RIDU_PB_CONCURRENCY", 16);
const mutationConcurrency = positiveInteger("RIDU_PB_MUTATION_CONCURRENCY", 8);
const requestTimeoutMs = positiveInteger("RIDU_PB_REQUEST_TIMEOUT_MS", 30_000);
// identity keeps both servers serializing the same uncompressed work; Ridu gzips GET bodies over
// 1 KiB for clients that accept it and PocketBase does not compress API responses.
const acceptEncoding = Bun.env.RIDU_PB_ACCEPT_ENCODING ?? "identity";
// Bun's fetch runs one JavaScript thread plus its HTTP thread, so a single-process client
// saturates a little above one core.
const clientBoundPercent = 90;

const email = "admin@riducms.test";
const password = "ridu-admin";
// Every tenth seeded post is a draft, so the anonymous access predicate filters real rows.
const publishedCount = Array.from({ length: datasetSize }, (_, index) => isPublished(index)).filter(
	Boolean
).length;
if (publishedCount < 10) throw new Error("RIDU_PB_DATASET must seed at least ten published posts");
const buildFlags = ["-trimpath", "-ldflags=-s -w"];
const buildEnvironment = { CGO_ENABLED: "0", GOWORK: "off", GOFLAGS: "-mod=readonly" };

const selectedFrameworks = (Bun.env.RIDU_PB_FRAMEWORKS ?? "ridu,pocketbase,pocketbase-full")
	.split(",")
	.map((name) => name.trim())
	.filter((name) => name !== "");
for (const name of selectedFrameworks) {
	if (!["ridu", "pocketbase", "pocketbase-full"].includes(name)) {
		throw new Error(`RIDU_PB_FRAMEWORKS contains unknown framework ${JSON.stringify(name)}`);
	}
}
if (selectedFrameworks.length === 0) throw new Error("RIDU_PB_FRAMEWORKS selects no framework");

mkdirSync(join(outputDirectory, "logs"), { recursive: true });
const riduBuildCommand = [
	"go",
	"build",
	...buildFlags,
	"-o",
	relative(repository, riduBinary),
	"./tests/performance/pocketbase/ridu",
];
const pocketBaseBuildCommand = ["go", "build", ...buildFlags, "-o", pocketBaseBinary, "."];
runCommand(riduBuildCommand, repository, buildEnvironment);
runCommand(pocketBaseBuildCommand, pocketBaseModule, buildEnvironment);
// Mirror a deployment: write the migration history once and take its executable digest. Each
// trial applies it in a separate step before the server starts, as PocketBase's migrate up does.
rmSync(riduMigrationDirectory, { recursive: true, force: true });
const riduHistoryDigest = commandOutput(riduBinary, ["migrations", riduMigrationDirectory]);
const riduMigrationArtifacts = readdirSync(riduMigrationDirectory).filter((name) =>
	name.endsWith(".ridu.json")
);

const riduSelect = encodeURIComponent(JSON.stringify({ title: true, summary: true, status: true }));
const riduFramework: Framework = {
	name: "ridu",
	baseURL: "http://127.0.0.1:18281",
	port: 18281,
	migrateCommand: () => [riduBinary, "migrate", riduMigrationDirectory],
	serveCommand: () => [riduBinary],
	environment: (dataDirectory) => ({
		RIDU_PB_BENCH_SQLITE_PATH: join(dataDirectory, "ridu.db"),
		RIDU_PB_BENCH_ADDRESS: "127.0.0.1:18281",
		RIDU_PB_BENCH_HISTORY_DIGEST: riduHistoryDigest,
	}),
	bootstrap: async () => {
		await checkedFetch(
			"http://127.0.0.1:18281/api/auth/users/create-user",
			jsonRequest("POST", { data: { email }, password }),
			[201]
		);
	},
	login: async () => {
		const response = await fetch("http://127.0.0.1:18281/api/auth/users/login", {
			...jsonRequest("POST", { email, password }),
			signal: AbortSignal.timeout(requestTimeoutMs),
		});
		await response.arrayBuffer();
		const cookie = response.headers.get("set-cookie");
		if (response.status !== 200 || !cookie)
			throw new Error(`ridu login returned ${response.status}`);
		return { Cookie: cookie.split(";", 1)[0]! };
	},
	readyPath: "/api/collections/posts?limit=1",
	adminPath: "/admin/login",
	listPath: `/api/collections/posts?limit=10&sort=id&select=${riduSelect}`,
	findPath: (id) => `/api/collections/posts/${id}?select=${riduSelect}`,
	createPath: "/api/collections/posts",
	documentPath: (id) => `/api/collections/posts/${id}`,
	createStatus: 201,
	deleteStatus: 200,
	listTotal: (body) => body.pagination?.totalDocs,
	listItems: (body) => body.docs,
	document: (body) => body.doc,
};

function pocketBaseFramework(name: "pocketbase" | "pocketbase-full"): Framework {
	const baseURL = "http://127.0.0.1:18290";
	return {
		name,
		baseURL,
		port: 18290,
		migrateCommand: (dataDirectory) => [pocketBaseBinary, "migrate", "up", "--dir", dataDirectory],
		serveCommand: (dataDirectory) => [
			pocketBaseBinary,
			"serve",
			"--dir",
			dataDirectory,
			"--http",
			"127.0.0.1:18290",
		],
		environment: () => ({ PB_BENCH_SYNCHRONOUS: name === "pocketbase-full" ? "FULL" : "NORMAL" }),
		// The fixture's migration creates the benchmark user, as Ridu's create-user does here.
		login: async () => {
			const response = await checkedJSON(
				`${baseURL}/api/collections/users/auth-with-password`,
				jsonRequest("POST", { identity: email, password }),
				[200]
			);
			if (typeof response.body.token !== "string") throw new Error(`${name} login omitted token`);
			return { Authorization: response.body.token };
		},
		readyPath: "/api/collections/posts/records?perPage=1",
		adminPath: "/_/",
		listPath: "/api/collections/posts/records?perPage=10&sort=id&fields=id,title,summary,status",
		findPath: (id) => `/api/collections/posts/records/${id}?fields=id,title,summary,status`,
		createPath: "/api/collections/posts/records",
		documentPath: (id) => `/api/collections/posts/records/${id}`,
		createStatus: 200,
		deleteStatus: 204,
		listTotal: (body) => body.totalItems,
		listItems: (body) => body.items,
		document: (body) => body,
	};
}

const allFrameworks = [
	riduFramework,
	pocketBaseFramework("pocketbase"),
	pocketBaseFramework("pocketbase-full"),
];
const frameworks = allFrameworks.filter((framework) => selectedFrameworks.includes(framework.name));

const startedAt = new Date();
const results: Array<Record<string, unknown>> = [];
for (let trial = 1; trial <= trials; trial += 1) {
	// Rotate the order every trial so no framework always runs first or on a warmer machine.
	const offset = (trial - 1) % frameworks.length;
	for (const framework of [...frameworks.slice(offset), ...frameworks.slice(0, offset)]) {
		results.push(await runTrial(framework, trial));
	}
}
const finishedAt = new Date();

const gitStatus = commandOutput("git", ["status", "--porcelain=v1", "--untracked-files=all"]);
const summary = summarize(results);
const report = {
	version: 1,
	startedAt: startedAt.toISOString(),
	finishedAt: finishedAt.toISOString(),
	environment: {
		platform: commandOutput("sw_vers", ["-productVersion"]),
		kernel: commandOutput("uname", ["-srvmp"]),
		cpu: commandOutput("sysctl", ["-n", "machdep.cpu.brand_string"]),
		logicalCPUs: Number(commandOutput("sysctl", ["-n", "hw.logicalcpu"])),
		memoryBytes: Number(commandOutput("sysctl", ["-n", "hw.memsize"])),
		go: commandOutput("go", ["version"]),
		bun: Bun.version,
		riduRevision: commandOutput("git", ["rev-parse", "HEAD"]),
		git: { dirty: gitStatus !== "", status: gitStatus === "" ? [] : gitStatus.split("\n") },
	},
	configuration: {
		trials,
		datasetSize,
		publishedCount,
		readRequests,
		mutationRequests,
		adminRequests,
		concurrency,
		mutationConcurrency,
		requestTimeoutMs,
		acceptEncoding,
		frameworks: frameworks.map((framework) => framework.name),
		inheritedRuntimeTuning: inheritedRuntimeTuning(),
		build: {
			flags: buildFlags,
			environment: buildEnvironment,
			ridu: riduBuildCommand.join(" "),
			pocketbase: `${pocketBaseBuildCommand.join(" ")} (in ${relative(repository, pocketBaseModule)})`,
		},
		riduMigration: {
			separateStep: true,
			artifacts: riduMigrationArtifacts,
			historyDigest: riduHistoryDigest,
		},
		// Ridu's adapter sets these on every pooled connection; it exposes no pragma diagnostic.
		riduSQLite: {
			source: "adapters/sqlite/sqlite.go",
			synchronous: "full",
			journalMode: "wal",
			busyTimeoutMs: 10_000,
			maxConnections: 8,
		},
	},
	artifacts: {
		harness: fileArtifact(join(import.meta.dir, "run.ts")),
		riduFixture: fileArtifact(join(import.meta.dir, "ridu/main.go")),
		pocketBaseFixture: fileArtifact(join(pocketBaseModule, "main.go")),
		pocketBaseGoSum: fileArtifact(join(pocketBaseModule, "go.sum")),
		riduBinary: { ...fileArtifact(riduBinary), modules: binaryModules(riduBinary) },
		pocketBaseBinary: {
			...fileArtifact(pocketBaseBinary),
			modules: binaryModules(pocketBaseBinary),
		},
	},
	summary,
	results,
};

const stamp = startedAt.toISOString().replaceAll(":", "-");
const resultPath = join(outputDirectory, `results-${stamp}.json`);
await Bun.write(resultPath, `${JSON.stringify(report, null, 2)}\n`);
await Bun.write(join(outputDirectory, "latest.json"), `${JSON.stringify(report, null, 2)}\n`);
printSummary(summary);
console.log(`\nRaw results: ${resultPath}`);

async function runTrial(framework: Framework, trial: number): Promise<Record<string, unknown>> {
	console.log(`\n=== ${framework.name} trial ${trial}/${trials} ===`);
	assertPortAvailable(framework.port);
	const dataDirectory = join(outputDirectory, "data", `${framework.name}-${trial}`);
	rmSync(dataDirectory, { recursive: true, force: true });
	mkdirSync(dataDirectory, { recursive: true });
	const environment = framework.environment(dataDirectory);
	runCommand(framework.migrateCommand(dataDirectory), outputDirectory, environment);
	const logPath = join(outputDirectory, "logs", `${framework.name}-${trial}.log`);
	const log = openSync(logPath, "w");
	const launchedAt = performance.now();
	const server = Bun.spawn(framework.serveCommand(dataDirectory), {
		cwd: outputDirectory,
		env: { ...Bun.env, ...environment },
		stdout: log,
		stderr: log,
	});
	closeSync(log);
	try {
		await waitUntilReady(`${framework.baseURL}${framework.readyPath}`, server, logPath);
		const startupMs = performance.now() - launchedAt;
		await framework.bootstrap?.();
		const auth = await framework.login();
		const sqlitePragmas = framework.name.startsWith("pocketbase")
			? (await checkedJSON(`${framework.baseURL}/__bench/sqlite`, {}, [200])).body
			: undefined;
		await delay(2_000);
		const idleRSSMiB = await stableRSS(server.pid);
		const coldAdminStarted = performance.now();
		await checkedFetch(`${framework.baseURL}${framework.adminPath}`, {}, [200]);
		const coldAdminMs = performance.now() - coldAdminStarted;

		console.log(`seeding ${datasetSize} posts (${publishedCount} published)`);
		const seeded = await parallelMap(datasetSize, 8, async (index) => {
			const created = await createPost(framework, auth, index, isPublished(index));
			return { id: created.id, published: isPublished(index) };
		});
		const publishedIDs = seeded.filter((post) => post.published).map((post) => post.id);
		await validateCounts(framework, auth, datasetSize);
		await delay(2_000);
		const datasetRSSMiB = await stableRSS(server.pid);
		const databaseBytes = directoryBytes(dataDirectory);

		await parallelMap(200, 8, (index) =>
			checkedFetch(
				`${framework.baseURL}${index % 2 === 0 ? framework.listPath : framework.findPath(publishedIDs[0]!)}`,
				{},
				[200]
			)
		);
		const loads: LoadResult[] = [];
		loads.push(
			await runLoad("list", readRequests, concurrency, server.pid, () =>
				checkedList(framework, {}, publishedCount)
			)
		);
		loads.push(
			await runLoad("find", readRequests, concurrency, server.pid, async (index) => {
				const id = publishedIDs[index % publishedIDs.length]!;
				const response = await checkedJSON(
					`${framework.baseURL}${framework.findPath(id)}`,
					{},
					[200]
				);
				if (framework.document(response.body)?.id !== id)
					throw new Error(`${framework.name} find returned another document`);
				return response;
			})
		);
		loads.push(
			await runLoad("list-authenticated", readRequests, concurrency, server.pid, () =>
				checkedList(framework, auth, datasetSize)
			)
		);
		loads.push(
			await runLoad("admin-html", adminRequests, Math.min(concurrency, 8), server.pid, () =>
				checkedFetch(`${framework.baseURL}${framework.adminPath}`, {}, [200])
			)
		);
		const mutationIDs: string[] = [];
		loads.push(
			await runLoad("create", mutationRequests, mutationConcurrency, server.pid, async (index) => {
				const created = await createPost(framework, auth, datasetSize + index, false);
				mutationIDs[index] = created.id;
				return created.response;
			})
		);
		loads.push(
			await runLoad("update", mutationRequests, mutationConcurrency, server.pid, async (index) => {
				const summary = `Updated benchmark summary ${trial}-${index}`;
				const response = await checkedJSON(
					`${framework.baseURL}${framework.documentPath(mutationIDs[index]!)}`,
					jsonRequest("PATCH", { summary }, auth),
					[200]
				);
				if (framework.document(response.body)?.summary !== summary) {
					throw new Error(`${framework.name} update did not echo the summary`);
				}
				return response;
			})
		);
		loads.push(
			await runLoad("delete", mutationRequests, mutationConcurrency, server.pid, (index) =>
				checkedFetch(
					`${framework.baseURL}${framework.documentPath(mutationIDs[index]!)}`,
					{ method: "DELETE", headers: auth },
					[framework.deleteStatus]
				)
			)
		);
		await validateCounts(framework, auth, datasetSize);

		return {
			framework: framework.name,
			trial,
			startupMs: round(startupMs),
			idleRSSMiB: round(idleRSSMiB),
			coldAdminMs: round(coldAdminMs),
			datasetRSSMiB: round(datasetRSSMiB),
			databaseBytesAfterSeeding: databaseBytes,
			sqlitePragmas,
			loads,
		};
	} finally {
		server.kill("SIGTERM");
		await Promise.race([server.exited, delay(5_000)]);
		if (server.exitCode === null) server.kill("SIGKILL");
	}
}

function isPublished(index: number): boolean {
	return index % 10 !== 9;
}

async function createPost(
	framework: Framework,
	auth: Record<string, string>,
	index: number,
	published: boolean
): Promise<{ id: string; response: Response }> {
	const title = `Benchmark post ${index}`;
	const response = await checkedJSON(
		`${framework.baseURL}${framework.createPath}`,
		jsonRequest(
			"POST",
			{
				title,
				summary: `Equivalent SQLite benchmark document ${index}`,
				status: published ? "published" : "draft",
				readingMinutes: (index % 12) + 1,
				featured: index % 5 === 0,
			},
			auth
		),
		[framework.createStatus]
	);
	const document = framework.document(response.body);
	if (typeof document?.id !== "string" || document.title !== title) {
		throw new Error(`${framework.name} create did not echo the new document`);
	}
	return { id: document.id, response };
}

async function checkedList(
	framework: Framework,
	auth: Record<string, string>,
	expectedTotal: number
): Promise<Response> {
	const response = await checkedJSON(
		`${framework.baseURL}${framework.listPath}`,
		{ headers: auth },
		[200]
	);
	const items = framework.listItems(response.body);
	if (
		framework.listTotal(response.body) !== expectedTotal ||
		!Array.isArray(items) ||
		items.length !== 10
	) {
		throw new Error(`${framework.name} list returned an unexpected page`);
	}
	return response;
}

async function validateCounts(
	framework: Framework,
	auth: Record<string, string>,
	expectedTotal: number
): Promise<void> {
	await checkedList(framework, {}, publishedCount);
	await checkedList(framework, auth, expectedTotal);
}

async function runLoad(
	name: string,
	requests: number,
	loadConcurrency: number,
	pid: number,
	request: (index: number) => Promise<Response>
): Promise<LoadResult> {
	console.log(`${name}: ${requests} requests at concurrency ${loadConcurrency}`);
	let peakRSS = await treeRSSMiB(pid);
	let sampling = true;
	const sampler = (async () => {
		while (sampling) {
			peakRSS = Math.max(peakRSS, await treeRSSMiB(pid));
			await delay(100);
		}
	})();
	const serverCPUBefore = await cpuSeconds(pid);
	const clientCPUBefore = process.cpuUsage();
	const started = performance.now();
	const responses = await parallelMap(requests, loadConcurrency, request);
	const durationMs = performance.now() - started;
	const clientCPU = process.cpuUsage(clientCPUBefore);
	const serverCPUSeconds = (await cpuSeconds(pid)) - serverCPUBefore;
	sampling = false;
	await sampler;
	const latencies = responses.map((response) => response.elapsedMs).sort((a, b) => a - b);
	const statuses: Record<string, number> = {};
	const contentEncodings: Record<string, number> = {};
	let totalBytes = 0;
	for (const response of responses) {
		statuses[String(response.status)] = (statuses[String(response.status)] ?? 0) + 1;
		contentEncodings[response.encoding] = (contentEncodings[response.encoding] ?? 0) + 1;
		totalBytes += response.bytes;
	}
	return {
		name,
		requests,
		concurrency: loadConcurrency,
		durationMs: round(durationMs),
		requestsPerSecond: round((requests * 1_000) / durationMs),
		latencyMs: {
			p50: round(percentile(latencies, 0.5)),
			p95: round(percentile(latencies, 0.95)),
			p99: round(percentile(latencies, 0.99)),
			max: round(latencies.at(-1) ?? 0),
		},
		responseBytes: { average: round(totalBytes / requests), total: totalBytes },
		statuses,
		contentEncodings,
		serverCPUSeconds: round(serverCPUSeconds),
		serverCPUMsPerRequest: round((serverCPUSeconds * 1_000) / requests),
		clientCPUPercent: round(((clientCPU.user + clientCPU.system) / 1_000 / durationMs) * 100),
		peakRSSMiB: round(peakRSS),
	};
}

function jsonRequest(
	method: string,
	body: unknown,
	headers: Record<string, string> = {}
): RequestInit {
	return {
		method,
		headers: { ...headers, "Content-Type": "application/json" },
		body: JSON.stringify(body),
	};
}

async function checkedFetch(
	url: string,
	init: RequestInit,
	expectedStatuses: number[]
): Promise<Response> {
	return (await timedFetch(url, init, expectedStatuses)).response;
}

async function checkedJSON(
	url: string,
	init: RequestInit,
	expectedStatuses: number[]
): Promise<Response & { body: any }> {
	const { response, text } = await timedFetch(url, init, expectedStatuses);
	return { ...response, body: JSON.parse(text) };
}

async function timedFetch(
	url: string,
	init: RequestInit,
	expectedStatuses: number[]
): Promise<{ response: Response; text: string }> {
	const started = performance.now();
	const response = await fetch(url, {
		...init,
		headers: { "Accept-Encoding": acceptEncoding, ...(init.headers as Record<string, string>) },
		signal: AbortSignal.timeout(requestTimeoutMs),
	});
	const text = await response.text();
	const elapsedMs = performance.now() - started;
	if (!expectedStatuses.includes(response.status)) {
		throw new Error(`${url} returned ${response.status}: ${text.slice(0, 500)}`);
	}
	return {
		response: {
			status: response.status,
			bytes: Buffer.byteLength(text),
			elapsedMs,
			encoding: response.headers.get("content-encoding") ?? "none",
		},
		text,
	};
}

async function parallelMap<T>(
	count: number,
	workerCount: number,
	work: (index: number) => Promise<T>
): Promise<T[]> {
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

async function waitUntilReady(url: string, server: Bun.Subprocess, logPath: string): Promise<void> {
	const deadline = Date.now() + 60_000;
	let lastError = "not ready";
	while (Date.now() < deadline) {
		if (server.exitCode !== null) {
			throw new Error(
				`server exited with ${server.exitCode}: ${readFileSync(logPath, "utf8").slice(-2_000)}`
			);
		}
		try {
			const response = await fetch(url, { signal: AbortSignal.timeout(5_000) });
			const body = await response.text();
			if (response.status === 200) return;
			lastError = `${response.status}: ${body.slice(0, 300)}`;
		} catch (error) {
			lastError = String(error);
		}
		await delay(50);
	}
	throw new Error(`timed out waiting for ${url}: ${lastError}`);
}

async function stableRSS(pid: number): Promise<number> {
	const samples: number[] = [];
	for (let index = 0; index < 10; index += 1) {
		samples.push(await treeRSSMiB(pid));
		await delay(200);
	}
	samples.sort((a, b) => a - b);
	return samples[Math.floor(samples.length / 2)] ?? 0;
}

async function treeRSSMiB(pid: number): Promise<number> {
	return Number(await psField(pid, "rss")) / 1_024;
}

// ps prints accumulated user+system CPU as [hours:]minutes:seconds.hundredths.
async function cpuSeconds(pid: number): Promise<number> {
	return (await psField(pid, "time"))
		.split(":")
		.map(Number)
		.reduce((total, part) => total * 60 + part, 0);
}

async function psField(pid: number, field: string): Promise<string> {
	const process = Bun.spawn(["ps", "-o", `${field}=`, "-p", String(pid)], {
		stdout: "pipe",
		stderr: "pipe",
	});
	const output = (await new Response(process.stdout).text()).trim();
	if ((await process.exited) !== 0 || output === "")
		throw new Error(`ps -o ${field} failed for ${pid}`);
	return output;
}

function summarize(trialResults: Array<Record<string, unknown>>): Record<string, unknown> {
	const byFramework: Record<string, Record<string, unknown>> = {};
	for (const name of frameworks.map((framework) => framework.name)) {
		const runs = trialResults.filter((result) => result.framework === name);
		const loads: Record<string, Record<string, number>> = {};
		for (const loadName of (runs[0]?.loads as LoadResult[] | undefined)?.map((load) => load.name) ??
			[]) {
			const samples = runs.map((run) =>
				(run.loads as LoadResult[]).find((load) => load.name === loadName)!
			);
			loads[loadName] = {
				requestsPerSecond: median(samples.map((load) => load.requestsPerSecond)),
				p50Ms: median(samples.map((load) => load.latencyMs.p50)),
				p95Ms: median(samples.map((load) => load.latencyMs.p95)),
				serverCPUMsPerRequest: median(samples.map((load) => load.serverCPUMsPerRequest)),
				averageResponseBytes: median(samples.map((load) => load.responseBytes.average)),
				peakRSSMiB: median(samples.map((load) => load.peakRSSMiB)),
				clientCPUPercent: median(samples.map((load) => load.clientCPUPercent)),
			};
		}
		byFramework[name] = {
			trials: runs.length,
			startupMs: median(runs.map((run) => run.startupMs as number)),
			idleRSSMiB: median(runs.map((run) => run.idleRSSMiB as number)),
			datasetRSSMiB: median(runs.map((run) => run.datasetRSSMiB as number)),
			databaseBytesAfterSeeding: median(runs.map((run) => run.databaseBytesAfterSeeding as number)),
			loads,
		};
	}
	return {
		statistic: "median across trials",
		// At or above this client CPU, throughput and latency measure the load generator rather
		// than the server; compare serverCPUMsPerRequest for those loads instead.
		clientBoundPercent,
		frameworks: byFramework,
	};
}

function printSummary(result: Record<string, unknown>): void {
	const byFramework = result.frameworks as Record<string, any>;
	const names = Object.keys(byFramework);
	console.log(
		"\nMedian across trials: req/s · p95 ms · server CPU ms/request (* client-bound: compare CPU)"
	);
	const loadNames = Object.keys(byFramework[names[0]!]?.loads ?? {});
	const rows = loadNames.map((loadName) => {
		const row: Record<string, string> = { load: loadName };
		for (const name of names) {
			const load = byFramework[name].loads[loadName];
			const clientBound = load.clientCPUPercent >= clientBoundPercent ? " *" : "";
			row[name] =
				`${load.requestsPerSecond} · ${load.p95Ms} · ${load.serverCPUMsPerRequest}${clientBound}`;
		}
		return row;
	});
	for (const metric of ["startupMs", "idleRSSMiB", "datasetRSSMiB", "databaseBytesAfterSeeding"]) {
		const row: Record<string, string> = { load: metric };
		for (const name of names) row[name] = String(byFramework[name][metric]);
		rows.push(row);
	}
	console.table(rows);
}

function binaryModules(path: string): Record<string, string> {
	const modules: Record<string, string> = {};
	for (const line of commandOutput("go", ["version", "-m", path]).split("\n")) {
		const [kind, name, version] = line.trim().split(/\s+/);
		if ((kind === "dep" || kind === "mod") && name && version) {
			if (
				/^(modernc\.org\/(sqlite|libc)|github\.com\/pocketbase\/pocketbase|github\.com\/riducms\/ridu)$/.test(
					name
				)
			) {
				modules[name] = version;
			}
		}
		if (kind === "go") modules.go = name ?? "";
	}
	return modules;
}

function directoryBytes(path: string): number {
	let total = 0;
	for (const entry of readdirSync(path, { recursive: true, withFileTypes: true })) {
		if (entry.isFile()) total += statSync(join(entry.parentPath, entry.name)).size;
	}
	return total;
}

function fileArtifact(path: string): { bytes: number; sha256: string } {
	return {
		bytes: statSync(path).size,
		sha256: createHash("sha256").update(readFileSync(path)).digest("hex"),
	};
}

function inheritedRuntimeTuning(): Record<string, string> {
	const tuning: Record<string, string> = {};
	for (const name of ["GOGC", "GOMEMLIMIT", "GOMAXPROCS", "GODEBUG"]) {
		const value = Bun.env[name];
		if (value !== undefined && value !== "") tuning[name] = value;
	}
	return tuning;
}

function assertPortAvailable(port: number): void {
	const result = Bun.spawnSync(["lsof", "-nP", `-iTCP:${port}`, "-sTCP:LISTEN", "-t"], {
		stdout: "pipe",
		stderr: "pipe",
	});
	const owner = result.stdout.toString().trim();
	if (owner !== "") throw new Error(`benchmark port ${port} is already owned by process ${owner}`);
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

function commandOutput(command: string, args: string[], cwd = repository): string {
	return execFileSync(command, args, { cwd, encoding: "utf8" }).trim();
}

function percentile(sorted: number[], quantile: number): number {
	if (sorted.length === 0) return 0;
	return sorted[Math.min(sorted.length - 1, Math.ceil(sorted.length * quantile) - 1)]!;
}

function median(values: number[]): number {
	const sorted = [...values].sort((a, b) => a - b);
	if (sorted.length === 0) return 0;
	const middle = Math.floor(sorted.length / 2);
	return round(
		sorted.length % 2 === 1 ? sorted[middle]! : (sorted[middle - 1]! + sorted[middle]!) / 2
	);
}

function positiveInteger(name: string, fallback: number): number {
	const value = Number(Bun.env[name] ?? fallback);
	if (!Number.isSafeInteger(value) || value <= 0)
		throw new Error(`${name} must be a positive integer`);
	return value;
}

function round(value: number): number {
	return Math.round(value * 100) / 100;
}

function delay(milliseconds: number): Promise<void> {
	return new Promise((resolve) => setTimeout(resolve, milliseconds));
}
