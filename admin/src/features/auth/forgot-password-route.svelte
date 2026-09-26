<script lang="ts">
	import { Link } from "@hvniel/svelte-router";
	import { RiduError } from "@riducms/sdk";

	import { Banner } from "@admin/components/ui/banner";
	import { Button } from "@riducms/ui";
	import { adminRoutePatterns } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import AuthField from "@admin/features/auth/auth-field.svelte";
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
		<div class="ridu-auth__stack">
			<h1 class="ridu-auth__heading">
				{runtime.i18n.t("auth:emailSent")}
			</h1>
			<p class="ridu-auth__description">
				{runtime.i18n.t("auth:passwordResetSent")}
			</p>
			<Link class="ridu-auth__link" to={adminRoutePatterns.login}>
				{runtime.i18n.t("auth:backToLogin")}
			</Link>
		</div>
	{:else}
		<form class="ridu-auth__form" onsubmit={submit}>
			<div class="ridu-auth__header">
				<h1 class="ridu-auth__heading">
					{runtime.i18n.t("auth:forgotPassword")}
				</h1>
				<p class="ridu-auth__description">
					{runtime.i18n.t("auth:forgotPasswordDescription")}
				</p>
			</div>
			<AuthField
				disabled={pending}
				id="reset-email"
				label={runtime.i18n.t("auth:email")}
				autocomplete="email"
				bind:value={email}
			/>
			{#if error !== undefined}
				<Banner tone="destructive">{error}</Banner>
			{/if}
			<Button
				class="ridu-auth__submit"
				type="submit"
				size="lg"
				disabled={pending}
				aria-busy={pending}
			>
				{pending ? runtime.i18n.t("auth:sending") : runtime.i18n.t("auth:sendResetLink")}
			</Button>
			<Link class="ridu-auth__link" to={adminRoutePatterns.login}>
				{runtime.i18n.t("auth:backToLogin")}
			</Link>
		</form>
	{/if}
</AuthFrame>
