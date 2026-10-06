// Prints Markdown tables of trial medians from a run.ts results file.
export {};
type Load = {
	name: string;
	error?: string;
	requests: number;
	requestsPerSecond: number;
	latencyMs: { p50: number; p95: number; p99: number; max: number };
	responseBytes: { average: number };
	peakTreeRSSMiB: number;
	failures?: number;
	timeouts?: number;
	firstFailure?: string;
};
type Trial = Record<string, unknown> & {
	framework: string;
	scenario: string;
	variant: string;
	database: string;
	graphql?: boolean;
	loads?: Load[];
	error?: string;
	startupError?: string;
};
type Report = {
	configuration?: { hotTargetProcesses?: number };
	trials: Trial[];
	graphql?: Trial[];
	probes?: Record<
		string,
		{ ridu: Record<string, unknown>; payloadMongoose?: Record<string, unknown> }
	>;
	generation?: Record<string, Record<string, { wallMs: number; bytes: number | null }>>;
	builds?: Record<string, unknown>;
	specs?: Record<string, Record<string, unknown>>;
};

const path = Bun.argv[2] ?? ".ridu/performance/blocks/latest.json";
const report = (await Bun.file(path).json()) as Report;

const median = (values: number[]): number | undefined => {
	const sorted = values.filter((value) => Number.isFinite(value)).sort((a, b) => a - b);
	if (sorted.length === 0) return undefined;
	const middle = Math.floor(sorted.length / 2);
	return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1]! + sorted[middle]!) / 2;
};
const format = (value: number | undefined, digits = 1) =>
	value === undefined || !Number.isFinite(value)
		? "–"
		: value.toLocaleString("en-GB", { maximumFractionDigits: digits });
const mib = (bytes: unknown) => (typeof bytes === "number" ? bytes / 1048576 : undefined);
const numberAt = (trial: Trial, key: string) => trial[key] as number | undefined;
const heap = (trial: Trial, key: string) => {
	const value = trial[key] as Record<string, number> | undefined;
	return mib(value?.heapAlloc ?? value?.heapUsed);
};
const peak = (trial: Trial) =>
	Math.max(
		numberAt(trial, "startupPeakTreeRSSMiB") ?? 0,
		...(trial.loads ?? []).map((load) => load.peakTreeRSSMiB)
	);

function groups(trials: Trial[]): Map<string, Trial[]> {
	const out = new Map<string, Trial[]>();
	for (const trial of trials) {
		const key = [
			trial.scenario,
			trial.variant,
			trial.database,
			trial.framework,
			trial.graphql ?? "",
		].join("|");
		out.set(key, [...(out.get(key) ?? []), trial]);
	}
	return out;
}

function memoryTable(trials: Trial[]): void {
	console.log(
		"| Scenario | Variant | DB | Framework | n | Startup ms | Idle RSS | Admin login RSS | Docs RSS | Admin edit RSS | Peak RSS | Live heap idle | Live heap end | Admin edit KB | Admin edit cold ms | Migrate ms |"
	);
	console.log("|---|---|---|---|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|");
	for (const [, items] of groups(trials)) {
		const ok = items.filter((trial) => !trial.startupError && !trial.error);
		const first = items[0]!;
		const pick = (select: (trial: Trial) => number | undefined) =>
			format(median(ok.map((trial) => select(trial) ?? NaN)));
		const failures = items.length - ok.length;
		console.log(
			`| ${first.scenario} | ${first.variant} | ${first.database} | ${first.framework}${failures ? ` (${failures} failed)` : ""} | ${ok.length} | ${pick((t) => numberAt(t, "startupMs"))} | ${pick((t) => numberAt(t, "idleTreeRSSMiB"))} | ${pick((t) => numberAt(t, "afterAdminLoginTreeRSSMiB"))} | ${pick((t) => numberAt(t, "afterDocumentsTreeRSSMiB"))} | ${pick((t) => numberAt(t, "afterAdminEditTreeRSSMiB"))} | ${pick(peak)} | ${pick((t) => heap(t, "idleHeap"))} | ${pick((t) => heap(t, "finalHeap"))} | ${pick((t) => ((t.adminEdit as { bytes?: number })?.bytes ?? NaN) / 1024)} | ${pick((t) => (t.adminEdit as { coldMs?: number })?.coldMs)} | ${pick((t) => numberAt(t, "migrationMs"))} |`
		);
	}
}

function loadTable(trials: Trial[]): void {
	console.log(
		"| Scenario | Variant | DB | Load | Ridu req/s | Payload req/s | Ridu p50/p95/p99 ms | Payload p50/p95/p99 ms | Ridu KB | Payload KB |"
	);
	console.log("|---|---|---|---|--:|--:|---|---|--:|--:|");
	const grouped = groups(trials);
	const keys = new Set([...grouped.keys()].map((key) => key.split("|").slice(0, 3).join("|")));
	for (const key of keys) {
		const [scenario, variant, database] = key.split("|");
		const get = (framework: string) =>
			(
				grouped.get(`${key}|${framework}|${framework === "payload"}`) ??
				grouped.get(`${key}|${framework}|`) ??
				[]
			).filter((trial) => trial.loads);
		const ridu = get("ridu");
		const payload = get("payload");
		const names = new Set(
			[...ridu, ...payload].flatMap((trial) => trial.loads!.map((load) => load.name))
		);
		for (const name of names) {
			const stat = (items: Trial[], select: (load: Load) => number) =>
				median(
					items
						.map((trial) => trial.loads!.find((load) => load.name === name))
						.filter(Boolean)
						.map((load) => select(load!))
				);
			const latency = (items: Trial[]) =>
				items.length === 0
					? "–"
					: `${format(stat(items, (load) => load.latencyMs.p50))} / ${format(stat(items, (load) => load.latencyMs.p95))} / ${format(stat(items, (load) => load.latencyMs.p99))}`;
			console.log(
				`| ${scenario} | ${variant} | ${database} | ${name} | ${format(
					stat(ridu, (load) => load.requestsPerSecond),
					0
				)} | ${format(
					stat(payload, (load) => load.requestsPerSecond),
					0
				)} | ${latency(ridu)} | ${latency(payload)} | ${format((stat(ridu, (load) => load.responseBytes.average) ?? NaN) / 1024)} | ${format((stat(payload, (load) => load.responseBytes.average) ?? NaN) / 1024)} |`
			);
		}
	}
}

if (report.specs) {
	console.log("## Scenarios\n");
	console.log(
		"| Scenario | Blocks | Edges | Max nesting | Collections | Field placements (total) | Block placements (total) | Doc instances | Doc JSON KB |"
	);
	console.log("|---|--:|--:|--:|--:|--:|--:|--:|--:|");
	for (const [name, spec] of Object.entries(report.specs)) {
		console.log(
			`| ${name} | ${spec.blocks} | ${spec.referenceEdges} | ${spec.maxBlockNesting} | ${spec.collections} | ${format(spec.totalRiduPlacements as number, 0)} | ${format(spec.totalBlockPlacements as number, 0)} | ${spec.workloadInstances} | ${format((spec.workloadJSONBytes as number) / 1024)} |`
		);
	}
}
if (report.probes) {
	console.log("\n## Offline probes\n");
	console.log(
		"| Scenario | Ridu accepted | Ridu resolve ms | Ridu resolve alloc MiB | Ridu New ms | Ridu New alloc MiB | Ridu retained heap MiB | Ridu heap objects | Ridu alloc during probe MiB | Manifest KB | /api/schema KB | TS KB | OpenAPI KB | Go KB | Payload Mongoose schemas | Payload Mongo init ms | Payload Mongo heap MiB |"
	);
	console.log("|---|---|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|");
	for (const [name, probe] of Object.entries(report.probes)) {
		const ridu = probe.ridu;
		const generated = (ridu.generated ?? {}) as Record<string, number>;
		const payload = probe.payloadMongoose ?? {};
		console.log(
			`| ${name} | ${ridu.accepted} | ${format(ridu.resolveMs as number, 0)} | ${format(mib(ridu.resolveAllocBytes))} | ${format(ridu.newMs as number, 0)} | ${format(mib(ridu.newAllocBytes))} | ${format(mib(ridu.appHeapAllocBytes))} | ${format(ridu.appHeapObjects as number, 0)} | ${format(mib(ridu.totalAllocBytesDuringProbe), 0)} | ${format((ridu.manifestBytes as number) / 1024)} | ${format((ridu.schemaResponseBytes as number) / 1024)} | ${format(generated.typescript! / 1024, 0)} | ${format(generated.openAPI! / 1024, 0)} | ${format(generated.go! / 1024, 0)} | ${format(payload.mongooseSchemas as number, 0)} | ${format(payload.initMs as number, 0)} | ${format(mib(payload.heapUsedBytes))} |`
		);
	}
}
if (report.generation) {
	console.log("\n## Schema-dependent generation\n");
	console.log("| Scenario | Artifact | Wall ms | Bytes |");
	console.log("|---|---|--:|--:|");
	for (const [name, entries] of Object.entries(report.generation)) {
		for (const [artifact, value] of Object.entries(entries)) {
			console.log(
				`| ${name} | ${artifact} | ${format(value.wallMs, 0)} | ${format(value.bytes ?? undefined, 0)} |`
			);
		}
	}
}
console.log("\n## Memory, startup and admin (medians)\n");
console.log("RSS and heap values are MiB.\n");
memoryTable(report.trials);
console.log("\n## Requests (medians)\n");
loadTable(report.trials);
hotTargetTable(report.trials, report.configuration?.hotTargetProcesses ?? 1);
const physical = new Map<string, unknown>();
for (const trial of report.trials)
	if (trial.physicalSchema)
		physical.set(
			`${trial.scenario} ${trial.variant} ${trial.database} ${trial.framework}`,
			trial.physicalSchema
		);
console.log("\n## Physical schema\n");
for (const [key, value] of physical) console.log(`- ${key}: ${JSON.stringify(value)}`);
if (report.graphql?.length) {
	console.log("\n## GraphQL disabled vs enabled\n");
	console.log(
		"| Framework | GraphQL | n | Startup ms | Idle RSS | Live heap idle | First GraphQL ms | RSS after GraphQL | Heap after GraphQL |"
	);
	console.log("|---|---|--:|--:|--:|--:|--:|--:|--:|");
	for (const [, items] of groups(report.graphql)) {
		const first = items[0]!;
		const ok = items.filter((trial) => !trial.startupError && !trial.error);
		const pick = (select: (trial: Trial) => number | undefined) =>
			format(median(ok.map((trial) => select(trial) ?? NaN)));
		console.log(
			`| ${first.framework} | ${first.graphql} | ${ok.length} | ${pick((t) => numberAt(t, "startupMs"))} | ${pick((t) => numberAt(t, "idleTreeRSSMiB"))} | ${pick((t) => heap(t, "idleHeap"))} | ${pick((t) => (t.firstGraphQL as { ms?: number })?.ms)} | ${pick((t) => numberAt(t, "afterGraphQLTreeRSSMiB"))} | ${pick((t) => heap(t, "afterGraphQLHeap"))} |`
		);
	}
}
const failures = [...report.trials, ...(report.graphql ?? [])].filter(
	(trial) => trial.error || trial.startupError
);
if (failures.length) {
	console.log("\n## Failures\n");
	for (const trial of failures)
		console.log(
			`- ${trial.framework} ${trial.scenario}/${trial.variant}/${trial.database} trial ${trial.trial}: ${String(trial.startupError ?? trial.error).slice(0, 400)}`
		);
}

// The hot-target load edits the media every page save references while those saves run (see
// README). A MongoDB edit starves when any edit fails or times out, any trial's edit p99 exceeds
// one second, or its median edit p95 exceeds five times PostgreSQL's for the same scenario.
function hotTargetTable(trials: Trial[], processes: number): void {
	const rows = new Map<string, { saves: Load[]; edits: Load[]; errors: string[] }>();
	for (const trial of trials) {
		if (trial.framework !== "ridu") continue;
		const loads = trial.loads ?? [];
		const edit = loads.find((load) => load.name === "hot-target-edit");
		const save = loads.find((load) => load.name === "hot-target-save");
		const failed = loads.find((load) => load.name === "hot-target" && load.error);
		if (!edit && !failed) continue;
		const key = [trial.scenario, trial.variant, trial.database].join("|");
		const row = rows.get(key) ?? { saves: [], edits: [], errors: [] };
		if (edit) row.edits.push(edit);
		if (save) row.saves.push(save);
		if (failed?.error) row.errors.push(failed.error);
		rows.set(key, row);
	}
	if (rows.size === 0) return;
	const stat = (loads: Load[], select: (load: Load) => number) => median(loads.map(select));
	const latency = (loads: Load[]) =>
		["p50", "p95", "p99"]
			.map((quantile) =>
				format(
					stat(loads, (load) => load.latencyMs[quantile as "p50" | "p95" | "p99"]),
					0
				)
			)
			.join(" / ");
	const sum = (loads: Load[], select: (load: Load) => number | undefined) =>
		loads.reduce((total, load) => total + (select(load) ?? 0), 0);
	console.log(
		`\n## Hot-target contention (Ridu, ${processes} process${processes === 1 ? "" : "es"}, medians of trials)\n`
	);
	console.log(
		"Edits of the media every page save references, by one client during the save load. Starving: an edit failed or timed out, a trial's edit p99 exceeded 1 s, or the MongoDB median edit p95 exceeded 5x PostgreSQL's.\n"
	);
	console.log(
		"| Scenario | Variant | DB | n | Edits | Edit p50 / p95 / p99 ms | Worst edit p99 / max ms | Edits failed / timed out | Saves | Save p50 / p95 / p99 ms | Saves failed / timed out | Edit p95 vs PostgreSQL | Starving |"
	);
	console.log("|---|---|---|--:|--:|---|---|---|--:|---|---|--:|---|");
	for (const [key, row] of rows) {
		const [scenario, variant, database] = key.split("|");
		const worstP99 = Math.max(...row.edits.map((load) => load.latencyMs.p99));
		const worstMax = Math.max(...row.edits.map((load) => load.latencyMs.max));
		const editFailures = sum(row.edits, (load) => load.failures);
		const editTimeouts = sum(row.edits, (load) => load.timeouts);
		const p95 = stat(row.edits, (load) => load.latencyMs.p95);
		const reference = rows.get([scenario, variant, "postgres"].join("|"));
		const referenceP95 = reference && stat(reference.edits, (load) => load.latencyMs.p95);
		const ratio =
			database !== "postgres" && p95 !== undefined && referenceP95 ? p95 / referenceP95 : undefined;
		const reasons = [
			...(row.errors.length ? ["harness failed"] : []),
			...(editFailures + editTimeouts > 0 ? ["edits failed"] : []),
			...(worstP99 > 1_000 ? ["p99 > 1 s"] : []),
			...(ratio !== undefined && ratio > 5 ? ["p95 > 5x PostgreSQL"] : []),
		];
		console.log(
			`| ${scenario} | ${variant} | ${database} | ${row.edits.length} | ${format(
				stat(row.edits, (load) => load.requests),
				0
			)} | ${latency(row.edits)} | ${format(worstP99, 0)} / ${format(worstMax, 0)} | ${editFailures} / ${editTimeouts} | ${format(
				stat(row.saves, (load) => load.requests),
				0
			)} | ${latency(row.saves)} | ${sum(row.saves, (load) => load.failures)} / ${sum(row.saves, (load) => load.timeouts)} | ${ratio === undefined ? "–" : `${format(ratio, 2)}x`} | ${reasons.length ? `yes: ${reasons.join(", ")}` : "no"} |`
		);
	}
	for (const [key, row] of rows) {
		const failures = [
			...row.errors,
			...[...row.edits, ...row.saves].flatMap((load) =>
				load.firstFailure ? [`${load.name}: ${load.firstFailure}`] : []
			),
		];
		for (const failure of failures)
			console.log(
				`- ${key.replaceAll("|", " ")}: ${failure.replaceAll(/\s+/g, " ").slice(0, 300)}`
			);
	}
}
