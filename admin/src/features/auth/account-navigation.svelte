<script lang="ts">
	import { Link } from "@hvniel/svelte-router";

	import { buttonVariants } from "@admin/components/ui/button";
	import { adminRoutePatterns } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	const runtime = getAdminRuntime();
	let { active }: { active: "profile" | "security" } = $props();
</script>

<nav
	class="flex gap-1 border-b border-control-border"
	aria-label={runtime.i18n.t("account:accountMenu")}
>
	<Link
		to={adminRoutePatterns.account}
		class={buttonVariants({
			variant: "ghost",
			class: [
				"rounded-b-none border-b-2 px-3",
				active === "profile"
					? "border-primary text-foreground-strong"
					: "border-transparent text-foreground-muted hover:bg-control",
			],
		})}
	>
		{runtime.i18n.t("account:profileAndPreferences")}
	</Link>
	<Link
		to={adminRoutePatterns.accountSecurity}
		class={buttonVariants({
			variant: "ghost",
			class: [
				"rounded-b-none border-b-2 px-3",
				active === "security"
					? "border-primary text-foreground-strong"
					: "border-transparent text-foreground-muted hover:bg-control",
			],
		})}
	>
		{runtime.i18n.t("account:security")}
	</Link>
</nav>
