<script lang="ts">
	import { Link, useNavigate } from "@hvniel/svelte-router";
	import LogOutIcon from "~icons/lucide/log-out";
	import SettingsIcon from "~icons/lucide/settings";
	import UserRoundIcon from "~icons/lucide/user-round";

	import { Button, buttonVariants } from "@admin/components/ui/button";
	import { Popover, PopoverContent, PopoverTrigger } from "@admin/components/ui/popover";
	import { adminRoutePatterns } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	const runtime = getAdminRuntime();
	let { compact = false }: { compact?: boolean } = $props();
	const navigate = useNavigate();
	let open = $state(false);
	let pending = $state(false);
	let error = $state<string>();
	const authCollection = $derived(runtime.authCollection);
	const user = $derived(runtime.session?.user);
	const logoutHost = { logout };
	const accountAvatar = $derived(
		runtime.brandComponents.find((component) => component.surface === "accountAvatar")
	);
	const settingsComponents = $derived(
		runtime.shellComponents.filter((component) => component.position === "settingsMenu")
	);
	const displayName = $derived.by(() => {
		const candidate = user?.name;
		if (typeof candidate === "string" && candidate.trim().length > 0) return candidate;
		const identityField = authCollection?.authSettings?.identityField;
		const identity = identityField === undefined ? undefined : user?.[identityField];
		if (typeof identity === "string" && identity.trim().length > 0) return identity;
		return runtime.i18n.t("account:signedInUser");
	});
	const identity = $derived.by(() => {
		const identityField = authCollection?.authSettings?.identityField;
		const value = identityField === undefined ? undefined : user?.[identityField];
		return typeof value === "string" && value !== displayName ? value : undefined;
	});
	const initials = $derived(
		displayName
			.split(/\s+/)
			.filter(Boolean)
			.slice(0, 2)
			.map((part) => part.slice(0, 1).toLocaleUpperCase(runtime.i18n.language))
			.join("") || "Z"
	);

	async function logout() {
		pending = true;
		error = undefined;
		try {
			await runtime.client.logout();
			runtime.session = undefined;
			open = false;
			navigate(adminRoutePatterns.login, { replace: true });
		} catch (cause) {
			error = cause instanceof Error ? cause.message : runtime.i18n.t("account:signOutFailed");
		} finally {
			pending = false;
		}
	}
</script>

<Popover bind:open>
	<PopoverTrigger
		class={[
			"flex items-center rounded-full text-left transition-colors hover:text-foreground focus-visible:outline-primary/60",
			compact ? "size-7 justify-center" : "w-full gap-2.25 px-0.5 py-0",
		]}
		aria-label={runtime.i18n.t("account:accountMenuLabel", { name: displayName })}
	>
		{#if accountAvatar !== undefined && runtime.manifest !== undefined}
			<accountAvatar.component
				manifest={runtime.manifest}
				{user}
				surface="accountAvatar"
				i18n={runtime.i18n}
			/>
		{:else}
			<span
				class="font-mono grid size-6 shrink-0 place-items-center rounded-full bg-sidebar-accent text-[8.5px] font-semibold tracking-[0.04em] text-sidebar-accent-foreground"
				aria-hidden="true"
			>
				{initials}
			</span>
		{/if}
		{#if !compact}
			<span class="min-w-0 flex-1">
				<span class="block truncate text-[12.5px] text-foreground">{displayName}</span>
				<span class="block truncate text-[10.5px] text-foreground-sub">
					{runtime.i18n.t("account:adminRole")}
				</span>
			</span>
			<SettingsIcon class="size-3.5 shrink-0 text-foreground-faint" aria-hidden="true" />
		{/if}
	</PopoverTrigger>
	<PopoverContent side="top" align="start" sideOffset={8} class="w-[212px] gap-1 p-2">
		<div class="border-b border-control-border px-2.5 py-2 pb-3">
			<p class="truncate text-[13px] font-medium text-foreground">{displayName}</p>
			{#if identity !== undefined}
				<p class="mt-0.5 truncate text-[10.5px] text-foreground-faint">{identity}</p>
			{/if}
		</div>
		{#if error !== undefined}
			<p class="px-2.5 py-2 text-[11.5px] leading-4 text-destructive" role="alert">{error}</p>
		{/if}
		<Link
			to={adminRoutePatterns.account}
			class={buttonVariants({
				variant: "ghost",
				class: "h-9 w-full justify-start gap-2 px-2.5 text-[12.5px]",
			})}
			onclick={() => (open = false)}
		>
			<UserRoundIcon class="size-3.5" aria-hidden="true" />
			{runtime.i18n.t("account:profileAndPreferences")}
		</Link>
		<Link
			to={adminRoutePatterns.accountSecurity}
			class={buttonVariants({
				variant: "ghost",
				class: "h-9 w-full justify-start gap-2 px-2.5 text-[12.5px]",
			})}
			onclick={() => (open = false)}
		>
			<SettingsIcon class="size-3.5" aria-hidden="true" />
			{runtime.i18n.t("account:accountSecurity")}
		</Link>
		{#if runtime.manifest !== undefined}
			{#each settingsComponents as component (component.key)}
				<component.component
					manifest={runtime.manifest}
					{user}
					position="settingsMenu"
					i18n={runtime.i18n}
				/>
			{/each}
		{/if}
		{#if runtime.logoutButton !== undefined && runtime.manifest !== undefined}
			<runtime.logoutButton.component
				manifest={runtime.manifest}
				{user}
				host={logoutHost}
				i18n={runtime.i18n}
			/>
		{:else}
			<Button
				variant="ghost"
				class="h-9 w-full justify-start gap-2 px-2.5 text-[12.5px]"
				disabled={pending}
				onclick={logout}
			>
				<LogOutIcon class="size-3.5" aria-hidden="true" />
				{pending ? runtime.i18n.t("account:signingOut") : runtime.i18n.t("auth:logout")}
			</Button>
		{/if}
	</PopoverContent>
</Popover>
