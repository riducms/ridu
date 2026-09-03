<script lang="ts">
	import { useLocation } from "@hvniel/svelte-router";
	import { onMount, tick } from "svelte";

	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	const revealDelayMilliseconds = 160;
	const minimumVisibleMilliseconds = 180;
	const completionMilliseconds = 150;
	const fallbackIntentMilliseconds = 140;
	const runtime = getAdminRuntime();
	const routeLocation = $derived(useLocation());
	const routeKey = $derived(
		`${routeLocation.pathname}${routeLocation.search}${routeLocation.hash}`
	);

	type ProgressPhase = "idle" | "pending" | "running" | "finishing";

	let phase = $state<ProgressPhase>("idle");
	let progress = $state(0);
	let observedRouteKey = "";
	let startedAt = 0;
	let generation = 0;
	let revealTimer: number | undefined;
	let trickleTimer: number | undefined;
	let finishTimer: number | undefined;
	let hideTimer: number | undefined;
	let resetTimer: number | undefined;
	let intentTimer: number | undefined;

	function clearTimer(timer: number | undefined) {
		if (timer !== undefined) window.clearTimeout(timer);
	}

	function clearCompletionTimers() {
		clearTimer(finishTimer);
		clearTimer(hideTimer);
		clearTimer(resetTimer);
		finishTimer = undefined;
		hideTimer = undefined;
		resetTimer = undefined;
	}

	function scheduleTrickle() {
		clearTimer(trickleTimer);
		trickleTimer = window.setTimeout(() => {
			if (phase !== "running") return;
			progress += (0.92 - progress) * 0.16;
			scheduleTrickle();
		}, 150);
	}

	function start() {
		clearCompletionTimers();
		clearTimer(intentTimer);
		intentTimer = undefined;

		if (phase === "pending") return;
		if (phase === "running") {
			generation += 1;
			return;
		}

		generation += 1;
		const activeGeneration = generation;
		phase = "pending";
		progress = 0;
		clearTimer(revealTimer);
		revealTimer = window.setTimeout(() => {
			if (activeGeneration !== generation || phase !== "pending") return;
			revealTimer = undefined;
			startedAt = performance.now();
			progress = 0.08;
			phase = "running";
			scheduleTrickle();
		}, revealDelayMilliseconds);
	}

	function afterDestinationPaint() {
		return new Promise<void>((resolve) => {
			requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
		});
	}

	async function finish() {
		const activeGeneration = generation;
		await tick();
		await afterDestinationPaint();
		if (activeGeneration !== generation || phase === "idle" || phase === "finishing") return;
		if (phase === "pending") {
			clearTimer(revealTimer);
			revealTimer = undefined;
			phase = "idle";
			progress = 0;
			return;
		}

		const remaining = Math.max(0, minimumVisibleMilliseconds - (performance.now() - startedAt));
		finishTimer = window.setTimeout(() => {
			if (activeGeneration !== generation || phase !== "running") return;
			clearTimer(trickleTimer);
			trickleTimer = undefined;
			progress = 1;
			phase = "finishing";
			hideTimer = window.setTimeout(() => {
				if (activeGeneration !== generation) return;
				phase = "idle";
				resetTimer = window.setTimeout(() => {
					if (activeGeneration === generation) progress = 0;
				}, completionMilliseconds);
			}, completionMilliseconds);
		}, remaining);
	}

	function startIntent() {
		const routeBeforeIntent = routeKey;
		start();
		intentTimer = window.setTimeout(() => {
			intentTimer = undefined;
			if (routeKey === routeBeforeIntent) void finish();
		}, fallbackIntentMilliseconds);
	}

	function handleLinkIntent(event: MouseEvent) {
		if (
			event.defaultPrevented ||
			event.button !== 0 ||
			event.metaKey ||
			event.ctrlKey ||
			event.shiftKey ||
			event.altKey ||
			!(event.target instanceof Element)
		) {
			return;
		}

		const anchor = event.target.closest<HTMLAnchorElement>("a[href]");
		if (
			anchor === null ||
			anchor.hasAttribute("download") ||
			(anchor.target !== "" && anchor.target !== "_self")
		) {
			return;
		}

		const destination = new URL(anchor.href, window.location.href);
		if (
			destination.origin !== window.location.origin ||
			`${destination.pathname}${destination.search}${destination.hash}` ===
				`${window.location.pathname}${window.location.search}${window.location.hash}`
		) {
			return;
		}

		startIntent();
	}

	$effect(() => {
		const nextRouteKey = routeKey;
		if (observedRouteKey === "") {
			observedRouteKey = nextRouteKey;
			return;
		}
		if (nextRouteKey === observedRouteKey) return;

		observedRouteKey = nextRouteKey;
		start();
		void finish();
	});

	onMount(() => {
		const handleHistoryIntent = () => startIntent();
		document.addEventListener("click", handleLinkIntent, true);
		window.addEventListener("popstate", handleHistoryIntent);

		return () => {
			generation += 1;
			document.removeEventListener("click", handleLinkIntent, true);
			window.removeEventListener("popstate", handleHistoryIntent);
			clearTimer(revealTimer);
			clearTimer(trickleTimer);
			clearTimer(intentTimer);
			clearCompletionTimers();
		};
	});
</script>

<div
	class="pointer-events-none fixed inset-x-0 top-0 z-[100] h-0.5 overflow-hidden"
	aria-hidden={phase === "idle" || phase === "pending"}
>
	<div
		class={[
			"h-full origin-left bg-foreground-strong shadow-[0_0_10px_color-mix(in_srgb,currentColor_55%,transparent)] transition-[transform,opacity] duration-180 ease-out motion-reduce:transition-none rtl:origin-right",
			phase === "idle" || phase === "pending" ? "opacity-0" : "opacity-100",
		]}
		style:transform={`scaleX(${progress})`}
		role="progressbar"
		aria-label={runtime.i18n.t("navigation:loadingPage")}
		aria-valuemin="0"
		aria-valuemax="100"
		aria-valuenow={Math.round(progress * 100)}
	></div>
</div>
