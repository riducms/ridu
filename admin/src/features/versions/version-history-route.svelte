<script lang="ts" module>
	function withContentLocale(path: string, locale: string | undefined) {
		if (locale === undefined) return path;
		return `${path}?${new URLSearchParams({ locale }).toString()}`;
	}
</script>

<script lang="ts">
	import { Link, useLocation, useNavigate, useParams } from "@hvniel/svelte-router";
	import type { OperationCapabilities, SchemaCollection } from "@riducms/protocol";
	import { untrack } from "svelte";
	import ArrowLeftIcon from "~icons/lucide/arrow-left";
	import CalendarClockIcon from "~icons/lucide/calendar-clock";
	import RotateCcwIcon from "~icons/lucide/rotate-ccw";
	import XIcon from "~icons/lucide/x";

	import { Banner } from "@admin/components/ui/banner";
	import { Button, buttonVariants } from "@admin/components/ui/button";
	import { Checkbox } from "@admin/components/ui/checkbox";
	import { DateValueControl } from "@admin/components/ui/date-value-control";
	import { Pagination } from "@admin/components/ui/pagination";
	import { Select, SelectContent, SelectItem, SelectTrigger } from "@admin/components/ui/select";
	import { Skeleton } from "@admin/components/ui/skeleton";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import {
		documentIDFromAdminPath,
		documentPath,
		documentVersionPath,
		documentVersionsPath,
		globalPath,
		globalVersionPath,
		globalVersionsPath,
	} from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import type {
		AdminDocument,
		AdminScheduledPublish,
		AdminVersion,
	} from "@admin/core/api/admin-client";
	import { formatVersionValue, versionDiffRows } from "@admin/features/versions/version-diff";

	const runtime = getAdminRuntime();
	const notifications = getNotificationCenter();
	const navigate = useNavigate();
	const location = $derived(useLocation());
	const params = $derived(useParams<"collection" | "document" | "global" | "revision">());
	const globalResource = $derived(params.global !== undefined);
	const slug = $derived(params.global ?? params.collection ?? "");
	let collectionDocumentID = $state(
		untrack(() => documentIDFromAdminPath(location.pathname) ?? "")
	);
	$effect(() => {
		if (globalResource) return;
		const decoded = documentIDFromAdminPath(location.pathname);
		if (decoded !== undefined) collectionDocumentID = decoded;
	});
	const documentID = $derived(globalResource ? slug : collectionDocumentID);
	const requestedRevision = $derived(parseRevision(params.revision));
	const collection = $derived(
		(globalResource ? runtime.manifest?.globals : runtime.manifest?.collections)?.find(
			(candidate) => candidate.slug === slug
		)
	);
	const localization = $derived(runtime.manifest?.application.localization);
	const requestedLocale = $derived(new URLSearchParams(location.search).get("locale"));
	const contentLocale = $derived(
		localization?.locales.some((locale) => locale.code === requestedLocale)
			? (requestedLocale ?? localization?.defaultLocale)
			: (runtime.contentLocale ?? localization?.defaultLocale)
	);
	const historyPath = $derived(
		withContentLocale(
			globalResource ? globalVersionsPath(slug) : documentVersionsPath(slug, documentID),
			contentLocale
		)
	);
	const editPath = $derived(
		withContentLocale(
			globalResource ? globalPath(slug) : documentPath(slug, documentID),
			contentLocale
		)
	);

	let versions = $state.raw<AdminVersion[]>([]);
	let currentDocument = $state.raw<AdminDocument>();
	let operations = $state.raw<OperationCapabilities>();
	let schedules = $state.raw<AdminScheduledPublish[]>([]);
	let loading = $state(true);
	let error = $state<string>();
	let page = $state(1);
	let compareRevision = $state("");
	let modifiedOnly = $state(true);
	let restoring = $state(false);
	let scheduling = $state(false);
	let scheduleAt = $state(defaultScheduleTime());

	const selectedVersion = $derived(
		requestedRevision === undefined
			? undefined
			: versions.find((version) => version.Revision === requestedRevision)
	);
	const comparisonVersion = $derived(
		versions.find((version) => version.Revision === Number(compareRevision))
	);
	const comparisonLabel = $derived(
		comparisonVersion === undefined
			? runtime.i18n.t("versions:noComparison")
			: runtime.i18n.t("versions:revisionWithStatus", {
					revision: runtime.i18n.formatNumber(comparisonVersion.Revision),
					status: comparisonVersion.Status,
				})
	);
	const rows = $derived(
		collection === undefined || selectedVersion === undefined
			? []
			: versionDiffRows(collection, selectedVersion, comparisonVersion, runtime.i18n)
	);
	const visibleRows = $derived(modifiedOnly ? rows.filter((row) => row.changed) : rows);
	const pageSize = 10;
	const totalPages = $derived(Math.max(1, Math.ceil(versions.length / pageSize)));
	const pageVersions = $derived(versions.slice((page - 1) * pageSize, page * pageSize));

	$effect(() => {
		const key = `${runtime.manifestRevision}:${slug}:${documentID}:${requestedRevision ?? "list"}:${contentLocale ?? "default"}`;
		if (key.startsWith("0:") || slug === "" || documentID === "") return;
		const request = new AbortController();
		void load(request.signal);
		return () => request.abort();
	});
	$effect(() => {
		if (contentLocale === undefined || requestedLocale === contentLocale) return;
		const next = new URLSearchParams(location.search);
		next.set("locale", contentLocale);
		navigate(`${location.pathname}?${next.toString()}${location.hash}`, { replace: true });
	});
	$effect(() => runtime.registerContentLocaleBlocker(() => restoring || scheduling));

	async function load(signal: AbortSignal) {
		loading = true;
		error = undefined;
		try {
			const [history, detail, document, access, queued] = await Promise.all([
				globalResource
					? runtime.client.globalVersions(slug, { signal, locale: contentLocale })
					: runtime.client.versions(slug, documentID, { signal, locale: contentLocale }),
				requestedRevision === undefined
					? Promise.resolve(undefined)
					: globalResource
						? runtime.client.globalVersion(slug, requestedRevision, {
								signal,
								locale: contentLocale,
							})
						: runtime.client.version(slug, documentID, requestedRevision, {
								signal,
								locale: contentLocale,
							}),
				globalResource
					? runtime.client.global(slug, { signal, locale: contentLocale })
					: runtime.client.find(slug, documentID, { signal, locale: contentLocale }),
				globalResource
					? runtime.client.globalAccess(slug, { signal, locale: contentLocale })
					: runtime.client.collectionAccess(slug, {
							id: documentID,
							signal,
							locale: contentLocale,
						}),
				globalResource
					? Promise.resolve([])
					: runtime.client.scheduledPublishes(slug, documentID, { signal }),
			]);
			if (signal.aborted) return;
			versions = [...history].sort((left, right) => right.Revision - left.Revision);
			if (
				detail !== undefined &&
				!versions.some((version) => version.Revision === detail.Revision)
			) {
				versions = [detail, ...versions].sort((left, right) => right.Revision - left.Revision);
			}
			currentDocument = document;
			operations = access.operations;
			schedules = queued;
			page = Math.min(page, Math.max(1, Math.ceil(versions.length / pageSize)));
			if (detail !== undefined) {
				const index = versions.findIndex((version) => version.Revision === detail.Revision);
				compareRevision = String(
					versions[index + 1]?.Revision ?? versions[index - 1]?.Revision ?? ""
				);
			}
		} catch (cause) {
			if (!signal.aborted) {
				error =
					cause instanceof Error ? cause.message : runtime.i18n.t("versions:historyLoadFailed");
			}
		} finally {
			if (!signal.aborted) loading = false;
		}
	}

	async function restoreSelected(draft = false) {
		const version = selectedVersion;
		const document = currentDocument;
		if (version === undefined || document === undefined) return;
		const destination = withContentLocale(
			globalResource ? globalPath(slug) : documentPath(slug, documentID),
			contentLocale
		);
		restoring = true;
		try {
			const revision = typeof document._revision === "number" ? document._revision : 0;
			if (globalResource) {
				await runtime.client.restoreGlobal(slug, version.Revision, {
					revision,
					draft,
					locale: contentLocale,
				});
			} else {
				await runtime.client.restore(slug, documentID, version.Revision, {
					revision,
					draft,
					locale: contentLocale,
				});
			}
			runtime.documentsChanged();
			notifications.success({
				title: runtime.i18n.t(
					draft ? "versions:revisionRestoredAsDraft" : "versions:revisionRestored",
					{
						revision: runtime.i18n.formatNumber(version.Revision),
					}
				),
				message: runtime.i18n.t("versions:previousCurrentPreserved"),
			});
			navigate(destination);
		} catch (cause) {
			notifications.error({
				title: runtime.i18n.t("versions:revisionNotRestored"),
				message:
					cause instanceof Error ? cause.message : runtime.i18n.t("versions:revisionRestoreFailed"),
			});
		} finally {
			restoring = false;
		}
	}

	async function schedulePublish() {
		const runAt = new Date(scheduleAt);
		if (Number.isNaN(runAt.valueOf()) || runAt <= new Date()) {
			notifications.error({ title: runtime.i18n.t("versions:chooseFuturePublishTime") });
			return;
		}
		scheduling = true;
		try {
			const revision =
				typeof currentDocument?._revision === "number" ? currentDocument._revision : 0;
			await runtime.client.schedulePublish(slug, documentID, runAt, { revision });
			schedules = await runtime.client.scheduledPublishes(slug, documentID);
			scheduleAt = defaultScheduleTime();
			notifications.success({
				title: runtime.i18n.t("versions:publishScheduled"),
				message: formatDate(runAt.toISOString()),
			});
		} catch (cause) {
			notifications.error({
				title: runtime.i18n.t("versions:publishNotScheduled"),
				message:
					cause instanceof Error ? cause.message : runtime.i18n.t("versions:publishScheduleFailed"),
			});
		} finally {
			scheduling = false;
		}
	}

	async function cancelSchedule(job: AdminScheduledPublish) {
		try {
			await runtime.client.cancelScheduledPublish(slug, documentID, job.id);
			schedules = schedules.filter((candidate) => candidate.id !== job.id);
			notifications.success({ title: runtime.i18n.t("versions:scheduledPublishCancelled") });
		} catch (cause) {
			notifications.error({
				title: runtime.i18n.t("versions:scheduleNotCancelled"),
				message:
					cause instanceof Error ? cause.message : runtime.i18n.t("versions:scheduleCancelFailed"),
			});
		}
	}

	function versionPath(version: AdminVersion) {
		return withContentLocale(
			globalResource
				? globalVersionPath(slug, version.Revision)
				: documentVersionPath(slug, documentID, version.Revision),
			contentLocale
		);
	}

	function parseRevision(value: string | undefined) {
		const revision = Number(value);
		return Number.isInteger(revision) && revision > 0 ? revision : undefined;
	}

	function defaultScheduleTime() {
		const date = new Date(Date.now() + 60 * 60 * 1_000);
		date.setSeconds(0, 0);
		return date.toISOString();
	}

	function formatDate(value: string) {
		const date = new Date(value);
		if (Number.isNaN(date.valueOf())) return value;
		return runtime.i18n.formatDate(date, { dateStyle: "medium", timeStyle: "short" });
	}
</script>

<section class="mx-auto w-full max-w-6xl px-5 py-8 sm:px-8 lg:px-11">
	<header class="flex flex-wrap items-start gap-4 border-b border-control-border pb-6">
		<Link
			class={buttonVariants({ variant: "ghost", size: "icon-sm" })}
			to={editPath}
			aria-label={runtime.i18n.t("versions:backToDocument")}
		>
			<ArrowLeftIcon class="size-3.5 rtl:rotate-180" />
		</Link>
		<div class="min-w-0 flex-1 basis-full sm:basis-auto">
			<p class="font-mono text-[9.5px] tracking-[0.15em] text-foreground-faint uppercase">
				{collection === undefined
					? slug
					: runtime.i18n.text(collection.labels.singular, collection.labels.singularTranslations)} · {documentID}
			</p>
			<h1 class="mt-2 text-[28px] leading-tight font-semibold text-foreground">
				{requestedRevision === undefined
					? runtime.i18n.t("versions:history")
					: runtime.i18n.t("versions:revision", {
							revision: runtime.i18n.formatNumber(requestedRevision),
						})}
			</h1>
			<p class="mt-2 text-[13px] text-foreground-sub">
				{selectedVersion === undefined
					? runtime.i18n.t("versions:historyDescription")
					: runtime.i18n.t("versions:saved", {
							date: formatDate(selectedVersion.CreatedAt),
							status: selectedVersion.Status,
						})}
			</p>
		</div>
		{#if selectedVersion !== undefined}
			<Link class={buttonVariants({ variant: "outline" })} to={historyPath}>
				{runtime.i18n.t("versions:allVersions")}
			</Link>
			{#if operations?.update === true}
				<Button disabled={restoring} onclick={() => restoreSelected(false)}>
					<RotateCcwIcon class="size-3.5" />
					{restoring ? runtime.i18n.t("versions:restoring") : runtime.i18n.t("versions:restore")}
				</Button>
				{#if collection?.versionSettings?.drafts === true}
					<Button variant="outline" disabled={restoring} onclick={() => restoreSelected(true)}>
						<RotateCcwIcon class="size-3.5" />
						{runtime.i18n.t("versions:restoreAsDraft")}
					</Button>
				{/if}
			{/if}
		{/if}
	</header>

	{#if error !== undefined}
		<Banner class="mt-6" tone="destructive">{error}</Banner>
	{/if}

	{#if loading}
		<div class="mt-7 grid gap-4"><Skeleton class="h-12" /><Skeleton class="h-72" /></div>
	{:else if collection === undefined}
		<Banner class="mt-6" tone="warning">{runtime.i18n.t("versions:resourceUnavailable")}</Banner>
	{:else if selectedVersion !== undefined}
		<div
			class="mt-7 flex flex-wrap items-end gap-4 rounded-[4px] border border-control-border bg-control p-4"
		>
			<label class="grid min-w-56 gap-1.5 text-[11.5px] text-foreground-sub">
				{runtime.i18n.t("versions:compareRevisionWith", {
					revision: runtime.i18n.formatNumber(selectedVersion.Revision),
				})}
				<Select
					type="single"
					value={compareRevision}
					onValueChange={(revision) => (compareRevision = revision)}
				>
					<SelectTrigger
						size="compact"
						class="w-full"
						aria-label={runtime.i18n.t("versions:comparisonRevision")}
					>
						{comparisonLabel}
					</SelectTrigger>
					<SelectContent>
						<SelectItem value="" label={runtime.i18n.t("versions:noComparison")} />
						{#each versions.filter((version) => version.Revision !== selectedVersion?.Revision) as version (version.ID)}
							<SelectItem
								value={String(version.Revision)}
								label={runtime.i18n.t("versions:revisionWithStatus", {
									revision: runtime.i18n.formatNumber(version.Revision),
									status: version.Status,
								})}
							/>
						{/each}
					</SelectContent>
				</Select>
			</label>
			<label class="mb-2 flex items-center gap-2 text-[12px] text-foreground-muted">
				<Checkbox bind:checked={modifiedOnly} />
				{runtime.i18n.t("versions:modifiedFieldsOnly")}
			</label>
		</div>

		<div class="mt-5 overflow-hidden rounded-[4px] border border-control-border">
			<div
				class="grid grid-cols-[minmax(130px,0.6fr)_minmax(0,1fr)_minmax(0,1fr)] border-b border-control-border bg-control px-4 py-2.5 font-mono text-[9.5px] tracking-[0.08em] text-foreground-faint uppercase"
			>
				<span>{runtime.i18n.t("versions:field")}</span>
				<span>
					{runtime.i18n.t("versions:revision", {
						revision:
							comparisonVersion === undefined
								? "—"
								: runtime.i18n.formatNumber(comparisonVersion.Revision),
					})}
				</span>
				<span>
					{runtime.i18n.t("versions:revision", {
						revision: runtime.i18n.formatNumber(selectedVersion.Revision),
					})}
				</span>
			</div>
			{#if visibleRows.length === 0}
				<p class="px-4 py-10 text-center text-[13px] text-foreground-faint">
					{runtime.i18n.t("versions:noModifiedFields")}
				</p>
			{:else}
				{#each visibleRows as row (row.path)}
					<div
						class={[
							"grid grid-cols-[minmax(130px,0.6fr)_minmax(0,1fr)_minmax(0,1fr)] border-b border-control-border px-4 py-3 last:border-b-0",
							row.changed && "bg-primary/[0.025]",
						]}
					>
						<div class="pe-4">
							<p class="text-[12.5px] text-foreground-muted">{row.label}</p>
							<p class="font-mono mt-1 text-[9px] text-foreground-faint">{row.path}</p>
						</div>
						<pre
							class="font-mono overflow-x-auto pe-5 text-wrap text-[11px] leading-5 text-foreground-sub">{formatVersionValue(
								row.before,
								runtime.i18n
							)}</pre>
						<pre
							class="font-mono overflow-x-auto text-wrap text-[11px] leading-5 text-foreground-body">{formatVersionValue(
								row.after,
								runtime.i18n
							)}</pre>
					</div>
				{/each}
			{/if}
		</div>
	{:else}
		{#if !globalResource && operations?.publish === true}
			<section
				class="mt-7 rounded-[4px] border border-control-border bg-control p-5"
				aria-labelledby="scheduled-publishing"
			>
				<div class="flex items-start gap-3">
					<CalendarClockIcon class="mt-0.5 size-4 text-primary" />
					<div>
						<h2 id="scheduled-publishing" class="text-[14px] font-medium text-foreground">
							{runtime.i18n.t("versions:scheduledPublishing")}
						</h2>
						<p class="mt-1 text-[12px] text-foreground-sub">
							{runtime.i18n.t("versions:scheduledPublishingDescription")}
						</p>
					</div>
				</div>
				<div class="mt-4 flex flex-wrap items-end gap-3">
					<label class="grid min-w-64 gap-1.5 text-[11.5px] text-foreground-sub">
						{runtime.i18n.t("versions:publishDateTime")}
						<DateValueControl
							id="scheduled-publish-at"
							appearance="date-time"
							value={scheduleAt}
							label={runtime.i18n.t("versions:publishDateTime")}
							onValueChange={(next) => (scheduleAt = next)}
						/>
					</label>
					<Button disabled={scheduling} onclick={schedulePublish}>
						{scheduling
							? runtime.i18n.t("versions:scheduling")
							: runtime.i18n.t("versions:schedulePublish")}
					</Button>
				</div>
				{#if schedules.length > 0}
					<ul class="mt-4 divide-y divide-control-border border-t border-control-border">
						{#each schedules as job (job.id)}
							<li class="flex items-center gap-3 py-3 text-[12px]">
								<span class="min-w-0 flex-1 text-foreground-muted">
									{formatDate(job.runAt)}
									<span class="font-mono ms-2 text-[9.5px] text-foreground-faint">
										{runtime.i18n.t("versions:revision", {
											revision: runtime.i18n.formatNumber(job.expectedRevision),
										})}
									</span>
								</span>
								<Button
									size="icon-xs"
									variant="ghost"
									onclick={() => cancelSchedule(job)}
									aria-label={runtime.i18n.t("versions:cancelScheduledPublish")}
								>
									<XIcon class="size-3" />
								</Button>
							</li>
						{/each}
					</ul>
				{/if}
			</section>
		{/if}

		<section class="mt-7" aria-labelledby="revision-list">
			<div class="flex items-end justify-between gap-4">
				<div>
					<h2 id="revision-list" class="text-[14px] font-medium text-foreground">
						{runtime.i18n.t("versions:savedRevisions")}
					</h2>
					<p class="mt-1 text-[12px] text-foreground-sub">
						{runtime.i18n.t("versions:savedRevisionsDescription")}
					</p>
				</div>
				<span class="font-mono text-[10px] text-foreground-faint">
					{runtime.i18n.t("versions:total", {
						count: runtime.i18n.formatNumber(versions.length),
					})}
				</span>
			</div>
			<div class="mt-4 overflow-hidden rounded-[4px] border border-control-border">
				{#if versions.length === 0}
					<p class="px-5 py-12 text-center text-[13px] text-foreground-faint">
						{runtime.i18n.t("versions:noRevisions")}
					</p>
				{:else}
					{#each pageVersions as version (version.ID)}
						<Link
							class="grid grid-cols-[70px_minmax(0,1fr)_120px] items-center border-b border-control-border px-4 py-3 transition-colors last:border-b-0 hover:bg-control"
							to={versionPath(version)}
						>
							<span class="font-mono text-[11px] text-foreground-muted">
								{runtime.i18n.t("versions:shortRevision", {
									revision: runtime.i18n.formatNumber(version.Revision),
								})}
							</span>
							<span class="text-[12.5px] text-foreground-body">
								{formatDate(version.CreatedAt)}
							</span>
							<span
								class="justify-self-end rounded-[3px] border border-control-border bg-control px-2 py-0.5 text-[10px] capitalize text-foreground-sub"
							>
								{version.Status}
							</span>
						</Link>
					{/each}
				{/if}
			</div>
			{#if versions.length > 0}
				<Pagination
					start={(page - 1) * pageSize + 1}
					end={Math.min(page * pageSize, versions.length)}
					total={versions.length}
					{page}
					{totalPages}
					hasPrevious={page > 1}
					hasNext={page < totalPages}
					onPrevious={() => (page -= 1)}
					onNext={() => (page += 1)}
				/>
			{/if}
		</section>
	{/if}
</section>
