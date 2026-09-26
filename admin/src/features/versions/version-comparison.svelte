<script lang="ts">
	import { Dialog } from "bits-ui";
	import { untrack } from "svelte";
	import type { SchemaCollection } from "@riducms/protocol";
	import { Button, buttonVariants } from "@riducms/ui";
	import { Banner } from "@admin/components/ui/banner";
	import { Checkbox } from "@riducms/ui";
	import { Select, SelectContent, SelectItem, SelectTrigger } from "@admin/components/ui/select";
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuTrigger,
	} from "@admin/components/ui/dropdown-menu";
	import { Popover, PopoverContent, PopoverTrigger } from "@admin/components/ui/popover";
	import { ConfirmationDialog } from "@admin/components/ui/confirmation-dialog";
	import Chevron from "@admin/components/icons/chevron.svelte";
	import XIcon from "@admin/components/icons/x.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import type { AdminDocument } from "@admin/core/api/admin-client";
	import type { VersionHistoryController } from "@admin/features/versions/version-history-controller.svelte";
	import { versionDate, versionStatus } from "@admin/features/versions/version-history";
	import {
		versionDiffRows,
		versionReferences,
		type VersionDiffRow,
	} from "@admin/features/versions/version-diff";
	import VersionDiffFields from "@admin/features/versions/version-diff-fields.svelte";
	import VersionTable from "@admin/features/versions/version-table.svelte";

	let {
		controller,
		collection,
		search,
		onQueryChange,
		onRestored,
	}: {
		controller: VersionHistoryController;
		collection: SchemaCollection;
		search: URLSearchParams;
		onQueryChange: (changes: Record<string, string>) => void;
		onRestored: () => void;
	} = $props();

	const runtime = getAdminRuntime();
	const i18n = runtime.i18n;
	const selected = $derived(controller.selectedVersion!);
	const status = $derived(versionStatus(selected, controller.document));
	const comparisonOptions = $derived(
		controller.versions.filter((version) => version.Revision !== selected.Revision)
	);
	const previous = $derived(
		comparisonOptions.find((version) => version.Revision < selected.Revision)
	);
	const publishedComparison = $derived(
		comparisonOptions.find((version) => version.Status === "published")
	);
	const comparisonSummary = $derived(
		comparisonOptions.find((version) => version.Revision === Number(search.get("compare"))) ??
			previous ??
			comparisonOptions[0]
	);
	const selectedSummary = $derived(
		controller.versions.find((version) => version.Revision === selected.Revision) ?? selected
	);
	const modifiedOnly = $derived(search.get("modifiedOnly") !== "false");
	const locales = $derived(runtime.contentLocales.map((locale) => locale.code));
	const requestedLocales = $derived(
		search.has("locales")
			? locales.filter((locale) => search.get("locales")!.split(",").includes(locale))
			: locales
	);
	const selectedLocales = $derived(requestedLocales.length ? requestedLocales : locales);
	const choices = $derived(
		comparisonOptions.filter(
			(version) =>
				version === comparisonSummary ||
				version === previous ||
				version === comparisonOptions[0] ||
				version === publishedComparison
		)
	);
	const comparisonView = $derived.by(() => {
		const state = controller.comparisonState;
		const current = state?.revision === comparisonSummary?.Revision ? state : undefined;
		const ready = current?.status === "ready";
		return {
			current: ready ? selected : selectedSummary,
			comparison: ready ? current.version : comparisonSummary,
			error: current?.status === "error" ? current.error : undefined,
			locales: ready && locales.length ? selectedLocales : undefined,
			ready,
		};
	});

	let drawerOpen = $state(false);
	let drawerSearch = $state.raw(new URLSearchParams());
	let restoreOpen = $state(false);
	let restoreAsDraft = false;
	let references = $state.raw<ReadonlyMap<string, AdminDocument>>(new Map());
	const rows = $derived(
		versionDiffRows(
			collection,
			comparisonView.current,
			comparisonView.comparison,
			i18n,
			comparisonView.locales
		)
	);

	$effect(() => {
		const summary = comparisonSummary;
		untrack(() => controller.syncComparison(summary));
	});

	// Reference labels are authorized current reads; IDs survive deleted or unreadable targets.
	$effect(() => {
		if (!comparisonView.ready && locales.length) {
			references = new Map();
			return;
		}
		const request = new AbortController();
		const needed = new Map<string, { collection: string; id: string }>();
		function collect(items: VersionDiffRow[]) {
			for (const row of items) {
				if (modifiedOnly && !row.changed) continue;
				if (row.children) collect(row.children);
				else
					for (const value of [row.before, row.after])
						for (const reference of versionReferences(row, value))
							needed.set(reference.key, reference);
			}
		}
		collect(rows);
		references = new Map();
		loadReferences(needed, request.signal);
		return () => request.abort();
	});

	async function loadReferences(
		needed: Map<string, { collection: string; id: string }>,
		signal: AbortSignal
	) {
		const entries = await Promise.all(
			[...needed].map(async ([key, target]) => {
				try {
					return [
						key,
						await runtime.client.find(target.collection, target.id, {
							signal,
							locale: runtime.contentLocale,
						}),
					] as const;
				} catch {
					return undefined;
				}
			})
		);
		if (!signal.aborted) references = new Map(entries.filter((entry) => entry !== undefined));
	}

	function selectComparison(value: string) {
		if (value === "more") {
			drawerOpen = true;
			return;
		}
		onQueryChange({ compare: value });
	}

	function requestRestore(draft: boolean) {
		restoreAsDraft = draft;
		restoreOpen = true;
	}

	function relativeDate(value: string) {
		const seconds = (new Date(value).valueOf() - Date.now()) / 1000;
		if (!Number.isFinite(seconds)) return "";
		const unit = Math.abs(seconds) >= 86400 ? "day" : Math.abs(seconds) >= 3600 ? "hour" : "minute";
		return i18n.formatRelativeTime(
			Math.round(seconds / (unit === "day" ? 86400 : unit === "hour" ? 3600 : 60)),
			unit
		);
	}
</script>

<div class="ridu-version-comparison">
	<div class="ridu-version-comparison__toolbar">
		<h2>{i18n.t("versions:compareVersions")}</h2>
		<div class="ridu-version-comparison__options">
			<label>
				<Checkbox
					checked={modifiedOnly}
					onCheckedChange={(value) => onQueryChange({ modifiedOnly: String(value) })}
				/>{i18n.t("versions:modifiedFieldsOnly")}
			</label>
			{#if locales.length && comparisonView.ready}
				<Popover>
					<PopoverTrigger class="ridu-version-comparison__locales">
						{i18n.t("versions:locales")}: {selectedLocales.join(", ")}<Chevron />
					</PopoverTrigger>
					<PopoverContent align="end" class="ridu-version-comparison__locale-menu">
						{#each runtime.contentLocales as locale}
							<label>
								<Checkbox
									checked={selectedLocales.includes(locale.code)}
									disabled={selectedLocales.length === 1 && selectedLocales.includes(locale.code)}
									onCheckedChange={(checked) =>
										onQueryChange({
											locales: (checked
												? [...selectedLocales, locale.code]
												: selectedLocales.filter((code) => code !== locale.code)
											).join(","),
										})}
								/>{locale.label ?? locale.code}
							</label>
						{/each}
					</PopoverContent>
				</Popover>
			{/if}
		</div>
	</div>

	<div class="ridu-version-comparison__controls">
		<div class="ridu-version-comparison__from">
			<div class="ridu-version-comparison__caption">
				<span>{i18n.t("versions:comparingAgainst")}</span>
				<span>{comparisonSummary ? relativeDate(comparisonSummary.CreatedAt) : ""}</span>
			</div>
			<Select
				type="single"
				value={comparisonSummary ? String(comparisonSummary.Revision) : ""}
				onValueChange={selectComparison}
			>
				<SelectTrigger
					class="ridu-version-comparison__select"
					aria-label={i18n.t("versions:comparisonRevision")}
				>
					{#if comparisonSummary}
						<span>
							{comparisonSummary === previous
								? i18n.t("versions:previousVersion")
								: i18n.t(`versions:${versionStatus(comparisonSummary, controller.document)}`)}
						</span>
						<span class="ridu-version-comparison__date">
							{versionDate(comparisonSummary.CreatedAt, i18n)}
						</span>
					{:else}
						{i18n.t("versions:noComparison")}
					{/if}
				</SelectTrigger>
				<SelectContent class="ridu-version-comparison__choices">
					{#each choices as version (version.ID)}
						<SelectItem
							value={String(version.Revision)}
							label={`${i18n.t(version === previous ? "versions:previousVersion" : `versions:${versionStatus(version, controller.document)}`)} ${versionDate(version.CreatedAt, i18n)}`}
						/>
					{/each}
					<SelectItem value="more" label={i18n.t("versions:moreVersions")} />
				</SelectContent>
			</Select>
		</div>
		<div class="ridu-version-comparison__to">
			<div class="ridu-version-comparison__caption">
				<span>{i18n.t("versions:currentlyViewing")}</span>
				<span>{relativeDate(selected.CreatedAt)}</span>
			</div>
			<div class="ridu-version-comparison__current">
				<div class="ridu-version-comparison__current-label">
					<span
						class={[
							"ridu-version-status",
							status === "currentlyPublished" && "ridu-version-status--published",
						]}
					>
						{i18n.t(`versions:${status}`)}
					</span>
					<span class="ridu-version-comparison__date">{versionDate(selected.CreatedAt, i18n)}</span>
				</div>
				{#if controller.canRestore(false) || controller.canRestore(true)}
					<div class="ridu-version-comparison__restore">
						<Button
							size="sm"
							disabled={controller.restoring || !controller.canRestore(false)}
							onclick={() => requestRestore(false)}
						>
							{i18n.t("versions:restore")}
						</Button>
						{#if collection.versionSettings?.drafts && selected.Status !== "draft" && controller.canRestore(true)}
							<DropdownMenu>
								<DropdownMenuTrigger
									class={[buttonVariants({ size: "sm" }), "ridu-version-comparison__restore-menu"]}
									disabled={controller.restoring}
									aria-label={i18n.t("versions:restoreOptions")}
								>
									<Chevron />
								</DropdownMenuTrigger><DropdownMenuContent align="end">
									<DropdownMenuItem onSelect={() => requestRestore(true)}>
										{i18n.t("versions:restoreAsDraft")}
									</DropdownMenuItem>
								</DropdownMenuContent>
							</DropdownMenu>
						{/if}
					</div>
				{/if}
			</div>
		</div>
	</div>

	{#if comparisonView.error}
		<div class="ridu-versions__message">
			<Banner tone="destructive">{comparisonView.error}</Banner>
			<Button variant="outline" onclick={controller.retryComparison}>
				{i18n.t("general:retry")}
			</Button>
		</div>
	{/if}
	<VersionDiffFields {rows} {modifiedOnly} {references} />
</div>

<ConfirmationDialog
	bind:open={restoreOpen}
	title={i18n.t("versions:confirmRestore")}
	description={i18n.t("versions:restoreVersionDescription", {
		label: i18n.text(collection.labels.singular, collection.labels.singularTranslations),
		date: versionDate(selected.CreatedAt, i18n),
	})}
	confirmLabel={i18n.t(controller.restoring ? "versions:restoring" : "general:confirm")}
	disabled={controller.restoring}
	onconfirm={() => controller.restore(restoreAsDraft, onRestored)}
/>

<Dialog.Root bind:open={drawerOpen}>
	<Dialog.Portal>
		<Dialog.Overlay class="ridu-version-picker-overlay" />
		<Dialog.Content class="ridu-version-picker" dir={i18n.direction}>
			<header>
				<Dialog.Title>{i18n.t("versions:selectVersion")}</Dialog.Title><Dialog.Close
					class="ridu-version-picker__close"
					aria-label={i18n.t("general:close")}
				>
					<XIcon />
				</Dialog.Close>
			</header>
			<Dialog.Description class="sr-only">{i18n.t("versions:selectVersion")}</Dialog.Description>
			<VersionTable
				versions={comparisonOptions}
				document={controller.document}
				search={drawerSearch}
				onQueryChange={(changes) => {
					const next = new URLSearchParams(drawerSearch);
					for (const [key, value] of Object.entries(changes)) next.set(key, value);
					drawerSearch = next;
				}}
				onSelect={(version) => {
					onQueryChange({ compare: String(version.Revision) });
					drawerOpen = false;
				}}
			/>
		</Dialog.Content>
	</Dialog.Portal>
</Dialog.Root>
