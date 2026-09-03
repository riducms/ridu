<script lang="ts">
	import { Link } from "@hvniel/svelte-router";
	import { RiduError } from "@riducms/sdk";

	import { Banner } from "@admin/components/ui/banner";
	import { Button } from "@admin/components/ui/button";
	import { Input } from "@admin/components/ui/input";
	import { adminRoutePatterns } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import AuthFrame from "@admin/features/auth/auth-frame.svelte";

	const runtime = getAdminRuntime();
	const authCollection = $derived(runtime.authCollection);
	let email = $state("");
	let pending = $state(false);
	let sent = $state(false);
	let error = $state<string>();

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (authCollection === undefined) return;
		pending = true;
		error = undefined;
		try {
			await runtime.client.requestPasswordReset(authCollection.slug, email);
			sent = true;
		} catch (cause) {
			error =
				cause instanceof RiduError ? cause.message : runtime.i18n.t("auth:resetRequestFailed");
		} finally {
			pending = false;
		}
	}
</script>

<AuthFrame>
	{#if sent}
		<div class="grid gap-5">
			<h1 class="text-[32px] leading-tight font-medium tracking-[-0.025em] text-foreground">
				{runtime.i18n.t("auth:emailSent")}
			</h1>
			<p class="max-w-112 text-[13.5px] leading-5.5 text-foreground-muted">
				{runtime.i18n.t("auth:passwordResetSent")}
			</p>
			<Link
				class="w-fit text-[12.5px] text-foreground underline underline-offset-2 hover:text-foreground-muted"
				to={adminRoutePatterns.login}
			>
				{runtime.i18n.t("auth:backToLogin")}
			</Link>
		</div>
	{:else}
		<form class="grid gap-5" onsubmit={submit}>
			<div>
				<h1 class="text-[32px] leading-tight font-medium tracking-[-0.025em] text-foreground">
					{runtime.i18n.t("auth:forgotPassword")}
				</h1>
				<p class="mt-2 text-[13.5px] leading-5.5 text-foreground-muted">
					{runtime.i18n.t("auth:forgotPasswordDescription")}
				</p>
			</div>
			<label class="grid gap-2" for="reset-email">
				<span class="ridu-field-label">{runtime.i18n.t("auth:email")}</span>
				<Input id="reset-email" type="email" autocomplete="email" bind:value={email} required />
			</label>
			{#if error !== undefined}<Banner tone="destructive">{error}</Banner>{/if}
			<Button class="h-10 w-full" type="submit" size="lg" disabled={pending}>
				{pending ? runtime.i18n.t("auth:sending") : runtime.i18n.t("auth:sendResetLink")}
			</Button>
			<Link
				class="w-fit text-[12.5px] text-foreground underline underline-offset-2 hover:text-foreground-muted"
				to={adminRoutePatterns.login}
			>
				{runtime.i18n.t("auth:backToLogin")}
			</Link>
		</form>
	{/if}
</AuthFrame>
