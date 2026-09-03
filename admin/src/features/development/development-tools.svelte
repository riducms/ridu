<script lang="ts">
	import BookOpenIcon from "~icons/lucide/book-open";
	import ExternalLinkIcon from "~icons/lucide/external-link";
	import LoaderCircleIcon from "~icons/lucide/loader-circle";
	import { onDestroy } from "svelte";

	import RiduLogo from "@admin/components/brand/ridu-logo.svelte";
	import { Button } from "@admin/components/ui/button";
	import { Popover, PopoverContent, PopoverTrigger } from "@admin/components/ui/popover";
	import { StatusIndicator } from "@admin/components/ui/status-indicator";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { cn } from "@riducms/ui";

	type SchemaUpdateNotice = {
		tone: "success" | "error";
		label: string;
	};

	const runtime = getAdminRuntime();
	let updateApplied = $state(false);
	let notice = $state<SchemaUpdateNotice>();
	let popoverOpen = $state(false);
	let recoveryAvailable = $state(runtime.manifestRefreshError !== undefined);
	let refreshObserved = false;
	let noticeTimer: number | undefined;
	const status = $derived(
		runtime.refreshingManifest
			? {
					label: runtime.i18n.t("navigation:applyingSchemaUpdate"),
					tone: "live" as const,
				}
			: runtime.manifestRefreshError !== undefined
				? {
						label: runtime.i18n.t("development:schemaUpdateFailed"),
						tone: "destructive" as const,
					}
				: updateApplied
					? {
							label: runtime.i18n.t("development:upToDate"),
							tone: "success" as const,
						}
					: {
							label: runtime.i18n.t("development:ready"),
							tone: "live" as const,
						}
	);

	function hideNotice() {
		if (noticeTimer !== undefined) window.clearTimeout(noticeTimer);
		notice = undefined;
		noticeTimer = undefined;
	}

	function showNotice(next: SchemaUpdateNotice, duration: number) {
		hideNotice();
		notice = next;
		noticeTimer = window.setTimeout(() => {
			notice = undefined;
			noticeTimer = undefined;
		}, duration);
	}

	onDestroy(() => {
		if (noticeTimer !== undefined) window.clearTimeout(noticeTimer);
	});

	function retrySchema() {
		if (runtime.loading || runtime.refreshingManifest) return;
		void runtime.refreshManifest();
	}

	$effect(() => {
		const refreshing = runtime.refreshingManifest;
		const error = runtime.manifestRefreshError;
		if (refreshing) {
			refreshObserved = true;
			hideNotice();
			updateApplied = false;
			return;
		}
		if (!refreshObserved) return;
		refreshObserved = false;

		if (error !== undefined) {
			recoveryAvailable = true;
			showNotice({ tone: "error", label: runtime.i18n.t("development:schemaUpdateFailed") }, 7_000);
		} else {
			showNotice(
				{ tone: "success", label: runtime.i18n.t("development:schemaUpdateApplied") },
				4_500
			);
			updateApplied = true;
			if (recoveryAvailable) popoverOpen = false;
			recoveryAvailable = false;
		}
	});
</script>

<div class="fixed end-3 bottom-3 z-30" data-ridu-development-tools>
	{#if notice !== undefined}
		<div
			class={cn(
				"ridu-popover-enter pointer-events-none absolute end-full bottom-0 me-2 flex h-9 max-w-[calc(100vw-4.5rem)] items-center gap-2 whitespace-nowrap rounded-[8px] border bg-popover px-3 text-[12.5px] font-medium shadow-[var(--shadow-popover)]",
				notice.tone === "error"
					? "border-destructive/30 text-destructive"
					: "border-success/30 text-success"
			)}
			role={notice.tone === "error" ? "alert" : "status"}
			data-ridu-development-toast
			data-tone={notice.tone}
		>
			<span class="size-1.75 shrink-0 rounded-full bg-current" aria-hidden="true"></span>
			<span class="truncate">{notice.label}</span>
		</div>
	{/if}

	<Popover bind:open={popoverOpen}>
		<PopoverTrigger
			class={cn(
				"relative inline-grid size-9 cursor-pointer place-items-center rounded-full border bg-popover text-popover-foreground shadow-[var(--shadow-popover)] transition-[background-color,border-color,color] duration-150 outline-none hover:bg-control-hover focus-visible:outline-2 focus-visible:outline-ring/70 focus-visible:outline-offset-2",
				runtime.manifestRefreshError !== undefined
					? "border-destructive/55"
					: updateApplied
						? "border-success/55"
						: "border-control-border"
			)}
			aria-label={runtime.i18n.t("development:toolsStatus", { status: status.label })}
			title={runtime.i18n.t("development:tools")}
			data-ridu-development-trigger
		>
			{#if runtime.refreshingManifest}
				<LoaderCircleIcon
					class="size-4 animate-spin text-primary motion-reduce:animate-none"
					aria-hidden="true"
				/>
			{:else}
				<RiduLogo variant="arch" class="h-4 text-foreground" />
			{/if}
			<span
				class={cn(
					"absolute top-0 end-0 size-2.5 rounded-full border-2 border-popover",
					runtime.manifestRefreshError !== undefined
						? "bg-destructive"
						: updateApplied
							? "bg-success"
							: "bg-primary"
				)}
				aria-hidden="true"
			></span>
		</PopoverTrigger>

		<PopoverContent
			class="w-[min(18rem,calc(100vw-1.5rem))] gap-0 overflow-hidden p-0"
			side="top"
			align="end"
			sideOffset={8}
			role="dialog"
			aria-label={runtime.i18n.t("development:tools")}
			data-ridu-development-panel
		>
			<div class="flex items-center gap-2.5 border-b border-control-border px-3.5 py-3">
				<RiduLogo variant="arch" class="h-4 text-foreground" />
				<p class="text-[13px] font-semibold text-foreground">
					{runtime.i18n.t("development:tools")}
				</p>
			</div>

			<div class="grid gap-2.5 px-3.5 py-3">
				<div class="flex items-center justify-between gap-4">
					<span class="font-mono text-[10px] tracking-[0.12em] text-foreground-faint uppercase">
						{runtime.i18n.t("development:schema")}
					</span>
					<StatusIndicator tone={status.tone} pulse={runtime.refreshingManifest}>
						{status.label}
					</StatusIndicator>
				</div>
				{#if runtime.manifestRefreshError !== undefined}
					<p class="text-[12px] leading-5 text-destructive">
						{runtime.manifestRefreshError}
					</p>
				{/if}
				{#if runtime.manifestRefreshError !== undefined || recoveryAvailable}
					<Button
						class="w-fit"
						variant="outline"
						size="sm"
						aria-disabled={runtime.refreshingManifest}
						aria-busy={runtime.refreshingManifest}
						onclick={retrySchema}
					>
						{runtime.refreshingManifest
							? runtime.i18n.t("navigation:applyingSchemaUpdate")
							: runtime.i18n.t("development:retrySchemaUpdate")}
					</Button>
				{/if}
			</div>

			<a
				class="flex items-center gap-2.5 border-t border-control-border px-3.5 py-3 text-foreground-muted outline-none transition-colors hover:bg-control-hover hover:text-foreground focus-visible:bg-control-hover focus-visible:text-foreground"
				href="https://riducms.com"
				target="_blank"
				rel="noreferrer"
			>
				<BookOpenIcon class="size-3.5 shrink-0" aria-hidden="true" />
				<span class="min-w-0 flex-1">
					<span class="block text-[12.5px] font-medium">
						{runtime.i18n.t("development:documentation")}
					</span>
					<span class="block text-[11px] text-foreground-faint">
						{runtime.i18n.t("development:documentationDescription")}
					</span>
				</span>
				<ExternalLinkIcon class="size-3.5 shrink-0" aria-hidden="true" />
			</a>
		</PopoverContent>
	</Popover>
</div>
