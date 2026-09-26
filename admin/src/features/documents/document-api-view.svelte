<script lang="ts">
	import APIReference from "@admin/features/api-reference/api-reference.svelte";
	import {
		isErrorEnvelope,
		isRecord,
		type AdminDocumentV1,
		type AdminReadResultV1,
	} from "@riducms/protocol";
	import { getAdminI18n } from "@riducms/plugin";
	import CheckIcon from "~icons/lucide/check";
	import CopyIcon from "~icons/lucide/copy";
	import RefreshCwIcon from "~icons/lucide/refresh-cw";

	import JsonViewer from "@admin/components/json-tree/json-viewer.svelte";
	import { Banner } from "@admin/components/ui/banner";
	import { CopyFeedback } from "@admin/core/clipboard/copy-feedback.svelte";
	import { Checkbox, Input } from "@riducms/ui";
	import { Select, SelectTrigger, SelectContent, SelectItem } from "@admin/components/ui/select";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import "@admin/features/documents/document-api-view.scss";

	let {
		resourceSlug,
		documentID,
		globalResource = false,
		contentLocale,
		fallbackValue,
		prepared,
	}: {
		resourceSlug: string;
		documentID?: string;
		globalResource?: boolean;
		contentLocale?: string;
		fallbackValue: unknown;
		prepared?: AdminReadResultV1<AdminDocumentV1>;
	} = $props();

	const i18n = getAdminI18n();
	const runtime = getAdminRuntime();
	const id = $props.id();
	const referenceCollection = $derived(
		!globalResource
			? runtime.manifest?.collections.find((collection) => collection.slug === resourceSlug)
			: undefined
	);
	let depth = $state(2);
	let authenticated = $state(true);
	let locale = $state<string>();
	let expanded = $state(false);
	let requestRevision = $state(0);
	let loading = $state(false);
	let responseValue = $state.raw<unknown>();
	let requestError = $state<string>();
	const copyFeedback = new CopyFeedback();
	let adoptedRouteKey = "";
	let preparedRequestKey = "";

	const routeKey = $derived(
		JSON.stringify([resourceSlug, documentID, globalResource, contentLocale])
	);
	const requestKey = $derived(
		JSON.stringify([routeKey, depth, authenticated, locale, requestRevision])
	);
	const requestURL = $derived.by(() => {
		if (!globalResource && documentID === undefined) return "";
		const path = globalResource
			? `/api/globals/${encodeURIComponent(resourceSlug)}`
			: `/api/collections/${encodeURIComponent(resourceSlug)}/${encodeURIComponent(documentID!)}`;
		const url = new URL(path, window.location.origin);
		url.searchParams.set("depth", String(depth));
		if (locale) url.searchParams.set("locale", locale);
		return url.href;
	});

	const currentCopy = $derived(
		copyFeedback.result?.source === requestURL ? copyFeedback.result : undefined
	);

	// A retained route adopts its prepared read once; changing query controls owns a new request.
	$effect.pre(() => {
		if (routeKey === adoptedRouteKey) return;
		adoptedRouteKey = routeKey;
		depth = 2;
		authenticated = true;
		locale = contentLocale;
		expanded = false;
		requestRevision = 0;
		loading = false;
		requestError = prepared?.error?.message;
		responseValue =
			prepared === undefined
				? $state.snapshot(fallbackValue)
				: prepared.error === undefined
					? prepared.value
					: { error: prepared.error.message };
		preparedRequestKey = prepared === undefined ? "" : requestKey;
	});

	$effect(() => {
		const key = requestKey;
		const url = requestURL;
		const includeSession = authenticated;
		if (preparedRequestKey === key) {
			preparedRequestKey = "";
			return;
		}
		if (!url) return;

		const abort = new AbortController();
		execute(url, includeSession, abort.signal);
		return () => abort.abort();
	});

	async function execute(url: string, includeSession: boolean, signal: AbortSignal) {
		loading = true;
		requestError = undefined;
		try {
			const response = await fetch(url, {
				headers: { Accept: "application/json" },
				credentials: includeSession ? "include" : "omit",
				signal,
			});
			const encoded = await response.text();
			if (signal.aborted) return;

			let body: unknown = encoded;
			try {
				body = encoded === "" ? null : JSON.parse(encoded);
			} catch {
				// Keep non-JSON failures inspectable too.
			}
			responseValue = isRecord(body) && "doc" in body ? body.doc : body;
			if (!response.ok) requestError = errorMessage(body, response);
		} catch (cause) {
			if (signal.aborted) return;
			requestError = cause instanceof Error ? cause.message : i18n.t("documents:apiRequestFailed");
			responseValue = { error: requestError };
		} finally {
			if (!signal.aborted) loading = false;
		}
	}

	function errorMessage(value: unknown, response: Response) {
		if (isErrorEnvelope(value)) return value.error.message;
		return `${response.status} ${response.statusText}`.trim();
	}

	function setDepth(event: Event) {
		const next = Number((event.currentTarget as HTMLInputElement).value);
		depth = Number.isFinite(next) ? Math.min(5, Math.max(0, Math.round(next))) : 0;
	}
</script>

<section
	class={["ridu-document-api", expanded && "ridu-document-api--expanded"]}
	aria-label={i18n.t("documents:api")}
>
	<div class="ridu-document-api__configuration" hidden={expanded}>
		<div class="ridu-document-api__url">
			<div class="ridu-document-api__label">
				{i18n.t("documents:apiURL")}
				{#if requestURL}
					<button
						type="button"
						class="ridu-document-api__icon"
						onclick={() => copyFeedback.copy(requestURL)}
						aria-label={i18n.t("documents:copyURL")}
						title={i18n.t(
							currentCopy?.copied
								? "general:copied"
								: currentCopy?.copied === false
									? "general:copyUnavailable"
									: "documents:copyURL"
						)}
					>
						{#if currentCopy?.copied}
							<CheckIcon />
						{:else}
							<CopyIcon />
						{/if}
					</button>
					<span class="ridu-document-api__status" role="status">
						{currentCopy
							? i18n.t(currentCopy.copied ? "general:copied" : "general:copyUnavailable")
							: ""}
					</span>
					<button
						type="button"
						class="ridu-document-api__icon ridu-document-api__refresh"
						disabled={loading}
						onclick={() => requestRevision++}
						aria-label={i18n.t("documents:apiRunRequest")}
						title={i18n.t("documents:apiRunRequest")}
					>
						<RefreshCwIcon />
					</button>
				{/if}
			</div>
			{#if requestURL}
				<a href={requestURL} target="_blank" rel="noopener noreferrer">{requestURL}</a>
			{:else}
				<Banner>{i18n.t("documents:apiRunnableAfterSave")}</Banner>
			{/if}
		</div>

		<div class="ridu-document-api__controls">
			<div class="ridu-document-api__checkboxes">
				<label for="{id}-authenticated">
					<Checkbox id="{id}-authenticated" bind:checked={authenticated} />{i18n.t(
						"documents:apiAuthenticated"
					)}
				</label>
			</div>

			{#if runtime.contentLocales.length > 0}
				<div class="ridu-document-api__field">
					<label for="{id}-locale">{i18n.t("documents:locale")}</label>
					<Select type="single" bind:value={locale}>
						<SelectTrigger id="{id}-locale" aria-label={i18n.t("documents:locale")}>
							{runtime.contentLocales.find((option) => option.code === locale)?.label ??
								locale ??
								i18n.t("documents:locale")}
						</SelectTrigger>
						<SelectContent>
							{#each runtime.contentLocales as option (option.code)}
								<SelectItem value={option.code} label={option.label ?? option.code} />
							{/each}
						</SelectContent>
					</Select>
				</div>
			{/if}

			<div class="ridu-document-api__field">
				<label for="{id}-depth">{i18n.t("documents:apiDepth")}</label>
				<Input
					id="{id}-depth"
					type="number"
					min="0"
					max="5"
					step="1"
					value={depth}
					oninput={setDepth}
				/>
			</div>
		</div>

		{#if referenceCollection}
			{#key routeKey}
				<APIReference collection={referenceCollection} {documentID} />
			{/key}
		{/if}

		{#if requestError}
			<Banner tone="destructive">{requestError}</Banner>
		{/if}
		<span class="ridu-document-api__status" role="status">
			{loading ? i18n.t("documents:apiRequesting") : ""}
		</span>
	</div>

	<JsonViewer value={responseValue} bind:expanded class="ridu-document-api__response" />
</section>
