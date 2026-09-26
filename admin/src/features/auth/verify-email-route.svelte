<script lang="ts">
	import { Link, useLocation } from "@hvniel/svelte-router";
	import { RiduError } from "@riducms/sdk";

	import { Banner } from "@admin/components/ui/banner";
	import { Button } from "@riducms/ui";
	import { adminRoutePatterns } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import AuthFrame from "@admin/features/auth/auth-frame.svelte";

	const runtime = getAdminRuntime();
	const location = useLocation();

	const token = $derived(new URLSearchParams(location.current.search).get("token") ?? "");
	const authCollection = $derived(runtime.authCollection);
	const owner = $derived(JSON.stringify([authCollection?.id, authCollection?.slug, token]));

	let pending = $state(false);
	let complete = $state(false);
	let error = $state<string>();
	let request: AbortController | undefined;

	$effect.pre(() => {
		// A retained route must reset its outcome before rendering a different token owner.
		owner;
		pending = false;
		complete = false;
		error = undefined;
		return () => {
			request?.abort();
			request = undefined;
		};
	});

	async function verify() {
		if (authCollection === undefined || token === "" || pending || complete) return;
		const requestOwner = owner;
		const activeRequest = new AbortController();
		request = activeRequest;
		const current = () => request === activeRequest && requestOwner === owner;

		pending = true;
		error = undefined;

		try {
			await runtime.client.verifyEmail(authCollection.slug, token, {
				signal: activeRequest.signal,
			});
			if (!current()) return;
			complete = true;
		} catch (cause) {
			if (!current()) return;
			error =
				cause instanceof RiduError ? cause.message : runtime.i18n.t("auth:emailVerificationFailed");
		} finally {
			if (current()) {
				request = undefined;
				pending = false;
			}
		}
	}
</script>

<AuthFrame>
	<div class="ridu-auth__stack">
		<h1 class="ridu-auth__heading">
			{runtime.i18n.t("auth:verifyYourEmail")}
		</h1>
		{#if complete}
			<Banner>{runtime.i18n.t("auth:emailVerified")}</Banner>
			<Link class="ridu-auth__link" to={adminRoutePatterns.login}>
				{runtime.i18n.t("auth:backToLogin")}
			</Link>
		{:else if token === ""}
			<Banner tone="destructive">{runtime.i18n.t("auth:missingVerificationToken")}</Banner>
		{:else}
			<p class="ridu-auth__description">
				{runtime.i18n.t("auth:verifyDescription")}
			</p>
			{#if error !== undefined}
				<Banner tone="destructive">{error}</Banner>
			{/if}
			<Button
				class="ridu-auth__submit"
				size="lg"
				onclick={verify}
				disabled={pending}
				aria-busy={pending}
			>
				{pending ? runtime.i18n.t("auth:verifying") : runtime.i18n.t("auth:verifyEmail")}
			</Button>
		{/if}
	</div>
</AuthFrame>
