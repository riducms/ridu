<script lang="ts">
	import type { PreviewToken, SchemaLivePreview } from "@riducms/protocol";
	import {
		RIDU_LIVE_PREVIEW_MESSAGE,
		type RiduLivePreviewReadyMessage,
		type RiduLivePreviewUpdateMessage,
	} from "@riducms/sdk";
	import ExternalLinkIcon from "~icons/lucide/external-link";
	import { TooltipContent, TooltipRoot, TooltipTrigger, Button, Input } from "@riducms/ui";

	import "@admin/features/documents/live-preview-panel.scss";

	import { Banner } from "@admin/components/ui/banner";

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
	}: {
		preview: SchemaLivePreview;
		collection: string;
		documentID: string;
		resource: "collection" | "global";
		values: FormValues;
	} = $props();

	const runtime = getAdminRuntime();
	let iframe = $state<HTMLIFrameElement>();
	let viewport = $state("responsive");
	let availableWidth = $state(768);
	let availableHeight = $state(720);
	let customWidth = $state(768);
	let customHeight = $state(720);
	let zoom = $state(100);
	let iframeReady = $state(false);
	let popupReady = false;
	let popupWindow: Window | null = null;
	let popupCapabilityToken: string | undefined;
	let sequence = 0;
	let capability = $state<PreviewToken>();
	let capabilityError = $state<string>();
	let capabilityRevision = $state(0);
	let activeCapabilityToken: string | undefined;
	let activeCapabilityOwner: string | undefined;
	const channel = crypto.randomUUID();
	const selectedBreakpoint = $derived(
		preview.breakpoints?.find((breakpoint) => `breakpoint:${breakpoint.name}` === viewport)
	);
	const frameWidth = $derived(
		Math.max(
			1,
			Math.round(
				selectedBreakpoint?.width ?? (viewport === "custom" ? customWidth : availableWidth)
			)
		)
	);
	const frameHeight = $derived(
		Math.max(
			1,
			Math.round(
				selectedBreakpoint?.height ?? (viewport === "custom" ? customHeight : availableHeight)
			)
		)
	);
	const viewportLabel = $derived(
		selectedBreakpoint
			? runtime.i18n.text(selectedBreakpoint.label, selectedBreakpoint.labelTranslations)
			: runtime.i18n.t(viewport === "custom" ? "documents:customViewport" : "documents:responsive")
	);

	function resizeViewport(axis: "width" | "height", event: Event) {
		const value = Number((event.currentTarget as HTMLInputElement).value);
		if (!Number.isFinite(value) || value < 1 || value > 3840) return;
		customWidth = axis === "width" ? value : frameWidth;
		customHeight = axis === "height" ? value : frameHeight;
		viewport = "custom";
	}
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
		const owner = `${resource}:${collection}:${documentID}`;
		if (activeCapabilityOwner !== owner) releaseCapability();
		void authorizePreview(owner, controller.signal);
		return () => controller.abort();
	});

	$effect(() => () => {
		releaseCapability();
		if (popupWindow !== null && !popupWindow.closed) popupWindow.close();
		popupWindow = null;
		popupReady = false;
		popupCapabilityToken = undefined;
	});

	$effect(() => {
		if (capability === undefined) return;
		const refreshIn = Math.max(1_000, Date.parse(capability.expiresAt) - Date.now() - 30_000);
		const timeout = window.setTimeout(() => capabilityRevision++, refreshIn);
		return () => window.clearTimeout(timeout);
	});

	async function authorizePreview(owner: string, signal: AbortSignal) {
		capabilityError = undefined;
		try {
			const nextCapability =
				resource === "global"
					? await runtime.client.createGlobalPreviewToken(collection, { signal })
					: await runtime.client.createPreviewToken(collection, documentID, { signal });
			if (signal.aborted) {
				revokeCapability(nextCapability.token);
				return;
			}
			const previousToken = activeCapabilityToken;
			activeCapabilityToken = nextCapability.token;
			activeCapabilityOwner = owner;
			capability = nextCapability;
			if (previousToken !== undefined && previousToken !== nextCapability.token) {
				revokeCapability(previousToken);
			}
		} catch (cause) {
			if (signal.aborted) return;
			releaseCapability();
			capabilityError =
				cause instanceof Error
					? cause.message
					: runtime.i18n.t("documents:previewAuthorizationFailed");
		}
	}

	function releaseCapability() {
		const token = activeCapabilityToken;
		activeCapabilityToken = undefined;
		activeCapabilityOwner = undefined;
		capability = undefined;
		if (token !== undefined) revokeCapability(token);
	}

	function revokeCapability(token: string) {
		void runtime.client.revokePreviewToken(token, { keepalive: true }).catch(() => undefined);
	}
</script>

<section class="ridu-live-preview" aria-label={runtime.i18n.t("documents:livePreview")}>
	<div class="ridu-live-preview__toolbar">
		<span class="ridu-live-preview__status" role="status">
			{capabilityError !== undefined
				? runtime.i18n.t("documents:previewUnavailable")
				: capability === undefined
					? runtime.i18n.t("documents:previewAuthorizing")
					: iframeReady
						? runtime.i18n.t("documents:previewConnected")
						: runtime.i18n.t("documents:previewConnecting")}
		</span>
		<Select type="single" bind:value={viewport}>
			<SelectTrigger
				class="ridu-live-preview__select"
				aria-label={runtime.i18n.t("documents:previewViewport")}
			>
				{viewportLabel}
			</SelectTrigger>
			<SelectContent class="ridu-live-preview-menu" align="end">
				{#each preview.breakpoints ?? [] as breakpoint (breakpoint.name)}
					<SelectItem
						value={`breakpoint:${breakpoint.name}`}
						label={runtime.i18n.text(breakpoint.label, breakpoint.labelTranslations)}
					/>
				{/each}
				<SelectItem value="responsive" label={runtime.i18n.t("documents:responsive")} />
				{#if viewport === "custom"}
					<SelectItem value="custom" label={runtime.i18n.t("documents:customViewport")} />
				{/if}
			</SelectContent>
		</Select>

		<div class="ridu-live-preview__dimensions">
			<Input
				class="ridu-live-preview__size"
				type="number"
				min="1"
				max="3840"
				value={frameWidth}
				aria-label={runtime.i18n.t("documents:previewWidth")}
				oninput={(event) => resizeViewport("width", event)}
			/>
			<span aria-hidden="true">×</span>
			<Input
				class="ridu-live-preview__size"
				type="number"
				min="1"
				max="3840"
				value={frameHeight}
				aria-label={runtime.i18n.t("documents:previewHeight")}
				oninput={(event) => resizeViewport("height", event)}
			/>
		</div>
		<Select type="single" value={String(zoom)} onValueChange={(next) => (zoom = Number(next))}>
			<SelectTrigger
				class="ridu-live-preview__select"
				aria-label={runtime.i18n.t("documents:previewZoom")}
			>
				{runtime.i18n.formatNumber(zoom / 100, { style: "percent" })}
			</SelectTrigger>
			<SelectContent class="ridu-live-preview-menu" align="end">
				{#each [50, 75, 100, 125, 150, 200] as percentage}
					<SelectItem
						value={String(percentage)}
						label={runtime.i18n.formatNumber(percentage / 100, { style: "percent" })}
					/>
				{/each}
			</SelectContent>
		</Select>
		{#if previewURL !== undefined}
			<TooltipRoot>
				<TooltipTrigger>
					{#snippet child({ props })}
						<a
							{...props}
							class="ridu-live-preview__external"
							href={previewURL}
							target="_blank"
							rel="opener"
							aria-label={runtime.i18n.t("documents:openPreviewWindow")}
							onclick={openPreviewWindow}
						>
							<ExternalLinkIcon />
						</a>
					{/snippet}
				</TooltipTrigger>
				<TooltipContent>{runtime.i18n.t("documents:openPreviewWindow")}</TooltipContent>
			</TooltipRoot>
		{/if}
	</div>

	<div
		class="ridu-live-preview__viewport"
		data-preview-viewport={viewport}
		bind:clientWidth={availableWidth}
		bind:clientHeight={availableHeight}
	>
		{#if capabilityError !== undefined}
			<Banner class="ridu-live-preview__error" tone="destructive">
				<span>{capabilityError}</span>
				<Button variant="outline" size="sm" onclick={() => capabilityRevision++}>
					{runtime.i18n.t("documents:retryPreview")}
				</Button>
			</Banner>
		{:else if previewURL === undefined}
			<div class="ridu-live-preview__loading">
				{runtime.i18n.t("documents:authorizingDraftPreview")}
			</div>
		{:else}
			<div
				class="ridu-live-preview__frame"
				style:width="{frameWidth * (zoom / 100)}px"
				style:height="{frameHeight * (zoom / 100)}px"
			>
				<iframe
					bind:this={iframe}
					src={previewURL}
					title={runtime.i18n.t("documents:livePreview")}
					width={frameWidth}
					height={frameHeight}
					style:transform="scale({zoom / 100})"
					onload={handleLoad}
				></iframe>
			</div>
		{/if}
	</div>
</section>
