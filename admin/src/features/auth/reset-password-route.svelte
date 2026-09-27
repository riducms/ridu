<script lang="ts">
	import { tick } from "svelte";
	import { Link, useLocation } from "@hvniel/svelte-router";
	import { RiduError } from "@riducms/sdk";

	import { Banner } from "@admin/components/ui/banner";
	import { Button } from "@riducms/ui";
	import { adminRoutePatterns } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import AuthField from "@admin/features/auth/auth-field.svelte";
	import AuthFrame from "@admin/features/auth/auth-frame.svelte";

	const runtime = getAdminRuntime();
	const location = useLocation();

	const token = $derived(new URLSearchParams(location.current.search).get("token") ?? "");
	const authCollection = $derived(runtime.authCollection);
	const owner = $derived(JSON.stringify([authCollection?.id, authCollection?.slug, token]));

	let password = $state("");
	let confirmation = $state("");
	let pending = $state(false);
	let complete = $state(false);
	let error = $state<string>();
	let confirmationError = $state<string>();
	let passwordErrors = $state<string[]>([]);
	let request: AbortController | undefined;
	let ownerGeneration = 0;

	$effect.pre(() => {
		// A retained route must clear credentials and outcomes before rendering a new token owner.
		owner;
		password = "";
		confirmation = "";
		pending = false;
		complete = false;
		error = undefined;
		confirmationError = undefined;
		passwordErrors = [];
		return () => {
			ownerGeneration += 1;
			request?.abort();
			request = undefined;
		};
	});

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (authCollection === undefined || token === "" || pending || complete) return;
		const generation = ownerGeneration;
		const requestOwner = owner;

		error = undefined;
		passwordErrors = [];
		confirmationError = undefined;
		if (password !== confirmation) {
			confirmationError = runtime.i18n.t("auth:passwordMismatch");
			await tick();
			if (generation === ownerGeneration && requestOwner === owner)
				document.getElementById("reset-confirm")?.focus();
			return;
		}

		const activeRequest = new AbortController();
		request = activeRequest;
		const current = () => request === activeRequest && requestOwner === owner;
		pending = true;

		try {
			await runtime.client.auth.resetPassword(
				{ collection: authCollection.slug, token, password },
				{
					signal: activeRequest.signal,
				}
			);
			if (!current()) return;
			complete = true;
			password = "";
			confirmation = "";
		} catch (cause) {
			if (!current()) return;
			if (cause instanceof RiduError) {
				passwordErrors = cause.issues
					.filter((issue) => issue.path === "password")
					.map((issue) => issue.message);
			}

			if (passwordErrors.length === 0) {
				error =
					cause instanceof RiduError ? cause.message : runtime.i18n.t("auth:passwordResetFailed");
			}
		} finally {
			if (current()) {
				pending = false;
				if (passwordErrors.length > 0) {
					await tick();
					if (current()) document.getElementById("reset-password")?.focus();
				}
				if (request === activeRequest) request = undefined;
			}
		}
	}
</script>

<AuthFrame>
	<div class="ridu-auth__stack">
		<h1 class="ridu-auth__heading">
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
			<Link class="ridu-auth__link" to={terminalState.to}>
				{terminalState.action}
			</Link>
		{:else}
			<form class="ridu-auth__form" onsubmit={submit}>
				<AuthField
					disabled={pending}
					id="reset-password"
					label={runtime.i18n.t("account:newPassword")}
					type="password"
					autocomplete="new-password"
					minlength={authCollection?.authSettings?.passwordMinLength ?? 8}
					bind:value={password}
					errors={passwordErrors}
					oninput={() => {
						passwordErrors = [];
						confirmationError = undefined;
					}}
				/>
				<AuthField
					disabled={pending}
					id="reset-confirm"
					label={runtime.i18n.t("auth:confirmPassword")}
					type="password"
					autocomplete="new-password"
					bind:value={confirmation}
					errors={confirmationError === undefined ? [] : [confirmationError]}
					oninput={() => (confirmationError = undefined)}
					showLabel={runtime.i18n.t("auth:showPasswordConfirmation")}
					hideLabel={runtime.i18n.t("auth:hidePasswordConfirmation")}
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
					{pending
						? runtime.i18n.t("auth:resettingPassword")
						: runtime.i18n.t("auth:resetPassword")}
				</Button>
			</form>
		{/if}
	</div>
</AuthFrame>
