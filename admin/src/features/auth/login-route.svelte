<script lang="ts">
	import { Link, useLocation } from "@hvniel/svelte-router";
	import { RiduError } from "@riducms/sdk";
	import EyeIcon from "~icons/lucide/eye";
	import EyeOffIcon from "~icons/lucide/eye-off";

	import { Banner } from "@admin/components/ui/banner";
	import { Button } from "@admin/components/ui/button";
	import { Input } from "@admin/components/ui/input";
	import { adminRedirectFromSearch, adminRoutePatterns } from "@admin/core/routing/admin-paths";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import AuthBrand from "@admin/features/auth/auth-brand.svelte";
	import AuthFrame from "@admin/features/auth/auth-frame.svelte";

	const runtime = getAdminRuntime();
	const notifications = getNotificationCenter();
	const location = $derived(useLocation());
	const postLoginPath = $derived(adminRedirectFromSearch(location.search));
	let email = $state("");
	let password = $state("");
	let passwordVisible = $state(false);
	let pending = $state(false);
	let error = $state<string>();
	const authCollection = $derived(runtime.authCollection);
	const replacement = $derived(
		runtime.loginComponents.find((component) => component.position === "replace")
	);
	const loginLogo = $derived(
		runtime.brandComponents.find((component) => component.surface === "loginLogo")
	);
	const beforeComponents = $derived(
		runtime.loginComponents.filter((component) => component.position === "before")
	);
	const afterComponents = $derived(
		runtime.loginComponents.filter(
			(component) => component.position === undefined || component.position === "after"
		)
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
		const session = await runtime.client.login(authCollection.slug, credentials);
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
		// Keep this route mounted until access and the destination are ready.
		// Assigning the session before the await swaps the route tree and leaves
		// this component's navigate function inactive.
		runtime.session = session;
		await Promise.all([
			runtime.loadTheme(),
			runtime.loadContentLocale(destinationLocale),
			runtime.i18n.loadPreferences(),
		]);
		const preferredAccessAuthoritative = await runtime.refreshAccess(runtime.contentLocale);
		if (
			preferredAccessAuthoritative &&
			runtime.collectionOperations[authCollection.slug]?.admin !== true
		) {
			try {
				await runtime.client.logout();
			} catch {
				// The shell remains denied even if server cleanup fails.
			}
			runtime.session = undefined;
			throw new Error(runtime.i18n.t("auth:adminAccessDenied"));
		}
		// Assigning the authenticated session swaps this route for the authenticated
		// route tree. Its /login route owns the deferred redirect so this component
		// never reads route-owned derived state after it has been destroyed.
	}

	async function login() {
		pending = true;
		error = undefined;
		try {
			await signIn({ email, password });
		} catch (cause) {
			error =
				cause instanceof RiduError || cause instanceof Error
					? cause.message
					: runtime.i18n.t("auth:loginFailed");
		} finally {
			pending = false;
		}
	}

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		await login();
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

		<form class="grid gap-5" onsubmit={handleSubmit}>
			<h1 class="sr-only">{runtime.i18n.t("auth:login")}</h1>

			<label class="grid gap-2" for="ridu-login-email">
				<span class="ridu-field-label text-foreground-body">{runtime.i18n.t("auth:email")}</span>
				<Input
					id="ridu-login-email"
					type="email"
					bind:value={email}
					autocomplete="email"
					placeholder={runtime.i18n.t("auth:emailPlaceholder")}
					required
				/>
			</label>

			<div class="grid gap-2">
				<label class="ridu-field-label text-foreground-body" for="ridu-login-password">
					{runtime.i18n.t("auth:password")}
				</label>
				<span class="relative block">
					<Input
						id="ridu-login-password"
						class="pe-11"
						type={passwordVisible ? "text" : "password"}
						bind:value={password}
						autocomplete="current-password"
						placeholder={runtime.i18n.t("auth:passwordPlaceholder")}
						required
					/>
					<Button
						variant="ghost"
						size="icon-sm"
						class="absolute top-1/2 end-2.5 -translate-y-1/2"
						onclick={() => (passwordVisible = !passwordVisible)}
						aria-label={passwordVisible
							? runtime.i18n.t("auth:hidePassword")
							: runtime.i18n.t("auth:showPassword")}
					>
						{#if passwordVisible}
							<EyeOffIcon class="size-3.5" />
						{:else}
							<EyeIcon class="size-3.5" />
						{/if}
					</Button>
				</span>
				{#if authCollection?.authSettings?.passwordReset}
					<Link
						class="w-fit text-[12px] text-foreground underline underline-offset-2 hover:text-foreground-muted"
						to={adminRoutePatterns.forgotPassword}
					>
						{runtime.i18n.t("auth:forgotPassword")}
					</Link>
				{/if}
			</div>

			{#if error !== undefined}
				<Banner tone="destructive">
					<span class="size-2 shrink-0 rounded-full bg-destructive" aria-hidden="true"></span>
					{error}
				</Banner>
			{/if}

			<Button class="mt-1 h-10 w-full" type="submit" size="lg" disabled={pending}>
				{pending ? runtime.i18n.t("auth:signingIn") : runtime.i18n.t("auth:login")}
			</Button>
			{#if authCollection?.authSettings?.verifyEmail}
				<Link
					class="text-center text-[11.5px] text-foreground-muted hover:text-primary"
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
