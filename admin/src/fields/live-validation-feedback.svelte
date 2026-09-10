<script lang="ts">
	import type { AdminI18n, FieldLiveValidation } from "@riducms/plugin";
	import { Button } from "@riducms/ui";
	let { feedback, i18n, path }: { feedback: FieldLiveValidation; i18n: AdminI18n; path: string } =
		$props();
</script>

<div data-live-validation={feedback.status} data-live-validation-path={path} aria-live="polite">
	{#if feedback.status === "pending"}
		<p class="mt-1 text-xs text-muted-foreground">{i18n.t("fields:liveValidationPending")}</p>
	{:else if feedback.status === "failed"}
		<div class="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
			<p>{i18n.t("fields:liveValidationFailed")}</p>
			<Button variant="ghost" size="sm" onclick={feedback.retry}>
				{i18n.t("fields:liveValidationRetry")}
			</Button>
		</div>
	{:else if feedback.status === "skipped"}
		<p class="mt-1 text-xs text-muted-foreground">{i18n.t("fields:liveValidationSkipped")}</p>
	{/if}
</div>
