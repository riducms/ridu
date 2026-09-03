<script lang="ts">
	import type { Snippet } from "svelte";

	import RiduLogo from "@admin/components/brand/ridu-logo.svelte";
	import { Banner } from "@admin/components/ui/banner";
	import { Button } from "@admin/components/ui/button";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	let { children }: { children: Snippet } = $props();
	const runtime = getAdminRuntime();
	const development = import.meta.hot !== undefined;
</script>

{#if runtime.loading}
	<main
		class="ridu-surface-grid grid min-h-screen place-items-center"
		aria-label={runtime.i18n.t("general:loadingSchema")}
	>
		<div class="grid justify-items-center gap-4 text-[13px] text-foreground-muted">
			<RiduLogo class="h-7 text-foreground" />
			<span
				class="size-4 animate-spin rounded-full border border-control-border-hover border-r-primary"
				aria-hidden="true"
			></span>
			{runtime.i18n.t("general:loadingSchema")}
		</div>
	</main>
{:else if runtime.error !== undefined}
	<main class="ridu-surface-grid grid min-h-screen place-items-center p-6">
		<section
			class="grid max-w-[480px] gap-5 rounded-[4px] border border-control-border bg-background-surface p-7"
			role="alert"
		>
			<p class="font-mono text-[10px] tracking-[0.14em] text-destructive uppercase">
				{runtime.i18n.t("errors:adminBootstrapFailed")}
			</p>
			<h1 class="font-serif text-[31px] leading-tight tracking-[-0.015em]">
				{runtime.i18n.t("errors:couldNotLoadApplication")}
			</h1>
			<p class="text-[14px] leading-6 text-foreground-muted">{runtime.error}</p>
			<Button class="w-fit" onclick={() => runtime.bootstrap()}>
				{runtime.i18n.t("general:retry")}
			</Button>
		</section>
	</main>
{:else}
	{@render children()}
	{#if !development && runtime.refreshingManifest}
		<div class="pointer-events-none fixed top-4 left-1/2 z-50 w-[min(92vw,420px)] -translate-x-1/2">
			<Banner class="shadow-[var(--shadow-popover)]" role="status">
				<span
					class="size-3.5 shrink-0 animate-spin rounded-full border border-control-border-hover border-r-primary"
					aria-hidden="true"
				></span>
				{runtime.i18n.t("navigation:applyingSchemaUpdate")}
			</Banner>
		</div>
	{:else if !development && runtime.manifestRefreshError !== undefined}
		<div class="fixed top-4 left-1/2 z-50 w-[min(92vw,520px)] -translate-x-1/2">
			<Banner class="shadow-[var(--shadow-popover)]" tone="destructive" role="alert">
				<span class="size-2 shrink-0 rounded-full bg-destructive" aria-hidden="true"></span>
				<span class="min-w-0">
					{runtime.i18n.t("navigation:schemaUpdateFailed", {
						message: runtime.manifestRefreshError,
					})}
				</span>
			</Banner>
		</div>
	{/if}
{/if}
