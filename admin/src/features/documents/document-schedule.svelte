<script lang="ts">
	import { Dialog } from "bits-ui";
	import { Button } from "@riducms/ui";
	import XIcon from "~icons/lucide/x";
	import TimeZonePicker from "@admin/components/ui/timezone-picker/timezone-picker.svelte";
	import { DateValueControl } from "@admin/components/ui/date-value-control";
	import { provideDrawerDepth } from "@admin/components/ui/drawer/drawer-depth";
	import { RadioCardItem, RadioGroup } from "@admin/components/ui/radio-group";
	import { timeZoneLabel } from "@admin/core/i18n/time-zone-label";
	import { dateTimeHasAmbiguousWallTime } from "@admin/fields/scalar/date-control-value";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import type { AdminScheduledPublication } from "@admin/core/api/admin-client";
	import "@admin/features/documents/document-schedule.scss";

	let {
		slug,
		documentID,
		title,
		revision,
		editable,
		canPublish,
		canUnpublish,
		status,
		dirty,
		onClose,
	}: {
		slug: string;
		documentID: string;
		title: string;
		revision: number;
		editable: boolean;
		canPublish: boolean;
		canUnpublish: boolean;
		status: "draft" | "published";
		dirty: boolean;
		onClose: () => void;
	} = $props();

	const runtime = getAdminRuntime();
	const notifications = getNotificationCenter();
	const depth = provideDrawerDepth();
	let request: AbortController | undefined;
	let runAt = $state("");
	const openedAt = new Date();
	const scheduleDate = $derived(runAt ? new Date(runAt) : openedAt);
	// Each drawer owns its selection; changing it never updates account preferences.
	const browserTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
	const defaultTimeZone =
		runtime.manifest?.application.adminLocalization?.defaultTimeZone ??
		runtime.i18n.timeZone ??
		browserTimeZone;
	let timeZone = $state(defaultTimeZone);
	const effectiveTimeZone = $derived(timeZone || browserTimeZone);
	// The action defaults from the document state when this drawer opens.
	// svelte-ignore state_referenced_locally
	let action = $state<AdminScheduledPublication["action"]>(
		status === "published" && canUnpublish ? "unpublish" : "publish"
	);
	const canScheduleAction = $derived(
		action === "publish" ? canPublish : canUnpublish && status === "published"
	);
	let schedules = $state.raw<AdminScheduledPublication[]>([]);
	let loading = $state(true);
	let busy = $state(false);
	let error = $state<string>();

	$effect(() => {
		const controller = new AbortController();
		request = controller;
		loading = true;
		busy = false;
		error = undefined;
		runAt = "";
		timeZone = defaultTimeZone;
		schedules = [];
		load(slug, documentID, controller.signal);
		return () => {
			controller.abort();
			if (request === controller) request = undefined;
		};
	});

	async function load(targetSlug: string, targetDocumentID: string, signal: AbortSignal) {
		try {
			const jobs = await runtime.client.scheduledPublications(targetSlug, targetDocumentID, {
				signal,
			});
			if (!signal.aborted) schedules = jobs;
		} catch (cause) {
			if (!signal.aborted)
				error =
					cause instanceof Error ? cause.message : runtime.i18n.t("versions:publishScheduleFailed");
		} finally {
			if (!signal.aborted) loading = false;
		}
	}

	async function schedule() {
		const signal = request?.signal;
		if (
			!editable ||
			!canScheduleAction ||
			dirty ||
			loading ||
			busy ||
			signal === undefined ||
			signal.aborted
		)
			return;
		const date = new Date(runAt);
		if (!Number.isFinite(date.valueOf()) || date <= new Date()) {
			error = runtime.i18n.t(
				action === "publish"
					? "versions:chooseFuturePublishTime"
					: "versions:chooseFutureUnpublishTime"
			);
			return;
		}
		if (dateTimeHasAmbiguousWallTime(runAt, effectiveTimeZone)) {
			error = runtime.i18n.t("versions:chooseUnambiguousTime");
			return;
		}
		busy = true;
		error = undefined;
		try {
			const options = { revision, signal, timeZone: effectiveTimeZone };
			const job =
				action === "publish"
					? await runtime.client.schedulePublish(slug, documentID, date, options)
					: await runtime.client.scheduleUnpublish(slug, documentID, date, options);
			if (signal.aborted) return;
			schedules = [...schedules, job].sort(
				(left, right) =>
					new Date(left.runAt).valueOf() - new Date(right.runAt).valueOf() ||
					left.id.localeCompare(right.id)
			);
			runAt = "";
			notifications.success({
				title: runtime.i18n.t(
					action === "publish" ? "versions:publishScheduled" : "versions:unpublishScheduled"
				),
			});
		} catch (cause) {
			if (!signal.aborted)
				error =
					cause instanceof Error ? cause.message : runtime.i18n.t("versions:publishScheduleFailed");
		} finally {
			if (!signal.aborted) busy = false;
		}
	}

	async function cancel(job: AdminScheduledPublication) {
		const signal = request?.signal;
		if (!editable || !canCancel(job) || busy || signal === undefined || signal.aborted) return;
		busy = true;
		error = undefined;
		try {
			await runtime.client.cancelScheduledPublication(slug, documentID, job.id, {
				signal,
			});
			if (signal.aborted) return;
			schedules = schedules.filter((candidate) => candidate.id !== job.id);
			notifications.success({ title: runtime.i18n.t("versions:scheduledPublicationCancelled") });
		} catch (cause) {
			if (!signal.aborted)
				error =
					cause instanceof Error ? cause.message : runtime.i18n.t("versions:scheduleCancelFailed");
		} finally {
			if (!signal.aborted) busy = false;
		}
	}

	function eventTimeZoneLabel(id: string, runAt: string) {
		const option = runtime.i18n.timeZones.find((zone) => zone.id === id);
		return timeZoneLabel(
			id,
			option ? runtime.i18n.text(option.label, option.labelTranslations) : id,
			runtime.i18n.language,
			new Date(runAt)
		);
	}

	function canCancel(job: AdminScheduledPublication) {
		return job.action === "publish" ? canPublish : canUnpublish;
	}
</script>

<Dialog.Root
	open
	onOpenChange={(open) => {
		if (!open && !busy) onClose();
	}}
>
	<Dialog.Portal>
		<Dialog.Overlay class="ridu-schedule-overlay" />
		<Dialog.Content
			class="ridu-schedule"
			style={`--drawer-depth: ${depth}`}
			dir={runtime.i18n.direction}
		>
			<header class="ridu-schedule__header">
				<Dialog.Title class="ridu-schedule__title">
					{runtime.i18n.t("documents:scheduleFor", { title })}
				</Dialog.Title>
				<Dialog.Close
					class="ridu-schedule__close"
					disabled={busy}
					aria-label={runtime.i18n.t("general:close")}
				>
					<XIcon />
				</Dialog.Close>
			</header>
			<div class="ridu-schedule__form">
				<Dialog.Description class="ridu-schedule__description">
					{runtime.i18n.t("versions:scheduledPublicationDescription")}
				</Dialog.Description>
				<fieldset class="ridu-schedule__type">
					<legend id="document-schedule-type-label">
						{runtime.i18n.t("documents:scheduleType")}
						<span aria-hidden="true">*</span>
					</legend>
					<RadioGroup
						class="ridu-schedule__choices"
						orientation="horizontal"
						name="document-schedule-action"
						value={action}
						disabled={busy}
						aria-labelledby="document-schedule-type-label"
						onValueChange={(next) => {
							if (next === "publish" || next === "unpublish") action = next;
						}}
					>
						<RadioCardItem
							value="publish"
							label={runtime.i18n.t("documents:publish")}
							disabled={!canPublish}
						/>
						<RadioCardItem
							value="unpublish"
							label={runtime.i18n.t("documents:unpublish")}
							disabled={!canUnpublish || status !== "published"}
						/>
					</RadioGroup>
				</fieldset>
				<label for="document-schedule-date">
					{runtime.i18n.t("fields:time")}
					<span aria-hidden="true">*</span>
				</label>
				<DateValueControl
					id="document-schedule-date"
					appearance="date-time"
					value={runAt}
					timeZone={effectiveTimeZone}
					disabled={!editable || busy || dirty}
					onValueChange={(value) => (runAt = value)}
				/>
				{#if runtime.i18n.timeZones.length > 0}
					<label for="document-schedule-timezone">{runtime.i18n.t("fields:timeZone")}</label>
					<TimeZonePicker
						id="document-schedule-timezone"
						at={scheduleDate}
						value={timeZone}
						options={runtime.i18n.timeZones}
						disabled={!editable || busy || dirty}
						onValueChange={(next) => (timeZone = next)}
					/>
				{/if}
				{#if dirty}
					<p role="status">{runtime.i18n.t("documents:saveBeforeSchedule")}</p>
				{/if}
				{#if error}
					<p class="ridu-field-error" role="alert">{error}</p>
				{/if}
				<Button
					disabled={!editable || !canScheduleAction || loading || busy || dirty}
					onclick={schedule}
				>
					{runtime.i18n.t(busy ? "versions:scheduling" : "documents:save")}
				</Button>
			</div>
			<section class="ridu-schedule__events" aria-busy={loading}>
				<h3>{runtime.i18n.t("documents:upcomingEvents")}</h3>
				{#each schedules as job (job.id)}
					<div class="ridu-schedule__event">
						<span>
							{runtime.i18n.t(
								job.action === "publish" ? "documents:publish" : "documents:unpublish"
							)} ·
							{runtime.i18n.formatDate(new Date(job.runAt), {
								dateStyle: "long",
								timeStyle: "short",
								timeZone: job.timeZone || runtime.i18n.timeZone || browserTimeZone,
							})}
							{#if job.timeZone}
								<span class="ridu-schedule__zone">
									{eventTimeZoneLabel(job.timeZone, job.runAt)}
								</span>
							{/if}
						</span>
						<Button
							variant="ghost"
							size="icon-sm"
							disabled={!editable || !canCancel(job) || busy}
							aria-label={runtime.i18n.t(
								job.action === "publish"
									? "versions:cancelScheduledPublish"
									: "versions:cancelScheduledUnpublish"
							)}
							onclick={() => cancel(job)}
						>
							<XIcon />
						</Button>
					</div>
				{:else}
					{#if !loading}
						<p>{runtime.i18n.t("documents:noScheduledEvents")}</p>
					{/if}
				{/each}
			</section>
		</Dialog.Content>
	</Dialog.Portal>
</Dialog.Root>
