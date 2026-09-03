<script lang="ts">
	import { Link, useLocation } from "@hvniel/svelte-router";
	import { RiduError } from "@riducms/sdk";

	import { Banner } from "@admin/components/ui/banner";
	import { Button } from "@admin/components/ui/button";
	import { Input } from "@admin/components/ui/input";
	import { adminRoutePatterns } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import AuthFrame from "@admin/features/auth/auth-frame.svelte";

	const runtime = getAdminRuntime();
	const location = $derived(useLocation());
	const token = $derived(new URLSearchParams(location.search).get("token") ?? "");
	const authCollection = $derived(runtime.authCollection);
	let password = $state("");
	let confirmation = $state("");
	let pending = $state(false);
	let complete = $state(false);
	let error = $state<string>();

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (authCollection === undefined || token === "") return;
		if (password !== confirmation) {
			error = runtime.i18n.t("auth:passwordMismatch");
			return;
		}
		pending = true;
		error = undefined;
		try {
			await runtime.client.resetPassword(authCollection.slug, token, password);
			complete = true;
			password = "";
			confirmation = "";
		} catch (cause) {
			error =
				cause instanceof RiduError ? cause.message : runtime.i18n.t("auth:passwordResetFailed");
		} finally {
			pending = false;
		}
	}
</script>

<AuthFrame>
	<div class="grid gap-5">
		<h1 class="text-[32px] leading-tight font-medium tracking-[-0.025em] text-foreground">
			{runtime.i18n.t("auth:chooseNewPassword")}
		</h1>
		{const terminalState = $derived(
			complete
				? {
						message: runtime.i18n.t("auth:passwordResetSuccess"),
						to: adminRoutePatterns.login,
						action: runtime.i18n.t("auth:backToLogin"),
					}
				: token === ""
					? {
							message: runtime.i18n.t("auth:missingResetToken"),
							tone: "destructive" as const,
							to: adminRoutePatterns.forgotPassword,
							action: runtime.i18n.t("auth:requestAnotherLink"),
						}
					: undefined
		)}

		{#if terminalState !== undefined}
			<Banner tone={terminalState.tone}>{terminalState.message}</Banner>
			<Link
				class="w-fit text-[12.5px] text-foreground underline underline-offset-2 hover:text-foreground-muted"
				to={terminalState.to}
			>
				{terminalState.action}
			</Link>
		{:else}
			<form class="grid gap-4" onsubmit={submit}>
				<label class="grid gap-2" for="reset-password">
					<span class="ridu-field-label">{runtime.i18n.t("account:newPassword")}</span>
					<Input
						id="reset-password"
						type="password"
						autocomplete="new-password"
						minlength={authCollection?.authSettings?.passwordMinLength ?? 8}
						bind:value={password}
						required
					/>
				</label>
				<label class="grid gap-2" for="reset-confirm">
					<span class="ridu-field-label">{runtime.i18n.t("auth:confirmPassword")}</span>
					<Input
						id="reset-confirm"
						type="password"
						autocomplete="new-password"
						bind:value={confirmation}
						required
					/>
				</label>
				{#if error !== undefined}<Banner tone="destructive">{error}</Banner>{/if}
				<Button class="h-10 w-full" type="submit" size="lg" disabled={pending}>
					{pending
						? runtime.i18n.t("auth:resettingPassword")
						: runtime.i18n.t("auth:resetPassword")}
				</Button>
			</form>
		{/if}
	</div>
</AuthFrame>
