import { execFileSync } from "node:child_process";
import { cpSync, mkdirSync, statSync } from "node:fs";
import { join } from "node:path";

type FrameworkName = "ridu" | "payload";

type Framework = {
	name: FrameworkName;
	baseURL: string;
	database: string;
	command: string[];
	cwd: string;
	environment: Record<string, string>;
	readyPath: string;
	adminPath: string;
	listPath: string;
	findPath: (id: string) => string;
	loginPath: string;
	createPath: (published: boolean) => string;
	updatePath: (id: string) => string;
	deletePath: (id: string) => string;
};

type LoadResult = {
	name: string;
	requests: number;
	concurrency: number;
	durationMs: number;
	requestsPerSecond: number;
	latencyMs: { p50: number; p95: number; p99: number; max: number };
	responseBytes: { average: number; total: number };
	statuses: Record<string, number>;
	peakTreeRSSMiB: number;
};

const repository = join(import.meta.dir, "../..");
const payloadDirectory = join(repository, "tests/contracts/payload_server");
const payloadStandaloneDirectory = join(payloadDirectory, ".next/standalone");
const outputDirectory = join(repository, ".ridu/performance");
const binary = join(outputDirectory, "ridu-server");
const databaseURLTemplate =
	Bun.env.RIDU_PERF_POSTGRES_URL_TEMPLATE ??
	`postgres://${encodeURIComponent(Bun.env.USER ?? "postgres")}@127.0.0.1:5432/{database}?sslmode=disable`;
if (!databaseURLTemplate.includes("{database}")) {
	throw new Error("RIDU_PERF_POSTGRES_URL_TEMPLATE must contain {database}");
}
const databaseURL = (database: string) => databaseURLTemplate.replace("{database}", database);
const postgresConnection = new URL(databaseURL("postgres"));
const trials = positiveInteger("RIDU_PERF_TRIALS", 3);
const datasetSize = positiveInteger("RIDU_PERF_DATASET", 250);
const readRequests = positiveInteger("RIDU_PERF_READ_REQUESTS", 3_000);
const mutationRequests = positiveInteger("RIDU_PERF_MUTATION_REQUESTS", 300);
const adminRequests = positiveInteger("RIDU_PERF_ADMIN_REQUESTS", 300);
const concurrency = positiveInteger("RIDU_PERF_CONCURRENCY", 16);
const mutationConcurrency = positiveInteger(
	"RIDU_PERF_MUTATION_CONCURRENCY",
	Math.min(concurrency, 8)
);
const requestTimeoutMs = positiveInteger("RIDU_PERF_REQUEST_TIMEOUT_MS", 30_000);
const secret = "ridu-performance-payload-secret";

mkdirSync(outputDirectory, { recursive: true });
cpSync(join(payloadDirectory, ".next/static"), join(payloadStandaloneDirectory, ".next/static"), {
	recursive: true,
	force: true,
});

const frameworks: Framework[] = [
	{
		name: "ridu",
		baseURL: "http://127.0.0.1:18181",
		database: "ridu_performance",
		command: [binary],
		cwd: repository,
		environment: {
			RIDU_BROWSER_ADDRESS: "127.0.0.1:18181",
			RIDU_BROWSER_PREVIEW_ADDRESS: "127.0.0.1:18182",
		},
		readyPath: "/readyz",
		adminPath: "/admin/login",
		listPath: '/api/collections/posts?limit=10&select={"title":true,"summary":true,"status":true}',
		findPath: (id) =>
			`/api/collections/posts/${id}?select={"title":true,"summary":true,"status":true}`,
		loginPath: "/api/auth/users/login",
		createPath: () => "/api/collections/posts",
		updatePath: (id) => `/api/collections/posts/${id}`,
		deletePath: (id) => `/api/collections/posts/${id}`,
	},
	{
		name: "payload",
		baseURL: "http://127.0.0.1:3010",
		database: "payload_performance",
		command: ["node", join(payloadStandaloneDirectory, "server.js")],
		cwd: payloadStandaloneDirectory,
		environment: {
			HOSTNAME: "127.0.0.1",
			NODE_ENV: "production",
			PAYLOAD_SECRET: secret,
			PORT: "3010",
		},
		readyPath:
			"/api/posts?limit=1&depth=0&select[title]=true&select[summary]=true&select[status]=true",
		adminPath: "/admin/login",
		listPath:
			"/api/posts?limit=10&depth=0&select[title]=true&select[summary]=true&select[status]=true",
		findPath: (id) =>
			`/api/posts/${id}?depth=0&select[title]=true&select[summary]=true&select[status]=true`,
		loginPath: "/api/users/login",
		createPath: (published) => `/api/posts?draft=${published ? "false" : "true"}&depth=0`,
		updatePath: (id) => `/api/posts/${id}?draft=true&depth=0`,
		deletePath: (id) => `/api/posts/${id}?depth=0`,
	},
];

const startedAt = new Date();
const results: Array<Record<string, unknown>> = [];

for (let trial = 1; trial <= trials; trial += 1) {
	for (const framework of trial % 2 === 1 ? frameworks : [...frameworks].reverse()) {
		results.push(await runTrial(framework, trial));
	}
}

const report = {
	version: 1,
	startedAt: startedAt.toISOString(),
	finishedAt: new Date().toISOString(),
	environment: {
		platform: commandOutput("sw_vers", ["-productVersion"]),
		kernel: commandOutput("uname", ["-srvmp"]),
		cpu: commandOutput("sysctl", ["-n", "machdep.cpu.brand_string"]),
		memoryBytes: Number(commandOutput("sysctl", ["-n", "hw.memsize"])),
		postgres: commandOutput("psql", ["-d", "postgres", "-Atqc", "show server_version"]),
		go: commandOutput("go", ["version"]),
		bun: commandOutput("bun", ["--version"]),
		node: commandOutput("node", ["--version"]),
		riduRevision: commandOutput("git", ["rev-parse", "HEAD"], repository),
		payloadVersion: "3.87.0",
		nextVersion: "16.2.6",
	},
	configuration: {
		trials,
		datasetSize,
		readRequests,
		mutationRequests,
		adminRequests,
		concurrency,
		mutationConcurrency,
		requestTimeoutMs,
		postgresIncludedInRSS: false,
		postgresHost: postgresConnection.hostname,
		postgresPort: postgresConnection.port || "5432",
		postgresUser: decodeURIComponent(postgresConnection.username),
		riduBinaryBytes: statSync(binary).size,
		payloadStandaloneKiB: Number(
			commandOutput("du", ["-sk", payloadStandaloneDirectory]).split(/\s+/)[0]
		),
	},
	results,
};

const stamp = startedAt.toISOString().replaceAll(":", "-");
const resultPath = join(outputDirectory, `results-${stamp}.json`);
await Bun.write(resultPath, `${JSON.stringify(report, null, 2)}\n`);
await Bun.write(join(outputDirectory, "latest.json"), `${JSON.stringify(report, null, 2)}\n`);
console.log(`\nRaw results: ${resultPath}`);

async function runTrial(framework: Framework, trial: number): Promise<Record<string, unknown>> {
	console.log(`\n=== ${framework.name} trial ${trial}/${trials} ===`);
	assertPortsAvailable(framework.name === "ridu" ? [18181, 18182] : [3010]);
	recreateDatabase(framework.database);
	const url = databaseURL(framework.database);
	if (framework.name === "payload") {
		runCommand([join(payloadDirectory, "node_modules/.bin/payload"), "migrate"], payloadDirectory, {
			PAYLOAD_DATABASE_URL: url,
			PAYLOAD_SECRET: secret,
		});
	}
	const launchedAt = performance.now();
	const process = Bun.spawn(framework.command, {
		cwd: framework.cwd,
		env: {
			...Bun.env,
			...framework.environment,
			...(framework.name === "ridu" ? { RIDU_POSTGRES_URL: url } : { PAYLOAD_DATABASE_URL: url }),
		},
		stdout: "inherit",
		stderr: "inherit",
	});
	try {
		await waitUntilReady(`${framework.baseURL}${framework.readyPath}`, process);
		const startupMs = performance.now() - launchedAt;
		const cookie = await login(framework);
		await delay(2_000);
		const idleTreeRSSMiB = await stableRSS(process.pid);
		const coldAdminStarted = performance.now();
		await checkedFetch(`${framework.baseURL}${framework.adminPath}`, {}, [200]);
		const coldAdminMs = performance.now() - coldAdminStarted;
		await delay(1_000);
		const adminTreeRSSMiB = await stableRSS(process.pid);

		console.log(`seeding ${datasetSize} published posts`);
		const publishedIDs = await parallelMap(
			datasetSize,
			Math.min(concurrency, 12),
			async (index) => {
				const { id } = await createPost(framework, cookie, index, true);
				if (framework.name === "ridu") {
					await checkedFetch(
						`${framework.baseURL}/api/collections/posts/${id}/publish`,
						{ method: "POST", headers: { Cookie: cookie } },
						[200]
					);
				}
				return id;
			}
		);
		const publishedDocuments = await validatePublishedDataset(framework);
		await delay(2_000);
		const datasetTreeRSSMiB = await stableRSS(process.pid);

		await warm(framework, publishedIDs[0]);
		const loads: LoadResult[] = [];
		loads.push(
			await runLoad("list", readRequests, concurrency, process.pid, () =>
				checkedFetch(`${framework.baseURL}${framework.listPath}`, {}, [200])
			)
		);
		loads.push(
			await runLoad("find", readRequests, concurrency, process.pid, (index) =>
				checkedFetch(
					`${framework.baseURL}${framework.findPath(publishedIDs[index % publishedIDs.length])}`,
					{},
					[200]
				)
			)
		);
		loads.push(
			await runLoad("admin-html", adminRequests, Math.min(concurrency, 8), process.pid, () =>
				checkedFetch(`${framework.baseURL}${framework.adminPath}`, {}, [200])
			)
		);

		const mutationIDs: string[] = [];
		loads.push(
			await runLoad(
				"create-draft",
				mutationRequests,
				mutationConcurrency,
				process.pid,
				async (index) => {
					const created = await createPost(framework, cookie, datasetSize + index, false);
					mutationIDs[index] = created.id;
					return { status: 201, bytes: created.bytes, elapsedMs: created.elapsedMs };
				}
			)
		);
		loads.push(
			await runLoad("update-draft", mutationRequests, mutationConcurrency, process.pid, (index) =>
				checkedFetch(
					`${framework.baseURL}${framework.updatePath(mutationIDs[index])}`,
					{
						method: "PATCH",
						headers: { "Content-Type": "application/json", Cookie: cookie },
						body: JSON.stringify({ summary: `Updated benchmark summary ${trial}-${index}` }),
					},
					[200]
				)
			)
		);
		loads.push(
			await runLoad("delete-draft", mutationRequests, mutationConcurrency, process.pid, (index) =>
				checkedFetch(
					`${framework.baseURL}${framework.deletePath(mutationIDs[index])}`,
					{ method: "DELETE", headers: { Cookie: cookie } },
					[200]
				)
			)
		);

		return {
			framework: framework.name,
			trial,
			startupMs: round(startupMs),
			idleTreeRSSMiB: round(idleTreeRSSMiB),
			coldAdminMs: round(coldAdminMs),
			adminTreeRSSMiB: round(adminTreeRSSMiB),
			datasetTreeRSSMiB: round(datasetTreeRSSMiB),
			publishedDocuments,
			loads,
		};
	} finally {
		process.kill("SIGTERM");
		await Promise.race([process.exited, delay(5_000)]);
		if (process.exitCode === null) process.kill("SIGKILL");
	}
}

async function validatePublishedDataset(framework: Framework): Promise<number> {
	const response = await fetch(
		`${framework.baseURL}${framework.listPath.replace("limit=10", "limit=1")}`,
		{
			signal: AbortSignal.timeout(requestTimeoutMs),
		}
	);
	const body = (await response.json()) as {
		totalDocs?: number;
		pagination?: { totalDocs?: number };
	};
	if (response.status !== 200)
		throw new Error(`${framework.name} dataset check returned ${response.status}`);
	const total = framework.name === "ridu" ? body.pagination?.totalDocs : body.totalDocs;
	const expected = datasetSize + 1;
	if (total !== expected) {
		throw new Error(
			`${framework.name} published dataset has ${total} documents; expected ${expected}`
		);
	}
	return total;
}

async function login(framework: Framework): Promise<string> {
	const response = await fetch(`${framework.baseURL}${framework.loginPath}`, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ email: "admin@riducms.test", password: "ridu-admin" }),
		signal: AbortSignal.timeout(requestTimeoutMs),
	});
	if (response.status !== 200)
		throw new Error(`${framework.name} login returned ${response.status}`);
	await response.arrayBuffer();
	const setCookie = response.headers.get("set-cookie");
	if (!setCookie) throw new Error(`${framework.name} login did not set a cookie`);
	return setCookie.split(";", 1)[0];
}

async function createPost(
	framework: Framework,
	cookie: string,
	index: number,
	published: boolean
): Promise<{ id: string; bytes: number; elapsedMs: number }> {
	const started = performance.now();
	const body: Record<string, unknown> = {
		title: `Performance post ${index}`,
		summary: `Equivalent PostgreSQL benchmark document ${index}`,
		status: published ? "published" : "draft",
		readingMinutes: (index % 12) + 1,
		featured: index % 5 === 0,
	};
	if (framework.name === "payload") body._status = published ? "published" : "draft";
	const response = await fetch(`${framework.baseURL}${framework.createPath(published)}`, {
		method: "POST",
		headers: { "Content-Type": "application/json", Cookie: cookie },
		body: JSON.stringify(body),
		signal: AbortSignal.timeout(requestTimeoutMs),
	});
	const text = await response.text();
	const elapsedMs = performance.now() - started;
	if (response.status !== 201) {
		throw new Error(`${framework.name} create returned ${response.status}: ${text.slice(0, 500)}`);
	}
	const envelope = JSON.parse(text) as { doc?: { id?: string | number } };
	if (envelope.doc?.id === undefined) throw new Error(`${framework.name} create omitted doc.id`);
	return { id: String(envelope.doc.id), bytes: Buffer.byteLength(text), elapsedMs };
}

async function warm(framework: Framework, id: string): Promise<void> {
	await parallelMap(100, Math.min(concurrency, 8), (index) =>
		checkedFetch(
			index % 2 === 0
				? `${framework.baseURL}${framework.listPath}`
				: `${framework.baseURL}${framework.findPath(id)}`,
			{},
			[200]
		)
	);
}

async function runLoad(
	name: string,
	requests: number,
	loadConcurrency: number,
	pid: number,
	request: (index: number) => Promise<{ status: number; bytes: number; elapsedMs: number }>
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
	const started = performance.now();
	const responses = await parallelMap(requests, loadConcurrency, request);
	const durationMs = performance.now() - started;
	sampling = false;
	await sampler;
	const latencies = responses.map((response) => response.elapsedMs).sort((a, b) => a - b);
	const statuses: Record<string, number> = {};
	let totalBytes = 0;
	for (const response of responses) {
		statuses[String(response.status)] = (statuses[String(response.status)] ?? 0) + 1;
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
		peakTreeRSSMiB: round(peakRSS),
	};
}

async function checkedFetch(
	url: string,
	init: RequestInit,
	expectedStatuses: number[]
): Promise<{ status: number; bytes: number; elapsedMs: number }> {
	const started = performance.now();
	const response = await fetch(url, {
		...init,
		signal: init.signal ?? AbortSignal.timeout(requestTimeoutMs),
	});
	const body = await response.arrayBuffer();
	const elapsedMs = performance.now() - started;
	if (!expectedStatuses.includes(response.status)) {
		throw new Error(
			`${url} returned ${response.status}: ${new TextDecoder().decode(body).slice(0, 500)}`
		);
	}
	return { status: response.status, bytes: body.byteLength, elapsedMs };
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

async function waitUntilReady(url: string, process: Bun.Subprocess): Promise<void> {
	const deadline = Date.now() + 120_000;
	let lastError = "not ready";
	while (Date.now() < deadline) {
		if (process.exitCode !== null)
			throw new Error(`server exited with ${process.exitCode}: ${lastError}`);
		try {
			const response = await fetch(url, { signal: AbortSignal.timeout(5_000) });
			const body = await response.text();
			if (response.status === 200) return;
			lastError = `${response.status}: ${body.slice(0, 300)}`;
		} catch (error) {
			lastError = String(error);
		}
		await delay(250);
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

async function treeRSSMiB(rootPID: number): Promise<number> {
	const process = Bun.spawn(["ps", "-axo", "pid=,ppid=,rss="], {
		stdout: "pipe",
		stderr: "pipe",
	});
	const output = await new Response(process.stdout).text();
	const error = await new Response(process.stderr).text();
	if ((await process.exited) !== 0) throw new Error(`ps failed: ${error}`);
	const rows = output
		.split("\n")
		.map((line) => line.trim().split(/\s+/).map(Number))
		.filter((row) => row.length === 3 && row.every(Number.isFinite));
	const children = new Map<number, number[]>();
	const rss = new Map<number, number>();
	for (const [pid, parent, resident] of rows) {
		rss.set(pid, resident);
		children.set(parent, [...(children.get(parent) ?? []), pid]);
	}
	let totalKiB = 0;
	const stack = [rootPID];
	const visited = new Set<number>();
	while (stack.length > 0) {
		const pid = stack.pop()!;
		if (visited.has(pid)) continue;
		visited.add(pid);
		totalKiB += rss.get(pid) ?? 0;
		stack.push(...(children.get(pid) ?? []));
	}
	return totalKiB / 1_024;
}

function recreateDatabase(database: string): void {
	const environment = {
		PGHOST: postgresConnection.hostname,
		PGPORT: postgresConnection.port || "5432",
		PGUSER: decodeURIComponent(postgresConnection.username),
		PGPASSWORD: decodeURIComponent(postgresConnection.password),
		PGSSLMODE: postgresConnection.searchParams.get("sslmode") ?? "prefer",
	};
	runCommand(["dropdb", "--if-exists", database], repository, environment);
	runCommand(["createdb", database], repository, environment);
}

function assertPortsAvailable(ports: number[]): void {
	for (const port of ports) {
		const result = Bun.spawnSync(["lsof", "-nP", `-iTCP:${port}`, "-sTCP:LISTEN", "-t"], {
			stdout: "pipe",
			stderr: "pipe",
		});
		const owner = result.stdout.toString().trim();
		if (owner !== "")
			throw new Error(`benchmark port ${port} is already owned by process ${owner}`);
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

function commandOutput(command: string, args: string[], cwd = repository): string {
	return execFileSync(command, args, { cwd, encoding: "utf8" }).trim();
}

function percentile(sorted: number[], quantile: number): number {
	if (sorted.length === 0) return 0;
	return sorted[Math.min(sorted.length - 1, Math.ceil(sorted.length * quantile) - 1)];
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
