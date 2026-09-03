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
			await runtime.client.requestVerification(authCollection.slug, email);
			sent = true;
		} catch (cause) {
			error =
				cause instanceof RiduError
					? cause.message
					: runtime.i18n.t("auth:verificationRequestFailed");
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
		{#if sent}
			<Banner>{runtime.i18n.t("auth:verificationSent")}</Banner>
			<Link
				class="w-fit text-[12.5px] text-foreground underline underline-offset-2 hover:text-foreground-muted"
				to={adminRoutePatterns.login}
			>
				{runtime.i18n.t("auth:backToLogin")}
			</Link>
		{:else}
			<form class="grid gap-4" onsubmit={submit}>
				<p class="text-[13px] leading-5 text-foreground-muted">
					{runtime.i18n.t("auth:verificationDescription")}
				</p>
				<label class="grid gap-2" for="verification-email">
					<span class="ridu-field-label">{runtime.i18n.t("auth:email")}</span>
					<Input
						id="verification-email"
						type="email"
						autocomplete="email"
						bind:value={email}
						required
					/>
				</label>
				{#if error !== undefined}<Banner tone="destructive">{error}</Banner>{/if}
				<Button class="h-10 w-full" type="submit" size="lg" disabled={pending}>
					{pending ? runtime.i18n.t("auth:sending") : runtime.i18n.t("auth:sendVerificationLink")}
				</Button>
			</form>
		{/if}
	</div>
</AuthFrame>
