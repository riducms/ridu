<script lang="ts">
	import { useNavigate } from "@hvniel/svelte-router";
	import KeyIcon from "~icons/lucide/key-round";
	import LaptopIcon from "~icons/lucide/laptop";
	import LockIcon from "~icons/lucide/lock-keyhole";
	import ShieldIcon from "~icons/lucide/shield-check";

	import { Banner } from "@admin/components/ui/banner";
	import { Button } from "@admin/components/ui/button";
	import { DateValueControl } from "@admin/components/ui/date-value-control";
	import { Input } from "@admin/components/ui/input";
	import {
		Dialog,
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle,
	} from "@admin/components/ui/dialog";
	import { adminRoutePatterns } from "@admin/core/routing/admin-paths";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { AccountSecurityController } from "@admin/features/auth/account-security-controller.svelte";
	import AccountNavigation from "@admin/features/auth/account-navigation.svelte";

	const runtime = getAdminRuntime();
	const notifications = getNotificationCenter();
	const navigate = useNavigate();
	// The authenticated route table is remounted when the session changes, so this
	// manifest capability is an intentional controller-lifetime snapshot.
	const controller = new AccountSecurityController(
		runtime.client,
		runtime.authCollection?.authSettings?.apiKeys === true,
		runtime.i18n.t
	);
	const { status, error, operation, sessions, apiKeys, createdAPIKey, apiKeysEnabled } =
		$derived(controller);
	let currentPassword = $state("");
	let nextPassword = $state("");
	let confirmPassword = $state("");
	let apiKeyName = $state("");
	let apiKeyExpiry = $state("");
	let destructiveAction = $state<{ kind: "session" | "api-key" | "logout-all"; id?: string }>();
	const accountComponents = $derived(
		runtime.accountComponents.filter((component) => component.surface === "security")
	);
	const replacement = $derived(
		accountComponents.find((component) => component.position === "replace")
	);
	const beforeComponents = $derived(
		accountComponents.filter((component) => component.position === "before")
	);
	const afterComponents = $derived(
		accountComponents.filter(
			(component) => component.position === undefined || component.position === "after"
		)
	);
	const host = {
		refreshUser,
		logout,
		notify: (tone: "success" | "error", title: string, message?: string) =>
			notifications[tone]({ title, message }),
	};

	$effect(() => {
		controller.load();
		return () => controller.destroy();
	});

	async function changePassword(event: SubmitEvent) {
		event.preventDefault();
		if (nextPassword !== confirmPassword) {
			notifications.error({ title: runtime.i18n.t("account:passwordMismatch") });
			return;
		}
		if (!(await controller.changePassword(currentPassword, nextPassword))) return;
		runtime.session = undefined;
		notifications.success({ title: runtime.i18n.t("account:passwordUpdated") });
		navigate(adminRoutePatterns.login, { replace: true });
	}

	async function createAPIKey(event: SubmitEvent) {
		event.preventDefault();
		const expiry = apiKeyExpiry === "" ? undefined : new Date(apiKeyExpiry).toISOString();
		if (!(await controller.createAPIKey(apiKeyName, expiry))) return;
		apiKeyName = "";
		apiKeyExpiry = "";
		notifications.success({ title: runtime.i18n.t("account:apiKeyCreated") });
	}

	async function revokeSession(id: string) {
		if (!(await controller.revokeSession(id))) return;
		notifications.success({ title: runtime.i18n.t("account:sessionRevoked") });
	}

	async function revokeAPIKey(id: string) {
		if (!(await controller.revokeAPIKey(id))) return;
		notifications.success({ title: runtime.i18n.t("account:apiKeyRevoked") });
	}

	async function logoutAll() {
		if (!(await controller.logoutAll())) return;
		runtime.session = undefined;
		notifications.success({ title: runtime.i18n.t("account:signedOutAll") });
		navigate(adminRoutePatterns.login, { replace: true });
	}

	async function refreshUser() {
		const collection = runtime.authCollection;
		const user = runtime.session?.user;
		if (collection === undefined || user === undefined) return undefined;
		const document = await runtime.client.find(collection.slug, user.id);
		if (runtime.session !== undefined) runtime.session = { ...runtime.session, user: document };
		return document;
	}

	async function logout() {
		await runtime.client.logout();
		runtime.session = undefined;
		navigate(adminRoutePatterns.login, { replace: true });
	}

	async function confirmDestructiveAction() {
		const action = destructiveAction;
		if (action === undefined) return;
		if (action.kind === "session" && action.id !== undefined) await revokeSession(action.id);
		if (action.kind === "api-key" && action.id !== undefined) await revokeAPIKey(action.id);
		if (action.kind === "logout-all") await logoutAll();
		destructiveAction = undefined;
	}

	async function copyAPIKey() {
		if (createdAPIKey === undefined) return;
		try {
			await navigator.clipboard.writeText(createdAPIKey.key);
			notifications.success({ title: runtime.i18n.t("account:apiKeyCopied") });
		} catch {
			notifications.error({ title: runtime.i18n.t("account:apiKeyCopyFailed") });
		}
	}

	function formatDate(value: string | undefined) {
		if (value === undefined || value === "") return runtime.i18n.t("account:never");
		return runtime.i18n.formatDate(value, { dateStyle: "medium", timeStyle: "short" });
	}
</script>

{#snippet defaultView()}
	<div class="mx-auto grid w-full max-w-[820px] gap-8 px-5 py-8 sm:px-8 sm:py-11">
		<header class="grid gap-2 border-b border-control-border pb-6">
			<p class="font-mono text-[9.5px] tracking-[0.16em] text-foreground-faint uppercase">
				{runtime.i18n.t("account:account")}
			</p>
			<h1 class="text-[28px] leading-tight font-semibold tracking-[-0.015em] text-foreground">
				{runtime.i18n.t("account:security")}
			</h1>
			<p class="max-w-[570px] text-[13.5px] leading-5.5 text-foreground-muted">
				{runtime.i18n.t("account:securityDescription")}
			</p>
		</header>

		{#if runtime.manifest !== undefined}
			{#each beforeComponents as component (component.key)}
				<component.component
					manifest={runtime.manifest}
					user={runtime.session?.user}
					surface="security"
					{defaultView}
					{host}
					i18n={runtime.i18n}
				/>
			{/each}
		{/if}

		<AccountNavigation active="security" />

		{#if error !== undefined}
			<Banner tone="destructive">{error}</Banner>
		{/if}

		<section class="grid gap-4" aria-labelledby="password-heading">
			<header class="flex items-center gap-3">
				<LockIcon class="size-4 text-foreground-sub" aria-hidden="true" />
				<div>
					<h2 id="password-heading" class="text-[15px] font-semibold text-foreground">
						{runtime.i18n.t("account:password")}
					</h2>
					<p class="text-[12px] text-foreground-faint">
						{runtime.i18n.t("account:passwordDescription")}
					</p>
				</div>
			</header>
			<form
				class="grid gap-3 rounded-[4px] border border-control-border bg-control p-5"
				onsubmit={changePassword}
			>
				<label class="grid gap-2" for="current-password">
					<span class="ridu-field-label">{runtime.i18n.t("account:currentPassword")}</span>
					<Input
						id="current-password"
						type="password"
						autocomplete="current-password"
						bind:value={currentPassword}
						required
					/>
				</label>
				<div class="grid gap-3 sm:grid-cols-2">
					<label class="grid gap-2" for="next-password">
						<span class="ridu-field-label">{runtime.i18n.t("account:newPassword")}</span>
						<Input
							id="next-password"
							type="password"
							autocomplete="new-password"
							minlength={runtime.authCollection?.authSettings?.passwordMinLength ?? 8}
							bind:value={nextPassword}
							required
						/>
					</label>
					<label class="grid gap-2" for="confirm-password">
						<span class="ridu-field-label">{runtime.i18n.t("auth:confirmPassword")}</span>
						<Input
							id="confirm-password"
							type="password"
							autocomplete="new-password"
							bind:value={confirmPassword}
							required
						/>
					</label>
				</div>
				<Button class="mt-1 justify-self-start" type="submit" disabled={operation !== undefined}>
					{operation === "password"
						? runtime.i18n.t("account:updatingPassword")
						: runtime.i18n.t("account:updatePassword")}
				</Button>
			</form>
		</section>

		<section class="grid gap-4" aria-labelledby="sessions-heading">
			<header class="flex items-center gap-3">
				<LaptopIcon class="size-4 text-foreground-sub" aria-hidden="true" />
				<div class="min-w-0 flex-1">
					<h2 id="sessions-heading" class="text-[15px] font-semibold text-foreground">
						{runtime.i18n.t("account:activeSessions")}
					</h2>
					<p class="text-[12px] text-foreground-faint">
						{runtime.i18n.t("account:sessionsDescription")}
					</p>
				</div>
				<Button
					variant="outline"
					onclick={() => (destructiveAction = { kind: "logout-all" })}
					disabled={operation !== undefined || sessions.length === 0}
				>
					{runtime.i18n.t("account:signOutAll")}
				</Button>
			</header>
			<div class="overflow-hidden rounded-[4px] border border-control-border bg-control">
				{const sessionMessage = $derived(
					status === "loading"
						? runtime.i18n.t("account:loadingSessions")
						: sessions.length === 0
							? runtime.i18n.t("account:noSessions")
							: undefined
				)}

				{#if sessionMessage !== undefined}
					<p class="px-5 py-6 text-[13px] text-foreground-muted">{sessionMessage}</p>
				{:else}
					{#each sessions as session (session.id)}
						<div
							class="flex flex-wrap items-center gap-3 border-b border-control-border px-5 py-4 last:border-b-0"
						>
							<ShieldIcon class="size-4 text-foreground-faint" aria-hidden="true" />
							<div class="min-w-[180px] flex-1">
								<p class="truncate text-[13px] text-foreground">
									{session.userAgent || runtime.i18n.t("account:unknownClient")}
								</p>
								<p class="mt-0.5 text-[10.5px] text-foreground-faint">
									{runtime.i18n.t("account:lastSeen", {
										date: formatDate(session.lastSeenAt),
									})}{session.ipAddress ? ` · ${session.ipAddress}` : ""}
								</p>
							</div>
							{#if session.current}
								<span
									class="rounded-full bg-success/10 px-2 py-1 text-[10px] font-medium text-success"
								>
									{runtime.i18n.t("account:currentSession")}
								</span>
							{:else}
								<Button
									variant="ghost"
									onclick={() => (destructiveAction = { kind: "session", id: session.id })}
									disabled={operation !== undefined}
								>
									{runtime.i18n.t("account:revoke")}
								</Button>
							{/if}
						</div>
					{/each}
				{/if}
			</div>
		</section>

		{#if apiKeysEnabled}
			<section class="grid gap-4" aria-labelledby="api-keys-heading">
				<header class="flex items-center gap-3">
					<KeyIcon class="size-4 text-foreground-sub" aria-hidden="true" />
					<div>
						<h2 id="api-keys-heading" class="text-[15px] font-semibold text-foreground">
							{runtime.i18n.t("account:apiKeys")}
						</h2>
						<p class="text-[12px] text-foreground-faint">
							{runtime.i18n.t("account:apiKeysDescription")}
						</p>
					</div>
				</header>
				<form
					class="grid gap-3 rounded-[4px] border border-control-border bg-control p-5 sm:grid-cols-[1fr_190px_auto] sm:items-end"
					onsubmit={createAPIKey}
				>
					<label class="grid gap-2" for="api-key-name">
						<span class="ridu-field-label">{runtime.i18n.t("account:name")}</span>
						<Input
							id="api-key-name"
							bind:value={apiKeyName}
							maxlength={100}
							required
							placeholder={runtime.i18n.t("account:apiKeyNamePlaceholder")}
						/>
					</label>
					<label class="grid gap-2" for="api-key-expiry">
						<span class="ridu-field-label">{runtime.i18n.t("account:expiration")}</span>
						<DateValueControl
							id="api-key-expiry"
							appearance="dayAndTime"
							value={apiKeyExpiry}
							label={runtime.i18n.t("account:expiration")}
							onValueChange={(next) => (apiKeyExpiry = next)}
						/>
					</label>
					<Button type="submit" disabled={operation !== undefined}>
						{runtime.i18n.t("account:createKey")}
					</Button>
				</form>
				{#if createdAPIKey !== undefined}
					<Banner>
						<div class="min-w-0 flex-1">
							<p class="font-medium">{runtime.i18n.t("account:copyAPIKeyNow")}</p>
							<code class="mt-2 block overflow-x-auto font-mono text-[11px]">
								{createdAPIKey.key}
							</code>
						</div>
						<div class="flex shrink-0 gap-2">
							<Button variant="outline" onclick={copyAPIKey}>
								{runtime.i18n.t("general:copy")}
							</Button><Button variant="ghost" onclick={controller.dismissCreatedAPIKey}>
								{runtime.i18n.t("general:done")}
							</Button>
						</div>
					</Banner>
				{/if}
				<div class="overflow-hidden rounded-[4px] border border-control-border bg-control">
					{#if apiKeys.length === 0}
						<p class="px-5 py-6 text-[13px] text-foreground-muted">
							{runtime.i18n.t("account:noAPIKeys")}
						</p>
					{:else}
						{#each apiKeys as key (key.id)}
							<div
								class="flex items-center gap-3 border-b border-control-border px-5 py-4 last:border-b-0"
							>
								<div class="min-w-0 flex-1">
									<p class="truncate text-[13px] text-foreground">{key.name}</p>
									<p class="mt-0.5 text-[10.5px] text-foreground-faint">
										{runtime.i18n.t("account:created", { date: formatDate(key.createdAt) })}
										· {runtime.i18n.t("account:expires", { date: formatDate(key.expiresAt) })}
									</p>
								</div>
								<Button
									variant="ghost"
									onclick={() => (destructiveAction = { kind: "api-key", id: key.id })}
									disabled={operation !== undefined}
								>
									{runtime.i18n.t("account:revoke")}
								</Button>
							</div>
						{/each}
					{/if}
				</div>
			</section>
		{/if}
		{#if runtime.manifest !== undefined}
			{#each afterComponents as component (component.key)}
				<component.component
					manifest={runtime.manifest}
					user={runtime.session?.user}
					surface="security"
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
		surface="security"
		{defaultView}
		{host}
		i18n={runtime.i18n}
	/>
{:else}
	{@render defaultView()}
{/if}

<Dialog
	bind:open={
		() => destructiveAction !== undefined,
		(open) => {
			if (!open) destructiveAction = undefined;
		}
	}
>
	<DialogContent>
		<DialogHeader>
			<p class="font-mono text-[10px] tracking-[0.13em] text-destructive uppercase">
				{runtime.i18n.t("account:securityAction")}
			</p>
			<DialogTitle class="text-[20px] leading-tight font-semibold">
				{destructiveAction?.kind === "logout-all"
					? runtime.i18n.t("account:confirmSignOutAll")
					: destructiveAction?.kind === "api-key"
						? runtime.i18n.t("account:confirmRevokeAPIKey")
						: runtime.i18n.t("account:confirmRevokeSession")}
			</DialogTitle>
			<DialogDescription class="mt-1 text-[13.5px] leading-5 text-foreground-muted">
				{destructiveAction?.kind === "logout-all"
					? runtime.i18n.t("account:signOutAllDescription")
					: runtime.i18n.t("account:revokeCredentialDescription")}
			</DialogDescription>
		</DialogHeader>
		<DialogFooter>
			<Button variant="outline" onclick={() => (destructiveAction = undefined)}>
				{runtime.i18n.t("general:cancel")}
			</Button>
			<Button
				variant="destructive"
				onclick={confirmDestructiveAction}
				disabled={operation !== undefined}
			>
				{runtime.i18n.t("general:confirm")}
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>
