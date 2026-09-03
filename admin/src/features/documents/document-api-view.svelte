<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import { onDestroy } from "svelte";
	import CheckIcon from "~icons/lucide/check";
	import CopyIcon from "~icons/lucide/copy";
	import ExternalLinkIcon from "~icons/lucide/external-link";
	import RefreshCwIcon from "~icons/lucide/refresh-cw";

	import CodeBlock from "@admin/components/code-block/code-block.svelte";
	import JsonViewer from "@admin/components/json-tree/json-viewer.svelte";
	import { Banner } from "@admin/components/ui/banner";
	import { Button } from "@admin/components/ui/button";
	import { Checkbox } from "@admin/components/ui/checkbox";
	import { Input } from "@admin/components/ui/input";
	import { Tabs, TabsContent, TabsList, TabsTrigger } from "@admin/components/ui/tabs";

	type APIAction = "list" | "find" | "create" | "update" | "duplicate" | "delete";
	const collectionActions: readonly {
		key: APIAction;
		method: "GET" | "POST" | "PATCH" | "DELETE";
	}[] = [
		{ key: "list", method: "GET" },
		{ key: "find", method: "GET" },
		{ key: "create", method: "POST" },
		{ key: "update", method: "PATCH" },
		{ key: "duplicate", method: "POST" },
		{ key: "delete", method: "DELETE" },
	];
	const i18n = getAdminI18n();

	let {
		resourceSlug,
		resourceLabel,
		documentID,
		globalResource = false,
		fallbackValue,
	}: {
		resourceSlug: string;
		resourceLabel: string;
		documentID?: string;
		globalResource?: boolean;
		fallbackValue: unknown;
	} = $props();

	let depth = $state(2);
	let authenticated = $state(true);
	let requestRevision = $state(0);
	let loading = $state(false);
	// Begin with a non-reactive form snapshot until a live request becomes available.
	// svelte-ignore state_referenced_locally
	let responseValue = $state.raw<unknown>($state.snapshot(fallbackValue));
	let responseStatus = $state<number>();
	let responseStatusText = $state("");
	let responseDuration = $state<number>();
	let responseSize = $state<number>();
	let requestError = $state<string>();
	let example = $state<"fetch" | "sdk" | "curl">("fetch");
	let apiAction = $state<APIAction>("find");
	let copied = $state<"url" | "example">();
	let copyResetTimer: ReturnType<typeof setTimeout> | undefined;

	onDestroy(() => clearTimeout(copyResetTimer));

	const availableActions = $derived(
		globalResource
			? collectionActions.filter((action) => action.key === "find" || action.key === "update")
			: collectionActions
	);
	const selectedAction = $derived(
		availableActions.find((action) => action.key === apiAction) ?? availableActions[0]!
	);
	const runnableAction = $derived(apiAction === "list" || apiAction === "find");
	const liveRequest = $derived(
		runnableAction && (apiAction === "list" || globalResource || documentID !== undefined)
	);
	const collectionEndpoint = $derived(`/api/collections/${encodeURIComponent(resourceSlug)}`);
	const documentEndpoint = $derived(
		`${collectionEndpoint}/${documentID === undefined ? ":id" : encodeURIComponent(documentID)}`
	);
	const endpointPath = $derived.by(() => {
		if (globalResource) return `/api/globals/${encodeURIComponent(resourceSlug)}`;
		if (apiAction === "list" || apiAction === "create") return collectionEndpoint;
		if (apiAction === "duplicate") return `${documentEndpoint}/duplicate`;
		return documentEndpoint;
	});
	const requestURL = $derived.by(() => {
		if (endpointPath.includes(":id")) return "";
		const url = new URL(endpointPath, window.location.origin);
		if (runnableAction) url.searchParams.set("depth", String(depth));
		return url.href;
	});
	const requestExample = $derived(requestExampleFor(example));

	function requestExampleFor(exampleKind: typeof example) {
		if (apiAction !== "find") return actionExample(exampleKind);
		if (exampleKind === "sdk") {
			if (globalResource)
				return `const settings = await client.global(${JSON.stringify(resourceSlug)}, { depth: ${depth} });`;
			return `const document = await client.find(\n  ${JSON.stringify(resourceSlug)},\n  ${JSON.stringify(documentID ?? "document-id")},\n  { depth: ${depth} },\n);`;
		}
		if (exampleKind === "curl") {
			return `curl ${authenticated ? "-H 'Authorization: Bearer <api-key>' \\\n  " : ""}${JSON.stringify(requestURL || endpointPath)}`;
		}
		return `const response = await fetch(${JSON.stringify(requestURL || endpointPath)}, {\n  headers: { Accept: "application/json" },\n  credentials: ${JSON.stringify(authenticated ? "include" : "omit")},\n});\n\nif (!response.ok) throw new Error(\`Request failed: \${response.status}\`);\nconst { doc } = await response.json();`;
	}

	$effect(() => {
		requestRevision;
		const url = requestURL;
		const includeSession = authenticated;
		if (!liveRequest || url === "") {
			responseValue = $state.snapshot(fallbackValue);
			responseStatus = undefined;
			responseStatusText = "";
			responseDuration = undefined;
			responseSize = undefined;
			requestError = undefined;
			return;
		}
		const abort = new AbortController();
		execute(url, includeSession, abort.signal);
		return () => abort.abort();
	});

	async function execute(url: string, includeSession: boolean, signal: AbortSignal) {
		loading = true;
		requestError = undefined;
		const startedAt = performance.now();
		try {
			const response = await fetch(url, {
				headers: { Accept: "application/json" },
				credentials: includeSession ? "include" : "omit",
				signal,
			});
			const encoded = await response.text();
			if (signal.aborted) return;
			responseStatus = response.status;
			responseStatusText = response.statusText;
			responseDuration = Math.round(performance.now() - startedAt);
			responseSize = new TextEncoder().encode(encoded).byteLength;
			let body: unknown = encoded;
			try {
				body = encoded === "" ? null : JSON.parse(encoded);
			} catch {
				// Non-JSON failures remain inspectable in the response viewer.
			}
			responseValue = documentFromEnvelope(body);
			if (!response.ok) requestError = errorMessage(body, response);
		} catch (cause) {
			if (signal.aborted) return;
			responseDuration = Math.round(performance.now() - startedAt);
			responseStatus = undefined;
			responseStatusText = "";
			responseSize = undefined;
			requestError = cause instanceof Error ? cause.message : i18n.t("documents:apiRequestFailed");
			responseValue = { error: requestError };
		} finally {
			if (!signal.aborted) loading = false;
		}
	}

	function documentFromEnvelope(value: unknown) {
		return isRecord(value) && "doc" in value ? value.doc : value;
	}

	function errorMessage(value: unknown, response: Response) {
		if (isRecord(value) && typeof value.message === "string") return value.message;
		if (isRecord(value) && isRecord(value.error) && typeof value.error.message === "string") {
			return value.error.message;
		}
		return `${response.status} ${response.statusText}`.trim();
	}

	function isRecord(value: unknown): value is Record<string, unknown> {
		return value !== null && typeof value === "object" && !Array.isArray(value);
	}

	function actionExample(language: "fetch" | "sdk" | "curl") {
		if (language === "sdk") return sdkActionExample();
		if (language === "curl") return curlActionExample();
		return fetchActionExample();
	}

	function sdkActionExample() {
		if (globalResource) {
			return [
				`const settings = await client.updateGlobal(${JSON.stringify(resourceSlug)}, {`,
				'  siteName: "Updated value",',
				"});",
			].join("\n");
		}
		const slug = JSON.stringify(resourceSlug);
		const id = JSON.stringify(documentID ?? "document-id");
		switch (apiAction) {
			case "list":
				return [
					`const page = await client.list(${slug}, {`,
					"  page: 1,",
					"  limit: 10,",
					`  depth: ${depth},`,
					"});",
				].join("\n");
			case "create":
				return `const document = await client.create(${slug}, {\n  title: "New ${resourceLabel}",\n});`;
			case "update":
				return `const document = await client.update(${slug}, ${id}, {\n  title: "Updated ${resourceLabel}",\n});`;
			case "duplicate":
				return `const copy = await client.duplicate(${slug}, ${id}, {\n  title: "Copy of ${resourceLabel}",\n});`;
			case "delete":
				return `const result = await client.delete(${slug}, ${id});`;
			default:
				return "";
		}
	}

	function fetchActionExample() {
		const hasBody = apiAction === "create" || apiAction === "update" || apiAction === "duplicate";
		const resultName = apiAction === "list" ? "page" : apiAction === "delete" ? "result" : "doc";
		const lines = [
			`const response = await fetch(${JSON.stringify(requestURL || endpointPath)}, {`,
			`  method: ${JSON.stringify(selectedAction.method)},`,
			"  headers: {",
			'    Accept: "application/json",',
		];
		if (hasBody) lines.push('    "Content-Type": "application/json",');
		lines.push("  },", `  credentials: ${JSON.stringify(authenticated ? "include" : "omit")},`);
		if (hasBody) lines.push('  body: JSON.stringify({ title: "Example value" }),');
		lines.push(
			"});",
			"",
			"if (!response.ok) throw new Error(`Request failed: ${response.status}`);"
		);
		lines.push(
			apiAction === "list" || apiAction === "delete"
				? `const ${resultName} = await response.json();`
				: `const { ${resultName} } = await response.json();`
		);
		return lines.join("\n");
	}

	function curlActionExample() {
		const hasBody = apiAction === "create" || apiAction === "update" || apiAction === "duplicate";
		const lines = [`curl -X ${selectedAction.method} \\`, "  -H 'Accept: application/json' \\"];
		if (authenticated) lines.push("  -H 'Authorization: Bearer <api-key>' \\");
		if (hasBody) {
			lines.push(
				"  -H 'Content-Type: application/json' \\",
				'  -d \'{"title":"Example value"}\' \\'
			);
		}
		lines.push(`  ${JSON.stringify(requestURL || endpointPath)}`);
		return lines.join("\n");
	}

	function setDepth(event: Event) {
		const next = Number((event.currentTarget as HTMLInputElement).value);
		depth = Number.isFinite(next) ? Math.min(5, Math.max(0, Math.round(next))) : 0;
	}

	async function copy(kind: "url" | "example", value: string) {
		await navigator.clipboard.writeText(value);
		copied = kind;
		clearTimeout(copyResetTimer);
		copyResetTimer = window.setTimeout(() => {
			if (copied === kind) copied = undefined;
		}, 1_500);
	}

	function formatBytes(value: number | undefined) {
		if (value === undefined) return "";
		return value < 1_024
			? i18n.formatNumber(value, { style: "unit", unit: "byte" })
			: i18n.formatNumber(value / 1_024, {
					maximumFractionDigits: 1,
					style: "unit",
					unit: "kilobyte",
				});
	}

	function actionLabel(action: APIAction) {
		switch (action) {
			case "list":
				return i18n.t("documents:apiListSearch");
			case "find":
				return i18n.t("documents:apiView");
			case "create":
				return i18n.t("documents:apiCreate");
			case "update":
				return i18n.t("documents:apiUpdate");
			case "duplicate":
				return i18n.t("documents:duplicate");
			case "delete":
				return i18n.t("documents:delete");
		}
	}
</script>

<section class="grid gap-6" aria-labelledby="api-heading">
	<header class="grid gap-2">
		<p class="font-mono text-[9.5px] tracking-[0.15em] text-foreground-faint uppercase">
			{i18n.t("documents:liveRESTExplorer")}
		</p>
		<h2
			id="api-heading"
			class="text-[28px] font-semibold leading-tight tracking-[-0.02em] text-foreground-strong"
		>
			{i18n.t("documents:api")}
		</h2>
		<p class="max-w-2xl text-[13px] leading-5 text-foreground-muted">
			{i18n.t("documents:apiDescription", {
				label: resourceLabel.toLocaleLowerCase(i18n.language),
			})}
		</p>
	</header>

	<div class="grid gap-5 xl:grid-cols-[minmax(0,1.65fr)_minmax(340px,0.85fr)] xl:items-start">
		<div class="grid min-w-0 gap-4">
			<div class="overflow-hidden rounded-[4px] border border-control-border bg-background">
				<div
					class="flex min-w-0 flex-wrap items-center gap-2 border-b border-control-border bg-control px-3 py-2.5"
				>
					<span
						class={[
							"rounded-[3px] px-2 py-1 font-mono text-[10px] font-semibold",
							selectedAction.method === "GET" && "bg-success/12 text-success",
							selectedAction.method === "POST" && "bg-primary/12 text-primary",
							selectedAction.method === "PATCH" && "bg-warning/12 text-warning",
							selectedAction.method === "DELETE" && "bg-destructive/12 text-destructive",
						]}
					>
						{selectedAction.method}
					</span>
					<code class="min-w-0 flex-1 truncate text-[11.5px] text-foreground-muted">
						{endpointPath || i18n.t("documents:apiAvailableAfterSave")}
					</code>
					{#if requestURL !== ""}
						<Button variant="ghost" size="xs" onclick={() => copy("url", requestURL)}>
							{#if copied === "url"}<CheckIcon class="size-3" />
								{i18n.t("documents:copied")}{:else}<CopyIcon class="size-3" />
								{i18n.t("documents:copyURL")}{/if}
						</Button>
						<Button
							variant="ghost"
							size="icon-xs"
							href={requestURL}
							target="_blank"
							aria-label={i18n.t("documents:openAPIURL")}
							tooltip={i18n.t("documents:openAPIURL")}
						>
							<ExternalLinkIcon class="size-3.5" />
						</Button>
					{/if}
				</div>

				<div
					class="grid gap-4 border-b border-control-border px-4 py-4 md:grid-cols-[minmax(0,1fr)_auto] md:items-end"
				>
					<div class="flex flex-wrap items-end gap-5">
						{#if runnableAction && !globalResource}
							<label class="grid gap-1.5" for="ridu-api-depth">
								<span class="text-[11.5px] font-medium text-foreground-muted">
									{i18n.t("documents:apiDepth")}
								</span>
								<Input
									id="ridu-api-depth"
									class="w-20 font-mono"
									type="number"
									min="0"
									max="5"
									step="1"
									value={depth}
									oninput={setDepth}
								/>
							</label>
						{/if}
						<label
							class="flex min-h-10 cursor-pointer items-center gap-2 pb-0.5"
							for="ridu-api-authenticated"
						>
							<Checkbox id="ridu-api-authenticated" bind:checked={authenticated} />
							<span class="text-[12.5px] text-foreground-muted">
								{i18n.t("documents:apiAuthenticated")}
							</span>
						</label>
					</div>
					<Button
						variant="outline"
						size="sm"
						disabled={!liveRequest || loading}
						onclick={() => (requestRevision += 1)}
					>
						<RefreshCwIcon class={["size-3.5", loading && "animate-spin"]} />
						{loading
							? i18n.t("documents:apiRequesting")
							: runnableAction
								? i18n.t("documents:apiRunRequest")
								: i18n.t("documents:apiExampleOnly")}
					</Button>
				</div>

				<div
					class="flex min-h-10 flex-wrap items-center gap-x-4 gap-y-1 px-4 py-2 text-[10.5px] text-foreground-faint"
				>
					{#if responseStatus !== undefined}
						<strong class={responseStatus < 400 ? "text-success" : "text-destructive"}>
							{i18n.formatNumber(responseStatus, { useGrouping: false })}
							{responseStatusText}
						</strong>
					{/if}
					{#if responseDuration !== undefined}<span>
							{i18n.formatNumber(responseDuration, {
								style: "unit",
								unit: "millisecond",
							})}
						</span>{/if}
					{#if responseSize !== undefined}<span>{formatBytes(responseSize)}</span>{/if}
					<span>
						{authenticated
							? i18n.t("documents:apiCurrentSession")
							: i18n.t("documents:apiPublicRequest")}
					</span>
				</div>
			</div>

			{const requestBanner = $derived(
				!runnableAction
					? {
							message: i18n.t("documents:apiMutationDescription"),
						}
					: !liveRequest
						? { message: i18n.t("documents:apiRunnableAfterSave") }
						: requestError === undefined
							? undefined
							: { message: requestError, tone: "destructive" as const }
			)}

			{#if requestBanner !== undefined}
				<Banner tone={requestBanner.tone}>{requestBanner.message}</Banner>
			{/if}

			<JsonViewer class="xl:h-[calc(100vh-9rem)] xl:min-h-[620px]" value={responseValue} />
		</div>

		<aside class="grid min-w-0 gap-4 xl:sticky xl:top-4">
			<nav
				class="overflow-hidden rounded-[4px] border border-control-border bg-background"
				aria-label={i18n.t("documents:apiActions")}
			>
				<p
					class="border-b border-control-border bg-control px-3 py-2 font-mono text-[9.5px] tracking-[0.12em] text-foreground-faint uppercase"
				>
					{i18n.t("documents:apiActions")}
				</p>
				<div class="grid grid-cols-2 gap-px bg-control-border p-px xl:grid-cols-1">
					{#each availableActions as action (action.key)}
						<button
							type="button"
							class={[
								"flex min-h-10 items-center gap-2.5 bg-background px-3 text-start text-[12.5px] outline-none transition-colors hover:bg-control focus-visible:bg-control",
								apiAction === action.key && "bg-control text-foreground-strong",
							]}
							onclick={() => (apiAction = action.key)}
							aria-current={apiAction === action.key ? "page" : undefined}
						>
							<span
								class={[
									"w-12 font-mono text-[9px] font-semibold",
									action.method === "GET" && "text-success",
									action.method === "POST" && "text-primary",
									action.method === "PATCH" && "text-warning",
									action.method === "DELETE" && "text-destructive",
								]}
							>
								{action.method}
							</span>
							<span>{actionLabel(action.key)}</span>
						</button>
					{/each}
				</div>
			</nav>
			<Tabs
				bind:value={example}
				class="overflow-hidden rounded-[4px] border border-control-border"
				aria-labelledby="api-example-heading"
			>
				<div
					class="flex flex-wrap items-center justify-between gap-3 border-b border-control-border bg-control px-3 py-2"
				>
					<TabsList
						variant="line"
						class="h-auto gap-1 rounded-none border-0 bg-transparent p-0"
						aria-label={i18n.t("documents:apiExampleLanguage")}
					>
						{#each ["fetch", "sdk", "curl"] as option}
							<TabsTrigger
								value={option}
								class={[
									"h-auto flex-none rounded-[3px] px-2.5 py-1.5 text-[11.5px] capitalize",
									"data-[state=active]:bg-background data-[state=active]:shadow-sm",
								]}
							>
								{option === "sdk" ? "Ridu SDK" : option}
							</TabsTrigger>
						{/each}
					</TabsList>
					<Button variant="ghost" size="xs" onclick={() => copy("example", requestExample)}>
						{#if copied === "example"}<CheckIcon class="size-3" />
							{i18n.t("documents:copied")}{:else}<CopyIcon class="size-3" />
							{i18n.t("documents:copy")}{/if}
					</Button>
				</div>
				<h3 id="api-example-heading" class="sr-only">
					{i18n.t("documents:apiRequestExample")}
				</h3>
				<TabsContent value="fetch" class="mt-0">
					<CodeBlock value={requestExampleFor("fetch")} language="javascript" />
				</TabsContent>
				<TabsContent value="sdk" class="mt-0">
					<CodeBlock value={requestExampleFor("sdk")} language="javascript" />
				</TabsContent>
				<TabsContent value="curl" class="mt-0">
					<CodeBlock value={requestExampleFor("curl")} language="bash" />
				</TabsContent>
			</Tabs>

			<details class="group rounded-[4px] border border-control-border px-4 py-3">
				<summary class="cursor-pointer text-[12.5px] font-medium text-foreground outline-none">
					{i18n.t("documents:apiRequestParameters")}
				</summary>
				<div
					class="mt-3 grid gap-3 border-t border-control-border pt-3 text-[12px] leading-5 text-foreground-muted sm:grid-cols-2"
				>
					{#if runnableAction}
						<p>
							<code class="text-foreground">depth</code>
							{i18n.t("documents:apiDepthDescription")}
						</p>
					{/if}
					<p>
						<code class="text-foreground">authenticated</code>
						{i18n.t("documents:apiAuthenticatedDescription")}
					</p>
				</div>
			</details>
		</aside>
	</div>
</section>
