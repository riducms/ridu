<script lang="ts">
	import { Link, useLocation, useParams } from "@hvniel/svelte-router";
	import type { SchemaField, ValidationIssue } from "@riducms/protocol";
	import { RiduError } from "@riducms/sdk";
	import { tick } from "svelte";
	import ArrowLeftIcon from "~icons/lucide/arrow-left";
	import CheckIcon from "~icons/lucide/check";
	import FileUpIcon from "~icons/lucide/file-up";
	import RotateCcwIcon from "~icons/lucide/rotate-ccw";
	import Trash2Icon from "~icons/lucide/trash-2";
	import LinkIcon from "~icons/lucide/link";

	import { Banner } from "@admin/components/ui/banner";
	import { Button, buttonVariants } from "@admin/components/ui/button";
	import { Input } from "@admin/components/ui/input";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import { collectionPath, documentPath } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import {
		bulkUploadMetadataFields,
		initialBulkUploadValues,
		uploadData,
	} from "@admin/features/uploads/bulk-upload";
	import UploadMetadataFields from "@admin/features/uploads/upload-metadata-fields.svelte";

	type QueueStatus = "queued" | "uploading" | "complete" | "failed" | "uncertain";
	type QueueItem = {
		id: string;
		file: File;
		values: Record<string, string>;
		status: QueueStatus;
		error?: string;
		issues: readonly ValidationIssue[];
		documentID?: string;
	};

	const runtime = getAdminRuntime();
	const notifications = getNotificationCenter();
	const params = $derived(useParams<"collection">());
	const location = $derived(useLocation());
	const slug = $derived(params.collection ?? "");
	const collection = $derived(runtime.manifest?.collections.find((item) => item.slug === slug));
	const requestedLocale = $derived(new URLSearchParams(location.search).get("locale"));
	const contentLocale = $derived(
		runtime.manifest?.application.localization?.locales.some(
			(locale) => locale.code === requestedLocale
		)
			? (requestedLocale ?? undefined)
			: undefined
	);
	const metadataFields = $derived(bulkUploadMetadataFields(collection?.fields ?? []));
	const acceptedTypes = $derived(collection?.uploadSettings?.mimeTypes?.join(",") ?? "");
	const acceptedTypeLabels = $derived(
		runtime.i18n.formatList(collection?.uploadSettings?.mimeTypes ?? [], {
			style: "short",
			type: "disjunction",
		})
	);
	const uploadEnabled = $derived(collection?.capabilities.upload === true);
	let queue = $state<QueueItem[]>([]);
	let running = $state(false);
	let accessLoading = $state(true);
	let canCreate = $state(false);
	let accessError = $state<string>();
	let remoteURL = $state("");
	let remoteValues = $state<Record<string, string>>({});
	let remotePending = $state(false);
	let remoteError = $state<string>();
	let remoteOutcomeUncertain = $state(false);
	let remoteAttemptURL = "";
	let remoteIssues = $state<readonly ValidationIssue[]>([]);
	let remoteDocumentID = $state<string>();
	let nextID = 0;
	let pageElement: HTMLElement;
	let queueRequest: AbortController | undefined;
	let remoteRequest: AbortController | undefined;
	const completed = $derived(queue.filter((item) => item.status === "complete").length);
	const failed = $derived(queue.filter((item) => item.status === "failed").length);
	const uncertain = $derived(queue.filter((item) => item.status === "uncertain").length);
	const pending = $derived(queue.filter((item) => item.status === "queued").length);
	const currentUpload = $derived(queue.find((item) => item.status === "uploading"));

	$effect(() => {
		const currentSlug = slug;
		queueRequest?.abort();
		remoteRequest?.abort();
		queueRequest = undefined;
		remoteRequest = undefined;
		queue = [];
		running = false;
		canCreate = false;
		remoteURL = "";
		remoteValues = {};
		remotePending = false;
		remoteError = undefined;
		remoteOutcomeUncertain = false;
		remoteAttemptURL = "";
		remoteIssues = [];
		remoteDocumentID = undefined;
		const request = new AbortController();
		accessLoading = true;
		accessError = undefined;
		runtime.client
			.collectionAccess(currentSlug, { signal: request.signal, locale: contentLocale })
			.then((access) => {
				if (!request.signal.aborted) canCreate = access.operations.create;
			})
			.catch((cause: unknown) => {
				if (!request.signal.aborted) {
					accessError =
						cause instanceof Error ? cause.message : runtime.i18n.t("uploads:accessCheckFailed");
				}
			})
			.finally(() => {
				if (!request.signal.aborted) accessLoading = false;
			});
		return () => {
			request.abort();
			queueRequest?.abort();
			remoteRequest?.abort();
		};
	});

	function addFiles(event: Event) {
		if (!uploadEnabled || !canCreate || running) return;
		const input = event.currentTarget as HTMLInputElement;
		const files = Array.from(input.files ?? []);
		const known = new Set(queue.map((item) => fileIdentity(item.file)));
		for (const file of files) {
			if (known.has(fileIdentity(file))) continue;
			known.add(fileIdentity(file));
			queue.push({
				id: `upload-${++nextID}`,
				file,
				values: initialBulkUploadValues(file.name, metadataFields, collection?.admin.useAsTitle),
				status: "queued",
				issues: [],
			});
		}
		input.value = "";
	}

	function removeItem(id: string) {
		queue = queue.filter((item) => item.id !== id);
	}

	function refreshQueue() {
		queue = [...queue];
	}

	async function retry(item: QueueItem, trigger: HTMLElement) {
		if (running || !uploadEnabled || !canCreate) return;
		const row = trigger.closest<HTMLElement>("[data-upload-row]");
		item.status = "queued";
		item.error = undefined;
		item.issues = [];
		refreshQueue();
		await tick();
		row?.focus();
		await runUploads([item]);
		await tick();
		const finalStatus = item.status as QueueStatus;
		if (finalStatus === "complete") {
			row?.querySelector<HTMLElement>("[data-upload-result-action]")?.focus();
		} else if (finalStatus === "failed" && item.issues.length === 0) {
			row?.querySelector<HTMLElement>("[data-upload-result-action]")?.focus();
		} else if (finalStatus === "uncertain") {
			row?.focus();
		}
	}

	async function uploadAll() {
		await runUploads(queue);
	}

	async function runUploads(items: readonly QueueItem[]) {
		if (running || !uploadEnabled || !canCreate) return;
		const currentSlug = slug;
		const request = new AbortController();
		queueRequest?.abort();
		queueRequest = request;
		running = true;
		for (const item of items) {
			if (request.signal.aborted) return;
			if (item.status !== "queued") continue;
			item.status = "uploading";
			item.error = undefined;
			item.issues = [];
			refreshQueue();
			try {
				const data = uploadData(item.values);
				const document = await runtime.client.upload(currentSlug, item.file, {
					data,
					signal: request.signal,
					locale: contentLocale,
				});
				if (request.signal.aborted || slug !== currentSlug) return;
				item.status = "complete";
				item.documentID = document.id;
				refreshQueue();
				runtime.documentsChanged();
			} catch (cause) {
				if (request.signal.aborted) return;
				const confirmed = cause instanceof RiduError && cause.status < 500;
				item.status = confirmed ? "failed" : "uncertain";
				item.error = confirmed ? cause.message : runtime.i18n.t("uploads:outcomeNotConfirmed");
				item.issues = confirmed ? cause.issues : [];
				refreshQueue();
			}
		}
		if (request.signal.aborted || slug !== currentSlug) return;
		running = false;
		queueRequest = undefined;
		const failures = queue.filter((item) => item.status === "failed").length;
		const successes = queue.filter((item) => item.status === "complete").length;
		const uncertainOutcomes = queue.filter((item) => item.status === "uncertain").length;
		if (failures === 0 && uncertainOutcomes === 0 && successes > 0) {
			notifications.success({
				title: runtime.i18n.t("uploads:assetsUploaded", { count: successes }),
			});
		} else if (failures > 0 || uncertainOutcomes > 0) {
			notifications.error({
				title: runtime.i18n.t("uploads:needAttention", {
					count: failures + uncertainOutcomes,
				}),
				message:
					uncertainOutcomes > 0
						? runtime.i18n.t("uploads:unknownOutcomesDescription")
						: runtime.i18n.t("uploads:correctMetadata"),
			});
			if (failures > 0) await focusFirstInvalid();
		}
	}

	async function uploadRemote(event: SubmitEvent) {
		event.preventDefault();
		if (remotePending || !uploadEnabled || !canCreate || remoteURL.trim() === "") return;
		const currentSlug = slug;
		const request = new AbortController();
		remoteRequest?.abort();
		remoteRequest = request;
		remotePending = true;
		remoteError = undefined;
		remoteOutcomeUncertain = false;
		remoteAttemptURL = remoteURL.trim();
		remoteIssues = [];
		remoteDocumentID = undefined;
		try {
			const defaults = initialBulkUploadValues(
				remoteFilename(remoteURL),
				metadataFields,
				collection?.admin.useAsTitle
			);
			const document = await runtime.client.uploadFromURL(currentSlug, remoteURL.trim(), {
				data: uploadData({ ...defaults, ...remoteValues }),
				signal: request.signal,
				locale: contentLocale,
			});
			if (request.signal.aborted || slug !== currentSlug) return;
			remoteDocumentID = document.id;
			runtime.documentsChanged();
			notifications.success({ title: runtime.i18n.t("uploads:remoteAssetUploaded") });
		} catch (cause) {
			if (request.signal.aborted) return;
			const confirmed = cause instanceof RiduError && cause.status < 500;
			remoteOutcomeUncertain = !confirmed;
			remoteError = confirmed ? cause.message : runtime.i18n.t("uploads:remoteOutcomeNotConfirmed");
			remoteIssues = confirmed ? cause.issues : [];
			if (remoteIssues.length > 0) await focusFirstInvalid("[data-remote-upload]");
		} finally {
			if (!request.signal.aborted && slug === currentSlug) {
				remotePending = false;
				remoteRequest = undefined;
			}
		}
	}

	function clearItemIssues(item: QueueItem, path: string, name: string) {
		item.issues = item.issues.filter((issue) => issue.path !== path && issue.path !== name);
		if (item.issues.length === 0 && item.status === "failed") item.error = undefined;
		refreshQueue();
	}

	function clearRemoteIssues(path: string, name: string) {
		remoteIssues = remoteIssues.filter((issue) => issue.path !== path && issue.path !== name);
		if (remoteIssues.length === 0) remoteError = undefined;
	}

	function updateRemoteMetadata(field: SchemaField, value: string) {
		remoteValues[field.name] = value;
		clearRemoteIssues(field.path, field.name);
	}

	function updateItemMetadata(item: QueueItem, field: SchemaField, value: string) {
		item.values = { ...item.values, [field.name]: value };
		clearItemIssues(item, field.path, field.name);
	}

	function updateRemoteURL(value: string) {
		remoteURL = value;
		if (remoteOutcomeUncertain && value.trim() !== remoteAttemptURL) {
			remoteOutcomeUncertain = false;
			remoteError = undefined;
		}
	}

	async function focusFirstInvalid(scope = "") {
		await tick();
		const selector = scope === "" ? '[aria-invalid="true"]' : `${scope} [aria-invalid="true"]`;
		const invalid = pageElement?.querySelector<HTMLElement>(selector);
		invalid?.scrollIntoView({ behavior: "smooth", block: "center" });
		invalid?.focus({ preventScroll: true });
	}

	function fileIdentity(file: File) {
		return `${file.name}\u0000${file.size}\u0000${file.lastModified}`;
	}

	function remoteFilename(value: string) {
		try {
			const filename = new URL(value).pathname.split("/").filter(Boolean).at(-1);
			return filename === undefined ? "remote-upload" : decodeURIComponent(filename);
		} catch {
			return "remote-upload";
		}
	}

	function formatBytes(value: number) {
		if (value < 1_024) return runtime.i18n.formatNumber(value, { style: "unit", unit: "byte" });
		if (value < 1_048_576)
			return runtime.i18n.formatNumber(value / 1_024, {
				maximumFractionDigits: 0,
				style: "unit",
				unit: "kilobyte",
			});
		return runtime.i18n.formatNumber(value / 1_048_576, {
			maximumFractionDigits: 1,
			style: "unit",
			unit: "megabyte",
		});
	}

	function statusLabel(status: QueueStatus) {
		return runtime.i18n.t(
			`uploads:status${status[0]?.toLocaleUpperCase("en-US")}${status.slice(1)}` as
				| "uploads:statusQueued"
				| "uploads:statusUploading"
				| "uploads:statusComplete"
				| "uploads:statusFailed"
				| "uploads:statusUncertain"
		);
	}

	function localizedPath(path: string) {
		return contentLocale === undefined
			? path
			: `${path}?${new URLSearchParams({ locale: contentLocale })}`;
	}
</script>

<section
	bind:this={pageElement}
	class="mx-auto w-full max-w-[1020px] px-5 py-9 sm:px-8 sm:py-11 lg:px-11 lg:pb-22.5"
	aria-busy={running || remotePending}
>
	<header class="flex flex-wrap items-end justify-between gap-5">
		<div>
			<Link
				class="mb-5 inline-flex items-center gap-1.5 text-[12px] text-foreground-faint hover:text-foreground"
				to={localizedPath(collectionPath(slug))}
			>
				<ArrowLeftIcon class="size-3.5 rtl:rotate-180" />
				{collection === undefined
					? slug
					: runtime.i18n.text(collection.labels.plural, collection.labels.pluralTranslations)}
			</Link>
			<h1 class="font-serif text-[34px] leading-none tracking-[0.004em] text-foreground">
				{runtime.i18n.t("uploads:bulkUpload")}
			</h1>
			<p class="mt-2 max-w-xl text-[13px] leading-5 text-foreground-sub">
				{runtime.i18n.t("uploads:bulkUploadDescription")}
			</p>
		</div>
		<div class="flex items-center gap-2">
			{#if queue.length > 0}
				<span
					class="font-mono me-2 text-[10px] text-foreground-faint"
					role="status"
					aria-live="polite"
				>
					{runtime.i18n.t("uploads:queueSummary", {
						current:
							currentUpload === undefined
								? ""
								: runtime.i18n.t("uploads:uploadingFilename", {
										filename: currentUpload.file.name,
									}),
						complete: runtime.i18n.formatNumber(completed),
						queued: runtime.i18n.formatNumber(pending),
						failed: runtime.i18n.formatNumber(failed),
						uncertain: runtime.i18n.formatNumber(uncertain),
					})}
				</span>
			{/if}
			<Button
				disabled={running || pending === 0 || !uploadEnabled || !canCreate}
				onclick={uploadAll}
			>
				<FileUpIcon class="size-3.5" />
				{running
					? runtime.i18n.t("uploads:uploading")
					: runtime.i18n.t("uploads:uploadCount", {
							count: pending === 0 ? runtime.i18n.t("uploads:files") : pending,
						})}
			</Button>
		</div>
	</header>

	{const accessNotice = $derived(
		collection !== undefined && collection.capabilities.upload !== true
			? {
					message: runtime.i18n.t("uploads:notConfigured"),
					tone: "destructive" as const,
				}
			: accessError !== undefined
				? { message: accessError, tone: "destructive" as const }
				: !accessLoading && !canCreate
					? {
							message: runtime.i18n.t("uploads:createPermissionDenied"),
							tone: "warning" as const,
						}
					: undefined
	)}

	{#if accessNotice !== undefined}
		<Banner class="mt-7" tone={accessNotice.tone}>{accessNotice.message}</Banner>
	{/if}

	<label
		class="mt-7 grid min-h-36 cursor-pointer place-items-center rounded-[4px] border border-dashed border-control-border-hover bg-control px-5 py-7 text-center transition-colors hover:border-primary/55 hover:bg-primary/[0.035]"
	>
		<input
			class="sr-only"
			type="file"
			multiple
			accept={acceptedTypes}
			onchange={addFiles}
			disabled={!uploadEnabled || !canCreate || running}
		/>
		<span>
			<FileUpIcon class="mx-auto size-5 text-foreground-faint" />
			<span class="mt-3 block text-[13.5px] font-medium text-foreground-muted">
				{runtime.i18n.t("uploads:chooseFilesForQueue")}
			</span>
			<span class="mt-1 block text-[11.5px] text-foreground-faint">
				{acceptedTypeLabels || runtime.i18n.t("uploads:configuredTypes")}
			</span>
		</span>
	</label>

	<form
		class="mt-4 grid gap-4 rounded-[4px] border border-control-border bg-control p-5"
		data-remote-upload
		onsubmit={uploadRemote}
	>
		<div class="flex items-start gap-3">
			<div
				class="grid size-9 shrink-0 place-items-center rounded-[3px] border border-control-border bg-background"
			>
				<LinkIcon class="size-4 text-foreground-faint" aria-hidden="true" />
			</div>
			<div>
				<h2 class="text-[13.5px] font-medium text-foreground">
					{runtime.i18n.t("uploads:pasteURL")}
				</h2>
				<p class="mt-0.5 text-[11.5px] leading-4 text-foreground-faint">
					{runtime.i18n.t("uploads:remoteImportDescription")}
				</p>
			</div>
		</div>
		<label class="grid gap-1.5 text-[11px] text-foreground-sub" for="remote-upload-url">
			<span>{runtime.i18n.t("uploads:assetURL")}</span>
			<Input
				id="remote-upload-url"
				type="url"
				placeholder="https://cdn.example.com/image.jpg"
				value={remoteURL}
				oninput={(event) => updateRemoteURL(event.currentTarget.value)}
				disabled={!uploadEnabled || !canCreate || remotePending}
				required
			/>
		</label>
		{#if metadataFields.length > 0}
			<UploadMetadataFields
				fields={metadataFields}
				values={remoteValues}
				issues={[...remoteIssues]}
				idPrefix="remote-upload"
				disabled={!uploadEnabled || !canCreate || remotePending}
				labelPrefix={runtime.i18n.t("uploads:urlImportMetadata")}
				onValueChange={updateRemoteMetadata}
			/>
		{/if}
		{#if remoteError !== undefined}<Banner tone="destructive">{remoteError}</Banner>{/if}
		<div class="flex flex-wrap items-center justify-between gap-3">
			{#if remoteDocumentID !== undefined}
				<Link
					class={buttonVariants({ variant: "ghost", size: "sm" })}
					to={localizedPath(documentPath(slug, remoteDocumentID))}
				>
					{runtime.i18n.t("uploads:openImportedAsset")}
				</Link>
			{:else}<span></span>{/if}
			<Button
				type="submit"
				disabled={!uploadEnabled ||
					!canCreate ||
					remotePending ||
					remoteOutcomeUncertain ||
					remoteURL.trim() === ""}
			>
				<LinkIcon class="size-3.5" aria-hidden="true" />
				{remotePending
					? runtime.i18n.t("uploads:importing")
					: runtime.i18n.t("uploads:importFromURL")}
			</Button>
		</div>
	</form>

	{#if queue.length === 0}
		<div
			class="mt-7 rounded-[4px] border border-control-border px-5 py-8 text-center text-[12.5px] text-foreground-faint"
		>
			{runtime.i18n.t("uploads:noFilesQueued")}
		</div>
	{:else}
		<ul
			class="mt-7 grid gap-3"
			aria-label={runtime.i18n.t("uploads:uploadQueue")}
			aria-busy={running}
		>
			{#each queue as item (item.id)}
				<li
					class="rounded-[4px] border border-control-border bg-control p-4"
					data-upload-row
					tabindex="-1"
				>
					<div class="flex items-start gap-4">
						<div
							class="grid size-10 shrink-0 place-items-center rounded-[3px] border border-control-border bg-background"
						>
							{#if item.status === "complete"}<CheckIcon
									class="size-4 text-success"
								/>{:else}<FileUpIcon class="size-4 text-foreground-faint" />{/if}
						</div>
						<div class="min-w-0 flex-1">
							<div class="flex flex-wrap items-start justify-between gap-2">
								<div class="min-w-0">
									<p class="truncate text-[13px] font-medium text-foreground">{item.file.name}</p>
									<p class="font-mono mt-1 text-[9.5px] text-foreground-faint">
										{formatBytes(item.file.size)} · {statusLabel(item.status)}
									</p>
								</div>
								<div class="flex items-center gap-1">
									{#if item.status === "complete" && item.documentID !== undefined}
										<Link
											class={buttonVariants({ variant: "ghost", size: "xs" })}
											to={localizedPath(documentPath(slug, item.documentID))}
											data-upload-result-action
										>
											{runtime.i18n.t("uploads:openAsset")}
										</Link>
									{:else if item.status === "failed"}
										<Button
											variant="ghost"
											size="icon-xs"
											onclick={(event) => retry(item, event.currentTarget)}
											aria-label={runtime.i18n.t("uploads:retryFilename", {
												filename: item.file.name,
											})}
											data-upload-result-action
											disabled={running}
										>
											<RotateCcwIcon />
										</Button>
									{/if}
									{#if item.status === "queued" || item.status === "failed"}
										<Button
											variant="ghost"
											size="icon-xs"
											onclick={() => removeItem(item.id)}
											aria-label={runtime.i18n.t("uploads:removeFilename", {
												filename: item.file.name,
											})}
											disabled={running}
										>
											<Trash2Icon />
										</Button>
									{/if}
								</div>
							</div>

							{#if metadataFields.length > 0}
								<div class="mt-4">
									<UploadMetadataFields
										fields={metadataFields}
										values={item.values}
										issues={[...item.issues]}
										idPrefix={item.id}
										disabled={item.status === "uploading" || item.status === "complete"}
										onValueChange={(field, value) => updateItemMetadata(item, field, value)}
									/>
								</div>
							{/if}
							{#if item.error !== undefined}<p class="mt-3 text-[11.5px] text-destructive">
									{item.error}
								</p>{/if}
						</div>
					</div>
				</li>
			{/each}
		</ul>
	{/if}
</section>
