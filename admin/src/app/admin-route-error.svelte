<script lang="ts">
	import { Link, useRouteError } from "@hvniel/svelte-router";
	import { getAdminI18n } from "@riducms/plugin";
	import { Button, buttonVariants } from "@riducms/ui";

	const i18n = getAdminI18n();
	const error = useRouteError();

	$effect(() => {
		console.error("Ridu admin route failed", error.current);
	});
</script>

<main class="grid min-h-screen place-items-center p-6">
	<section class="grid max-w-lg justify-items-start gap-5" role="alert">
		<h1 class="text-2xl font-semibold">{i18n.t("errors:unknown")}</h1>
		<div class="flex flex-wrap gap-3">
			<Button onclick={() => window.location.reload()}>{i18n.t("general:reloadPage")}</Button>
			<Link to="/" replace class={buttonVariants({ variant: "outline" })}>
				{i18n.t("errors:returnDashboard")}
			</Link>
		</div>
	</section>
</main>
