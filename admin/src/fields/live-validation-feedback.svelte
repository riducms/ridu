<script lang="ts">
	import type { AdminI18n, FieldLiveValidation } from "@riducms/plugin";
	import { Button } from "@riducms/ui";
	import "@admin/fields/field-layout.scss";
	let { feedback, i18n, path }: { feedback: FieldLiveValidation; i18n: AdminI18n; path: string } =
		$props();
</script>

<div data-live-validation={feedback.status} data-live-validation-path={path} aria-live="polite">
	{#if feedback.status === "pending"}
		<p class="ridu-field-live-validation">{i18n.t("fields:liveValidationPending")}</p>
	{:else if feedback.status === "failed"}
		<div class="ridu-field-live-validation ridu-field-live-validation--failed">
			<p>{i18n.t("fields:liveValidationFailed")}</p>
			<Button variant="ghost" size="sm" onclick={feedback.retry}>
				{i18n.t("fields:liveValidationRetry")}
			</Button>
		</div>
	{:else if feedback.status === "skipped"}
		<p class="ridu-field-live-validation">{i18n.t("fields:liveValidationSkipped")}</p>
	{/if}
</div>
