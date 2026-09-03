<script lang="ts">
	import type { PreviewToken, SchemaLivePreview } from "@riducms/protocol";
	import {
		RIDU_LIVE_PREVIEW_MESSAGE,
		type RiduLivePreviewReadyMessage,
		type RiduLivePreviewUpdateMessage,
	} from "@riducms/sdk";
	import ExternalLinkIcon from "~icons/lucide/external-link";
	import XIcon from "~icons/lucide/x";
	import { TooltipContent, TooltipRoot, TooltipTrigger } from "@riducms/ui";

	import { Button, buttonVariants } from "@admin/components/ui/button";
	import { Banner } from "@admin/components/ui/banner";
	import { Input } from "@admin/components/ui/input";
	import { Select, SelectContent, SelectItem, SelectTrigger } from "@admin/components/ui/select";
	import type { FormValues } from "@admin/core/forms/form-schema";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import {
		addLivePreviewChannel,
		addLivePreviewToken,
		livePreviewTargetOrigin,
		resolveLivePreviewURL,
	} from "@admin/features/documents/live-preview";

	let {
		preview,
		collection,
		documentID,
		resource,
		values,
		onclose,
	}: {
		preview: SchemaLivePreview;
		collection: string;
		documentID: string;
		resource: "collection" | "global";
		values: FormValues;
		onclose: () => void;
	} = $props();

	const runtime = getAdminRuntime();
	let iframe = $state<HTMLIFrameElement>();
	let viewport = $state("responsive");
	let responsiveWidth = $state(768);
	let responsiveHeight = $state(720);
	let zoom = $state(100);
	let iframeReady = $state(false);
	let popupReady = false;
	let popupWindow: Window | null = null;
	let popupCapabilityToken: string | undefined;
	let sequence = 0;
	let capability = $state<PreviewToken>();
	let capabilityError = $state<string>();
	let capabilityRevision = $state(0);
	const channel = crypto.randomUUID();
	const selectedBreakpoint = $derived(
		preview.breakpoints?.find((breakpoint) => breakpoint.name === viewport)
	);
	const frameWidth = $derived(selectedBreakpoint?.width ?? responsiveWidth);
	const frameHeight = $derived(selectedBreakpoint?.height ?? responsiveHeight);
	const previewURL = $derived(
		capability === undefined
			? undefined
			: addLivePreviewToken(
					addLivePreviewChannel(
						resolveLivePreviewURL(
							preview,
							{ collection, documentID, values },
							window.location.href
						),
						channel
					),
					capability.token
				)
	);
	const serializedValues = $derived(JSON.stringify(values));

	function sendPreview(target: Window) {
		if (previewURL === undefined) return;
		const message: RiduLivePreviewUpdateMessage<FormValues> = {
			type: RIDU_LIVE_PREVIEW_MESSAGE,
			channel,
			sequence: sequence++,
			resource,
			slug: collection,
			collection,
			id: documentID,
			data: JSON.parse(serializedValues) as FormValues,
		};
		target.postMessage(message, livePreviewTargetOrigin(previewURL));
	}

	function handleLoad() {
		iframeReady = false;
		const target = iframe?.contentWindow;
		if (target !== null && target !== undefined) sendPreview(target);
	}

	function openPreviewWindow(event: MouseEvent) {
		event.preventDefault();
		if (previewURL === undefined) return;
		popupReady = false;
		popupWindow = window.open(
			previewURL,
			`ridu-live-preview-${channel}`,
			"popup=yes,width=1200,height=800,resizable=yes,scrollbars=yes"
		);
		popupCapabilityToken = capability?.token;
	}

	$effect(() => {
		if (previewURL === undefined) return;
		const origin = livePreviewTargetOrigin(previewURL);
		const handleMessage = (event: MessageEvent) => {
			if (event.origin !== origin || !isReadyMessage(event.data, channel)) return;
			const iframeTarget = iframe?.contentWindow;
			if (event.source === iframeTarget && iframeTarget !== null && iframeTarget !== undefined) {
				iframeReady = true;
				return;
			}
			if (event.source === popupWindow && popupWindow !== null && !popupWindow.closed) {
				popupReady = true;
				sendPreview(popupWindow);
			}
		};
		window.addEventListener("message", handleMessage);
		return () => window.removeEventListener("message", handleMessage);
	});

	function isReadyMessage(
		value: unknown,
		expectedChannel: string
	): value is RiduLivePreviewReadyMessage {
		if (value === null || typeof value !== "object") return false;
		const candidate = value as Partial<RiduLivePreviewReadyMessage>;
		return (
			candidate.type === RIDU_LIVE_PREVIEW_MESSAGE &&
			candidate.ready === true &&
			candidate.channel === expectedChannel
		);
	}

	$effect(() => {
		previewURL;
		serializedValues;
		const iframeTarget = iframe?.contentWindow;
		if (iframeReady && iframeTarget !== null && iframeTarget !== undefined) {
			sendPreview(iframeTarget);
		}
		if (popupReady && popupWindow !== null && !popupWindow.closed) sendPreview(popupWindow);
	});

	$effect(() => {
		const nextURL = previewURL;
		const nextToken = capability?.token;
		if (
			nextURL === undefined ||
			nextToken === undefined ||
			popupWindow === null ||
			popupWindow.closed ||
			popupCapabilityToken === nextToken
		)
			return;
		popupReady = false;
		popupCapabilityToken = nextToken;
		popupWindow.location.replace(nextURL);
	});

	$effect(() => {
		capabilityRevision;
		const controller = new AbortController();
		void authorizePreview(controller.signal);
		return () => controller.abort();
	});

	$effect(() => {
		const token = capability?.token;
		if (token === undefined) return;
		return () => {
			void runtime.client.revokePreviewToken(token, { keepalive: true }).catch(() => undefined);
		};
	});

	$effect(() => {
		return () => {
			if (popupWindow !== null && !popupWindow.closed) popupWindow.close();
			popupWindow = null;
			popupReady = false;
			popupCapabilityToken = undefined;
		};
	});

	$effect(() => {
		if (capability === undefined) return;
		const refreshIn = Math.max(1_000, Date.parse(capability.expiresAt) - Date.now() - 30_000);
		const timeout = window.setTimeout(() => capabilityRevision++, refreshIn);
		return () => window.clearTimeout(timeout);
	});

	async function authorizePreview(signal: AbortSignal) {
		capabilityError = undefined;
		try {
			capability =
				resource === "global"
					? await runtime.client.createGlobalPreviewToken(collection, { signal })
					: await runtime.client.createPreviewToken(collection, documentID, { signal });
		} catch (cause) {
			if (signal.aborted) return;
			capability = undefined;
			capabilityError =
				cause instanceof Error
					? cause.message
					: runtime.i18n.t("documents:previewAuthorizationFailed");
		}
	}
</script>

<section
	class="flex min-h-[540px] min-w-0 flex-1 flex-col border-t border-control-border bg-background-layer min-[1240px]:min-h-0 min-[1240px]:border-t-0 min-[1240px]:border-s"
	aria-label={runtime.i18n.t("documents:livePreview")}
>
	<div
		class="flex min-h-12 flex-wrap items-center gap-1.5 border-b border-control-border px-3 py-2"
	>
		<span
			class="font-mono me-1 text-[9px] tracking-[0.08em] text-foreground-faint uppercase"
			role="status"
		>
			{capabilityError !== undefined
				? runtime.i18n.t("documents:previewUnavailable")
				: capability === undefined
					? runtime.i18n.t("documents:previewAuthorizing")
					: iframeReady
						? runtime.i18n.t("documents:previewConnected")
						: runtime.i18n.t("documents:previewConnecting")}
		</span>
		<div class="flex items-center gap-1" aria-label={runtime.i18n.t("documents:previewViewport")}>
			<Button
				variant="ghost"
				size="sm"
				class={[
					"h-7 px-2.5 text-[11px]",
					viewport === "responsive" && "bg-control-hover text-foreground",
				]}
				onclick={() => (viewport = "responsive")}
			>
				{runtime.i18n.t("documents:responsive")}
			</Button>
			{#each preview.breakpoints ?? [] as breakpoint (breakpoint.name)}
				<Button
					variant="ghost"
					size="sm"
					class={[
						"h-7 px-2.5 text-[11px]",
						viewport === breakpoint.name && "bg-control-hover text-foreground",
					]}
					onclick={() => (viewport = breakpoint.name)}
				>
					{runtime.i18n.text(breakpoint.label, breakpoint.labelTranslations)}
				</Button>
			{/each}
		</div>
		<div class="ms-auto flex items-center gap-1.5">
			<div class="font-mono flex items-center gap-1 text-[9.5px] text-foreground-faint">
				<label>
					<span class="sr-only">{runtime.i18n.t("documents:previewWidth")}</span>
					{#if selectedBreakpoint !== undefined}
						<Input
							class="h-7 w-15 px-1.5 text-center text-[10px]"
							type="number"
							value={frameWidth}
							disabled
						/>
					{:else}
						<Input
							class="h-7 w-15 px-1.5 text-center text-[10px]"
							type="number"
							min="240"
							max="3840"
							bind:value={responsiveWidth}
						/>
					{/if}
				</label>
				×
				<label>
					<span class="sr-only">{runtime.i18n.t("documents:previewHeight")}</span>
					{#if selectedBreakpoint !== undefined}
						<Input
							class="h-7 w-15 px-1.5 text-center text-[10px]"
							type="number"
							value={frameHeight}
							disabled
						/>
					{:else}
						<Input
							class="h-7 w-15 px-1.5 text-center text-[10px]"
							type="number"
							min="240"
							max="3840"
							bind:value={responsiveHeight}
						/>
					{/if}
				</label>
			</div>
			<label>
				<span class="sr-only">{runtime.i18n.t("documents:previewZoom")}</span>
				<Select type="single" value={String(zoom)} onValueChange={(next) => (zoom = Number(next))}>
					<SelectTrigger
						size="toolbar"
						class="w-18 text-[10px]"
						aria-label={runtime.i18n.t("documents:previewZoom")}
					>
						{runtime.i18n.formatNumber(zoom / 100, { style: "percent" })}
					</SelectTrigger>
					<SelectContent>
						{#each [50, 75, 100] as percentage}
							<SelectItem
								value={String(percentage)}
								label={runtime.i18n.formatNumber(percentage / 100, { style: "percent" })}
							/>
						{/each}
					</SelectContent>
				</Select>
			</label>
			{#if previewURL !== undefined}
				<TooltipRoot>
					<TooltipTrigger>
						{#snippet child({ props })}
							<a
								{...props}
								class={buttonVariants({ variant: "ghost", size: "icon-sm" })}
								href={previewURL}
								target="_blank"
								rel="opener"
								aria-label={runtime.i18n.t("documents:openPreviewWindow")}
								onclick={openPreviewWindow}
							>
								<ExternalLinkIcon class="size-3.5" />
							</a>
						{/snippet}
					</TooltipTrigger>
					<TooltipContent>{runtime.i18n.t("documents:openPreviewWindow")}</TooltipContent>
				</TooltipRoot>
			{/if}
			<Button
				variant="ghost"
				size="icon-sm"
				onclick={onclose}
				aria-label={runtime.i18n.t("documents:closeLivePreview")}
				tooltip={runtime.i18n.t("documents:closeLivePreview")}
			>
				<XIcon class="size-3.5" />
			</Button>
		</div>
	</div>
	<div class="min-h-0 flex-1 overflow-auto p-4" data-preview-viewport={viewport}>
		{#if capabilityError !== undefined}
			<Banner class="mx-auto mt-8 max-w-lg" tone="destructive">
				<div class="flex flex-1 items-center justify-between gap-4">
					<span>{capabilityError}</span>
					<Button variant="outline" size="sm" onclick={() => capabilityRevision++}>
						{runtime.i18n.t("documents:retryPreview")}
					</Button>
				</div>
			</Banner>
		{:else if previewURL === undefined}
			<div class="grid min-h-72 place-items-center text-[12px] text-foreground-faint">
				{runtime.i18n.t("documents:authorizingDraftPreview")}
			</div>
		{:else}
			<div
				class="mx-auto overflow-hidden rounded-[4px] border border-control-border bg-preview-canvas shadow-lg"
				style:width="{frameWidth * (zoom / 100)}px"
				style:height="{frameHeight * (zoom / 100)}px"
			>
				<iframe
					bind:this={iframe}
					class="block border-0 bg-preview-canvas"
					src={previewURL}
					title={runtime.i18n.t("documents:livePreview")}
					width={frameWidth}
					height={frameHeight}
					style:transform="scale({zoom / 100})"
					style:transform-origin="top left"
					onload={handleLoad}
				></iframe>
			</div>
		{/if}
	</div>
</section>
