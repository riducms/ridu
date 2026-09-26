<script lang="ts">
	import { Link } from "@hvniel/svelte-router";
	import "@admin/features/account/account-menu.scss";
	import SettingsIcon from "~icons/lucide/settings";
	import UserRoundIcon from "~icons/lucide/user-round";

	import { buttonVariants } from "@riducms/ui";
	import { Popover, PopoverContent, PopoverTrigger } from "@admin/components/ui/popover";
	import { adminRoutePatterns } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	const runtime = getAdminRuntime();
	const extensions = runtime.config.extensions;

	let open = $state(false);

	const accountAvatar = extensions.branding.find(
		(component) => component.surface === "accountAvatar"
	);
	const settingsComponents = extensions.shellSlots.filter(
		(component) => component.position === "settingsMenu"
	);

	function accountDetails() {
		const user = runtime.session?.user;
		const candidate = user?.name;
		let displayName =
			typeof candidate === "string" && candidate.trim().length > 0 ? candidate : undefined;

		const identityField = runtime.authCollection?.authSettings?.identityField;
		const identity = identityField === undefined ? undefined : user?.[identityField];
		if (displayName === undefined && typeof identity === "string" && identity.trim().length > 0)
			displayName = identity;
		displayName ??= runtime.i18n.t("account:signedInUser");

		return {
			user,
			displayName,
			identity: typeof identity === "string" && identity !== displayName ? identity : undefined,
		};
	}

	const account = $derived(accountDetails());
</script>

<Popover bind:open>
	<PopoverTrigger
		class="ridu-account-menu__trigger"
		aria-label={runtime.i18n.t("account:accountMenuLabel", { name: account.displayName })}
	>
		{#if accountAvatar !== undefined && runtime.manifest !== undefined}
			<accountAvatar.component
				manifest={runtime.manifest}
				user={account.user}
				surface="accountAvatar"
				i18n={runtime.i18n}
			/>
		{:else}
			<svg
				class="ridu-account-menu__avatar"
				width="25"
				height="25"
				viewBox="0 0 25 25"
				aria-hidden="true"
			>
				<circle class="ridu-account-menu__avatar-background" cx="12.5" cy="12.5" r="11.5" />
				<circle cx="12.5" cy="10.73" r="3.98" />
				<path
					d="M12.5,24a11.44,11.44,0,0,0,7.66-2.94c-.5-2.71-3.73-4.8-7.66-4.8s-7.16,2.09-7.66,4.8A11.44,11.44,0,0,0,12.5,24Z"
				/>
			</svg>
		{/if}
	</PopoverTrigger>
	<PopoverContent side="bottom" align="end" sideOffset={8} class="w-[212px] gap-1 p-2">
		<div class="border-b border-control-border px-2.5 py-2 pb-3">
			<p class="truncate text-[13px] font-medium text-foreground">{account.displayName}</p>
			{#if account.identity !== undefined}
				<p class="mt-0.5 truncate text-[10.5px] text-foreground-faint">{account.identity}</p>
			{/if}
		</div>

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
					user={account.user}
					position="settingsMenu"
					i18n={runtime.i18n}
				/>
			{/each}
		{/if}
	</PopoverContent>
</Popover>
