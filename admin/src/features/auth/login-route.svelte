<script lang="ts">
	import { Link, useLocation } from "@hvniel/svelte-router";

	import { Banner } from "@admin/components/ui/banner";
	import { Button } from "@riducms/ui";
	import { adminRedirectFromSearch, adminRoutePatterns } from "@admin/core/routing/admin-paths";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import AuthBrand from "@admin/features/auth/auth-brand.svelte";
	import AuthField from "@admin/features/auth/auth-field.svelte";
	import AuthFrame from "@admin/features/auth/auth-frame.svelte";

	const runtime = getAdminRuntime();
	const extensions = runtime.config.extensions;
	const notifications = getNotificationCenter();
	const location = useLocation();

	const postLoginPath = $derived(adminRedirectFromSearch(location.current.search));
	const setupNotice = $derived(new URLSearchParams(location.current.search).get("setup"));
	const setupMessage = $derived(
		setupNotice === "created"
			? "auth:accountReady"
			: setupNotice === "completed"
				? "auth:firstAccountExists"
				: setupNotice === "denied"
					? "auth:firstAccountNoAdminAccess"
					: undefined
	);

	let email = $state("");
	let password = $state("");
	let pending = $state(false);

	const authCollection = $derived(runtime.authCollection);
	const replacement = extensions.login.find((component) => component.position === "replace");
	const loginLogo = extensions.branding.find((component) => component.surface === "loginLogo");
	const beforeComponents = extensions.login.filter((component) => component.position === "before");
	const afterComponents = extensions.login.filter(
		(component) => component.position === undefined || component.position === "after"
	);

	const host = {
		login: signIn,
		notify: (tone: "success" | "error", title: string, message?: string) =>
			notifications[tone]({ title, message }),
	};

	async function signIn(credentials: { email: string; password: string }) {
		if (authCollection === undefined) {
			throw new Error(runtime.i18n.t("auth:configuredCollectionUnavailable"));
		}

		await runtime.client.login(authCollection.slug, credentials);
		const destinationLocale = runtime.contentLocaleForPath(postLoginPath);
		const destinationAccessAuthoritative = await runtime.refreshAccess(
			destinationLocale ?? runtime.contentLocale
		);
		if (
			destinationAccessAuthoritative &&
			runtime.collectionOperations[authCollection.slug]?.admin !== true
		) {
			try {
				await runtime.client.logout();
			} catch {
				// The shell remains denied even if server cleanup fails.
			}
			throw new Error(runtime.i18n.t("auth:adminAccessDenied"));
		}

		await runtime.hardNavigate(postLoginPath);
	}

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		pending = true;

		try {
			await signIn({ email, password });
		} catch (cause) {
			const message = cause instanceof Error ? cause.message : runtime.i18n.t("auth:loginFailed");
			notifications.error({ title: message });
		} finally {
			pending = false;
		}
	}
</script>

{#snippet brand()}
	{#if loginLogo !== undefined && runtime.manifest !== undefined}
		<loginLogo.component
			manifest={runtime.manifest}
			user={runtime.session?.user}
			surface="loginLogo"
			i18n={runtime.i18n}
		/>
	{:else}
		<AuthBrand />
	{/if}
{/snippet}

{#snippet defaultView()}
	<AuthFrame {brand}>
		{#if runtime.manifest !== undefined}
			{#each beforeComponents as component (component.key)}
				<component.component manifest={runtime.manifest} {defaultView} {host} i18n={runtime.i18n} />
			{/each}
		{/if}

		<form class="ridu-auth__form" onsubmit={handleSubmit}>
			<h1 class="ridu-auth__sr-only">{runtime.i18n.t("auth:login")}</h1>

			{#if setupMessage !== undefined}
				<Banner tone={setupNotice === "denied" ? "destructive" : "neutral"}>
					{runtime.i18n.t(setupMessage)}
				</Banner>
			{/if}

			<AuthField
				disabled={pending}
				id="ridu-login-email"
				label={runtime.i18n.t("auth:email")}
				autocomplete="email"
				bind:value={email}
			/>

			<div class="ridu-auth__field">
				<AuthField
					disabled={pending}
					id="ridu-login-password"
					label={runtime.i18n.t("auth:password")}
					type="password"
					autocomplete="current-password"
					bind:value={password}
				/>
				{#if authCollection?.authSettings?.passwordReset}
					<Link class="ridu-auth__link" to={adminRoutePatterns.forgotPassword}>
						{runtime.i18n.t("auth:forgotPassword")}
					</Link>
				{/if}
			</div>

			<Button
				class="ridu-auth__submit"
				type="submit"
				size="lg"
				disabled={pending}
				aria-busy={pending}
			>
				{pending ? runtime.i18n.t("auth:signingIn") : runtime.i18n.t("auth:login")}
			</Button>

			{#if authCollection?.authSettings?.verifyEmail}
				<Link
					class="ridu-auth__link ridu-auth__secondary-link"
					to={adminRoutePatterns.requestVerification}
				>
					{runtime.i18n.t("auth:requestVerification")}
				</Link>
			{/if}
		</form>
		{#if runtime.manifest !== undefined}
			{#each afterComponents as component (component.key)}
				<component.component manifest={runtime.manifest} {defaultView} {host} i18n={runtime.i18n} />
			{/each}
		{/if}
	</AuthFrame>
{/snippet}

{#if runtime.manifest !== undefined && replacement !== undefined}
	<replacement.component manifest={runtime.manifest} {defaultView} {host} i18n={runtime.i18n} />
{:else}
	{@render defaultView()}
{/if}
