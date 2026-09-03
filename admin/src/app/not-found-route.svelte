<script lang="ts">
	import { Link } from "@hvniel/svelte-router";
	import ArrowLeftIcon from "~icons/lucide/arrow-left";

	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	const runtime = getAdminRuntime();
</script>

<section class="grid min-h-[72vh] place-items-center px-5 py-12 text-center">
	<div class="max-w-md">
		<p class="font-mono text-[10px] tracking-[0.16em] text-primary uppercase">
			{runtime.i18n.t("errors:notFoundCode")}
		</p>
		<h1 class="font-serif mt-3 text-[38px] leading-tight tracking-[-0.02em]">
			{runtime.i18n.t("errors:notFoundHeading")}
		</h1>
		<p class="mt-3 text-[13.5px] leading-6 text-foreground-muted">
			{runtime.i18n.t("errors:notFoundDescription")}
		</p>
		<Link
			class="mt-6 inline-flex items-center gap-2 text-[13px] font-medium text-primary hover:text-primary-hover"
			to="/"
		>
			<ArrowLeftIcon class="size-3.5 rtl:rotate-180" />
			{runtime.i18n.t("errors:returnDashboard")}
		</Link>
	</div>
</section>
