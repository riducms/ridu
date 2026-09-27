<script lang="ts">
	import LogOutIcon from "~icons/lucide/log-out";
	import { adminRoutePatterns } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import "@admin/features/auth/logout-button.scss";

	const runtime = getAdminRuntime();
	const replacement = runtime.config.extensions.logoutButton;
	const host = { logout };

	let pending = $state(false);
	let error = $state<string>();

	async function logout() {
		if (pending) return;

		pending = true;
		error = undefined;

		try {
			await runtime.client.auth.logout();
			await runtime.hardNavigate(adminRoutePatterns.login);
		} catch (cause) {
			error = cause instanceof Error ? cause.message : runtime.i18n.t("account:signOutFailed");
		} finally {
			pending = false;
		}
	}
</script>

{#if replacement !== undefined && runtime.manifest !== undefined}
	<replacement.component
		manifest={runtime.manifest}
		user={runtime.session?.user}
		{host}
		i18n={runtime.i18n}
	/>
{:else}
	<button
		type="button"
		class="ridu-logout"
		disabled={pending}
		onclick={logout}
		aria-label={pending ? runtime.i18n.t("account:signingOut") : runtime.i18n.t("auth:logout")}
		title={runtime.i18n.t("auth:logout")}
	>
		<LogOutIcon aria-hidden="true" />
	</button>
{/if}
{#if error !== undefined}
	<p class="ridu-logout__error" role="alert">{error}</p>
{/if}
