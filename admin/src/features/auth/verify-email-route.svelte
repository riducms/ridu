<script lang="ts">
	import { Link, useLocation } from "@hvniel/svelte-router";
	import { RiduError } from "@riducms/sdk";

	import { Banner } from "@admin/components/ui/banner";
	import { Button } from "@admin/components/ui/button";
	import { adminRoutePatterns } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import AuthFrame from "@admin/features/auth/auth-frame.svelte";

	const runtime = getAdminRuntime();
	const location = $derived(useLocation());
	const token = $derived(new URLSearchParams(location.search).get("token") ?? "");
	const authCollection = $derived(runtime.authCollection);
	let pending = $state(false);
	let complete = $state(false);
	let error = $state<string>();

	async function verify() {
		if (authCollection === undefined || token === "") return;
		pending = true;
		error = undefined;
		try {
			await runtime.client.verifyEmail(authCollection.slug, token);
			complete = true;
		} catch (cause) {
			error =
				cause instanceof RiduError ? cause.message : runtime.i18n.t("auth:emailVerificationFailed");
		} finally {
			pending = false;
		}
	}
</script>

<AuthFrame>
	<div class="grid gap-5">
		<h1 class="text-[32px] leading-tight font-medium tracking-[-0.025em] text-foreground">
			{runtime.i18n.t("auth:verifyYourEmail")}
		</h1>
		{#if complete}
			<Banner>{runtime.i18n.t("auth:emailVerified")}</Banner>
			<Link
				class="w-fit text-[12.5px] text-foreground underline underline-offset-2 hover:text-foreground-muted"
				to={adminRoutePatterns.login}
			>
				{runtime.i18n.t("auth:backToLogin")}
			</Link>
		{:else if token === ""}
			<Banner tone="destructive">{runtime.i18n.t("auth:missingVerificationToken")}</Banner>
		{:else}
			<p class="text-[13.5px] leading-5.5 text-foreground-muted">
				{runtime.i18n.t("auth:verifyDescription")}
			</p>
			{#if error !== undefined}<Banner tone="destructive">{error}</Banner>{/if}
			<Button class="h-10 w-full" size="lg" onclick={verify} disabled={pending}>
				{pending ? runtime.i18n.t("auth:verifying") : runtime.i18n.t("auth:verifyEmail")}
			</Button>
		{/if}
	</div>
</AuthFrame>
