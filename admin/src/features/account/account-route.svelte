<script lang="ts">
	import { connectDocumentLiveValidation } from "@admin/core/forms/live-validation.svelte";
	import PaletteIcon from "~icons/lucide/palette";
	import RotateCcwIcon from "~icons/lucide/rotate-ccw";
	import UserRoundIcon from "~icons/lucide/user-round";

	import { Banner } from "@admin/components/ui/banner";
	import { Button } from "@riducms/ui";
	import { Select, SelectContent, SelectItem, SelectTrigger } from "@admin/components/ui/select";
	import { Skeleton } from "@admin/components/ui/skeleton";
	import { FormController, FormValidationError } from "@admin/core/forms/form-controller.svelte";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import { adminRoutePatterns } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime, type ThemePreference } from "@admin/core/runtime/admin-runtime.svelte";
	import { getAdminBootstrapCoordinator } from "@admin/core/bootstrap/admin-bootstrap";
	import type { SchemaField } from "@riducms/protocol";
	import FieldLayout from "@admin/fields/field-layout.svelte";
	import AccountNavigation from "@admin/features/account/account-navigation.svelte";
	import { useLocation } from "@hvniel/svelte-router";

	const runtime = getAdminRuntime();
	const bootstrap = getAdminBootstrapCoordinator();
	const notifications = getNotificationCenter();
	const location = useLocation();

	const form = new FormController({}, runtime.i18n);
	connectDocumentLiveValidation(form, runtime.client);

	let profileRequest: AbortController | undefined;
	let activeProfileKey = "";

	let loading = $state(true);
	let error = $state<string>();

	let profilePending = $state(false);

	let pendingThemeWrites = $state(0);
	const themePending = $derived(pendingThemeWrites > 0);
	let languagePending = $state(false);
	let timeZonePending = $state(false);
	let resetPending = $state(false);

	const collection = $derived(runtime.authCollection);
	const user = $derived(runtime.session?.user);
	const sessionID = $derived(runtime.session?.id);
	const userID = $derived(runtime.session?.user.id);
	const requestedLocale = $derived(new URLSearchParams(location.current.search).get("locale"));
	const contentLocale = $derived(runtime.resolveContentLocale(requestedLocale));

	const selectedLanguageLabel = $derived.by(() => {
		const language = runtime.i18n.languages.find((item) => item.code === runtime.i18n.language);

		return language === undefined
			? runtime.i18n.language
			: runtime.i18n.text(language.label, language.labelTranslations);
	});

	const selectedTimeZoneLabel = $derived.by(() => {
		const timeZone = runtime.i18n.timeZones.find((item) => item.id === runtime.i18n.timeZone);

		return timeZone === undefined
			? runtime.i18n.timeZone
			: runtime.i18n.text(timeZone.label, timeZone.labelTranslations);
	});

	const accountComponents = runtime.config.extensions.account.filter(
		(component) => component.surface === "profile"
	);
	const replacement = accountComponents.find((component) => component.position === "replace");
	const beforeComponents = accountComponents.filter((component) => component.position === "before");
	const afterComponents = accountComponents.filter(
		(component) => component.position === undefined || component.position === "after"
	);

	const host = {
		refreshUser,
		logout,
		notify: (tone: "success" | "error", title: string, message?: string) =>
			notifications[tone]({ title, message }),
	};

	function adoptPreparedProfile(
		slug: string,
		id: string,
		locale: string | undefined,
		fields: readonly SchemaField[]
	) {
		// The coordinator binds this exact pathname and query to the current runtime context. The
		// effect's owner key prevents a retained account route from reusing it after a session change.
		const route = bootstrap.routeData(location.current.pathname, location.current.search);
		if (route?.kind !== "account") return false;

		const { document, access } = route.document;
		profileRequest?.abort();
		profileRequest = undefined;
		profilePending = false;
		loading = false;
		error = undefined;
		form.setResource({ collection: slug, id });
		form.setLocalization(locale);
		if (document.error !== undefined || access.error !== undefined) {
			form.reset({}, fields);
			form.setAccess(undefined, "update");
			error = (document.error ?? access.error)?.message;

			return true;
		}

		form.reset(document.value, fields);
		form.setLocalization(locale, document.value._localization?.sources);
		form.setAccess(access.value, "update");

		return true;
	}

	$effect.pre(() => {
		const slug = collection?.slug;
		const owner = sessionID;
		const id = userID;
		const locale = contentLocale;
		const fields = collection?.fields;
		if (slug === undefined || owner === undefined || id === undefined || fields === undefined)
			return;

		const key = `${runtime.manifestRevision}:${slug}:${owner}:${id}:${locale ?? "default"}`;
		if (key === activeProfileKey) return;

		activeProfileKey = key;
		if (adoptPreparedProfile(slug, id, locale, fields)) return;

		const request = beginProfileRequest();
		profilePending = false;
		load(slug, owner, id, locale, request);
	});

	$effect(() => {
		const unregisterBlocker = runtime.registerContentLocaleBlocker(
			() => form.dirty || form.submitting
		);

		return () => {
			unregisterBlocker();
			profileRequest?.abort();
			form.disposeBindings();
		};
	});

	function beginProfileRequest() {
		profileRequest?.abort();
		const request = new AbortController();
		profileRequest = request;

		return request;
	}

	function ownsProfileRequest(request: AbortController, owner: string) {
		return profileRequest === request && !request.signal.aborted && runtime.session?.id === owner;
	}

	async function load(
		slug: string,
		owner: string,
		id: string,
		locale: string | undefined,
		request: AbortController
	) {
		form.setResource({ collection: slug, id });
		form.setLocalization(locale);
		loading = true;
		error = undefined;

		try {
			const [document, access] = await Promise.all([
				runtime.client.find(slug, id, { signal: request.signal, locale }),
				runtime.client.collectionAccess(slug, { id, signal: request.signal, locale }),
			]);
			if (!ownsProfileRequest(request, owner)) return;

			form.reset(document, collection?.fields ?? []);
			form.setLocalization(locale, document._localization?.sources);
			form.setAccess(access, "update");
		} catch (cause) {
			if (ownsProfileRequest(request, owner)) {
				error =
					cause instanceof Error ? cause.message : runtime.i18n.t("account:profileLoadFailed");
			}
		} finally {
			if (ownsProfileRequest(request, owner)) loading = false;

			if (profileRequest === request) profileRequest = undefined;
		}
	}

	async function saveProfile(event: SubmitEvent) {
		event.preventDefault();
		if (collection === undefined || user === undefined) return;

		const slug = collection.slug;
		const owner = runtime.session?.id;
		const id = user.id;
		if (owner === undefined) return;

		const locale = contentLocale;
		const fields = collection.fields;
		const request = beginProfileRequest();
		loading = false;
		profilePending = true;
		error = undefined;

		try {
			const updated = await form.submit(fields, false, (values) =>
				runtime.client.update(slug, id, values, { locale, signal: request.signal })
			);
			if (!ownsProfileRequest(request, owner)) return;

			const activeSession = runtime.session;
			if (activeSession === undefined) return;

			form.reset(updated);
			form.setLocalization(locale, updated._localization?.sources);
			runtime.session = { ...activeSession, user: updated };
			runtime.documentsChanged();
			notifications.success({ title: runtime.i18n.t("account:profileUpdated") });
		} catch (cause) {
			if (ownsProfileRequest(request, owner) && !(cause instanceof FormValidationError)) {
				error =
					cause instanceof Error ? cause.message : runtime.i18n.t("account:profileSaveFailed");
			}
		} finally {
			if (ownsProfileRequest(request, owner)) profilePending = false;

			if (profileRequest === request) profileRequest = undefined;
		}
	}

	async function updateTheme(preference: ThemePreference) {
		pendingThemeWrites += 1;
		error = undefined;

		try {
			await runtime.setTheme(preference);
			notifications.success({ title: runtime.i18n.t("account:themeSaved") });
		} catch (cause) {
			error = cause instanceof Error ? cause.message : runtime.i18n.t("account:themeSaveFailed");
		} finally {
			pendingThemeWrites -= 1;
		}
	}

	async function updateLanguage(language: string) {
		if (!runtime.i18n.languages.some((item) => item.code === language)) return;

		languagePending = true;
		error = undefined;

		try {
			await runtime.i18n.setLanguage(language);
			notifications.success({ title: runtime.i18n.t("account:languageSaved") });
		} catch (cause) {
			error = cause instanceof Error ? cause.message : runtime.i18n.t("account:languageSaveFailed");
		} finally {
			languagePending = false;
		}
	}

	async function updateTimeZone(timeZone: string) {
		if (!runtime.i18n.timeZones.some((item) => item.id === timeZone)) return;

		timeZonePending = true;
		error = undefined;

		try {
			await runtime.i18n.setTimeZone(timeZone);
			notifications.success({ title: runtime.i18n.t("account:timeZoneSaved") });
		} catch (cause) {
			error = cause instanceof Error ? cause.message : runtime.i18n.t("account:timeZoneSaveFailed");
		} finally {
			timeZonePending = false;
		}
	}

	async function resetPreferences() {
		resetPending = true;
		error = undefined;

		try {
			await runtime.resetPreferences();
			notifications.success({ title: runtime.i18n.t("account:preferencesReset") });
		} catch (cause) {
			error =
				cause instanceof Error ? cause.message : runtime.i18n.t("account:preferencesResetFailed");
		} finally {
			resetPending = false;
		}
	}

	async function refreshUser() {
		if (collection === undefined || user === undefined || form.submitting) return undefined;

		const owner = runtime.session?.id;
		const id = user.id;
		if (owner === undefined) return undefined;

		const locale = contentLocale;
		const request = beginProfileRequest();

		try {
			const document = await runtime.client.find(collection.slug, id, {
				locale,
				signal: request.signal,
			});
			if (!ownsProfileRequest(request, owner)) return undefined;

			const activeSession = runtime.session;
			if (activeSession === undefined) return undefined;

			runtime.session = { ...activeSession, user: document };
			form.reset(document);
			form.setLocalization(locale, document._localization?.sources);
			loading = false;

			return document;
		} catch (cause) {
			if (!ownsProfileRequest(request, owner)) return undefined;

			throw cause;
		} finally {
			if (profileRequest === request) profileRequest = undefined;
		}
	}

	async function logout() {
		const owner = runtime.session?.id;
		if (owner === undefined) return;

		const request = beginProfileRequest();

		try {
			await runtime.client.logout({ signal: request.signal });
			if (!ownsProfileRequest(request, owner)) return;

			await runtime.hardNavigate(adminRoutePatterns.login);
		} catch (cause) {
			if (!ownsProfileRequest(request, owner)) return;

			throw cause;
		} finally {
			if (profileRequest === request) profileRequest = undefined;
		}
	}
</script>

{#snippet defaultView()}
	<div class="mx-auto grid w-full max-w-[820px] gap-8 px-5 py-8 sm:px-8 sm:py-11">
		<header class="grid gap-2">
			<p class="font-mono text-[9.5px] tracking-[0.16em] text-foreground-faint uppercase">
				{runtime.i18n.t("account:account")}
			</p>
			<h1 class="text-[28px] leading-tight font-semibold tracking-[-0.015em] text-foreground">
				{runtime.i18n.t("account:profileAndPreferences")}
			</h1>
			<p class="max-w-[590px] text-[13.5px] leading-5.5 text-foreground-muted">
				{runtime.i18n.t("account:profileDescription")}
			</p>
		</header>

		{#if runtime.manifest !== undefined}
			{#each beforeComponents as component (component.key)}
				<component.component
					manifest={runtime.manifest}
					user={runtime.session?.user}
					surface="profile"
					{defaultView}
					{host}
					i18n={runtime.i18n}
				/>
			{/each}
		{/if}

		<AccountNavigation active="profile" />

		{#if error !== undefined}
			<Banner tone="destructive">{error}</Banner>
		{/if}

		<section class="grid gap-4" aria-labelledby="profile-heading">
			<header class="flex items-center gap-3">
				<UserRoundIcon class="size-4 text-foreground-sub" aria-hidden="true" />
				<div>
					<h2 id="profile-heading" class="text-[15px] font-semibold text-foreground">
						{runtime.i18n.t("account:profile")}
					</h2>
					<p class="text-[12px] text-foreground-faint">
						{runtime.i18n.t("account:profileRules")}
					</p>
				</div>
			</header>
			{#if loading}
				<div
					class="grid gap-4 rounded-[4px] border border-control-border bg-control p-5"
					data-ridu-loading-surface="account"
				>
					<Skeleton class="h-10 w-full" /><Skeleton class="h-24 w-full" />
				</div>
			{:else if collection !== undefined}
				<form
					class="grid gap-6 rounded-[4px] border border-control-border bg-control p-5"
					onsubmit={saveProfile}
				>
					<FieldLayout fields={collection.fields} {form} />
					<Button
						class="justify-self-start"
						type="submit"
						disabled={profilePending || !form.dirty || form.access?.operations.update !== true}
						aria-busy={form.submitting}
					>
						{runtime.i18n.t("account:saveProfile")}
					</Button>
				</form>
			{/if}
		</section>

		<section class="grid gap-4" aria-labelledby="appearance-heading">
			<header class="flex items-center gap-3">
				<PaletteIcon class="size-4 text-foreground-sub" aria-hidden="true" />
				<div>
					<h2 id="appearance-heading" class="text-[15px] font-semibold text-foreground">
						{runtime.i18n.t("account:appearance")}
					</h2>
					<p class="text-[12px] text-foreground-faint">
						{runtime.i18n.t("account:preferencesStored")}
					</p>
				</div>
			</header>
			<div
				class="grid gap-5 rounded-[4px] border border-control-border bg-control p-5 sm:grid-cols-[1fr_auto] sm:items-end"
			>
				<div class="grid gap-4 sm:grid-cols-2">
					<label class="grid gap-2" for="account-theme">
						<span class="ridu-field-label">{runtime.i18n.t("account:theme")}</span>
						<Select
							type="single"
							value={runtime.themePreference}
							disabled={resetPending}
							onValueChange={(preference) => void updateTheme(preference as ThemePreference)}
						>
							<SelectTrigger
								id="account-theme"
								class="w-full"
								aria-label={runtime.i18n.t("account:theme")}
								aria-busy={themePending || undefined}
							>
								{runtime.themePreference === "system"
									? runtime.i18n.t("account:systemTheme")
									: runtime.themePreference === "light"
										? runtime.i18n.t("account:lightTheme")
										: runtime.i18n.t("account:darkTheme")}
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="system" label={runtime.i18n.t("account:systemTheme")} />
								<SelectItem value="light" label={runtime.i18n.t("account:lightTheme")} />
								<SelectItem value="dark" label={runtime.i18n.t("account:darkTheme")} />
							</SelectContent>
						</Select>
					</label>
					{#if runtime.i18n.languages.length > 1}
						<label class="grid gap-2" for="account-language">
							<span class="ridu-field-label">{runtime.i18n.t("account:interfaceLanguage")}</span>
							<Select
								type="single"
								value={runtime.i18n.language}
								disabled={resetPending || languagePending}
								onValueChange={(language) => void updateLanguage(language)}
							>
								<SelectTrigger
									id="account-language"
									class="w-full"
									aria-label={runtime.i18n.t("account:interfaceLanguage")}
									aria-busy={languagePending || undefined}
								>
									{selectedLanguageLabel}
								</SelectTrigger>
								<SelectContent>
									{#each runtime.i18n.languages as language (language.code)}
										<SelectItem
											value={language.code}
											label={runtime.i18n.text(language.label, language.labelTranslations)}
										/>
									{/each}
								</SelectContent>
							</Select>
						</label>
					{/if}
					{#if runtime.i18n.timeZones.length > 0}
						<label class="grid gap-2" for="account-timezone">
							<span class="ridu-field-label">{runtime.i18n.t("account:timeZone")}</span>
							<Select
								type="single"
								value={runtime.i18n.timeZone}
								disabled={resetPending || timeZonePending}
								onValueChange={(timeZone) => void updateTimeZone(timeZone)}
							>
								<SelectTrigger
									id="account-timezone"
									class="w-full"
									aria-label={runtime.i18n.t("account:timeZone")}
									aria-busy={timeZonePending || undefined}
								>
									{selectedTimeZoneLabel}
								</SelectTrigger>
								<SelectContent>
									{#each runtime.i18n.timeZones as timeZone (timeZone.id)}
										<SelectItem
											value={timeZone.id}
											label={runtime.i18n.text(timeZone.label, timeZone.labelTranslations)}
										/>
									{/each}
								</SelectContent>
							</Select>
						</label>
					{/if}
				</div>
				<Button variant="outline" disabled={resetPending} onclick={resetPreferences}>
					<RotateCcwIcon class="size-3.5" aria-hidden="true" />
					{resetPending
						? runtime.i18n.t("account:resettingPreferences")
						: runtime.i18n.t("account:resetAllPreferences")}
				</Button>
			</div>
		</section>
		{#if runtime.manifest !== undefined}
			{#each afterComponents as component (component.key)}
				<component.component
					manifest={runtime.manifest}
					user={runtime.session?.user}
					surface="profile"
					{defaultView}
					{host}
					i18n={runtime.i18n}
				/>
			{/each}
		{/if}
	</div>
{/snippet}

{#if runtime.manifest !== undefined && replacement !== undefined}
	<replacement.component
		manifest={runtime.manifest}
		user={runtime.session?.user}
		surface="profile"
		{defaultView}
		{host}
		i18n={runtime.i18n}
	/>
{:else}
	{@render defaultView()}
{/if}
