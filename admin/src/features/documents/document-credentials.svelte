<script lang="ts">
	import { Input } from "@riducms/ui";
	import "@admin/features/documents/document-credentials.scss";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import type { AuthCreateCredentials } from "@admin/features/documents/document-credentials";

	const runtime = getAdminRuntime();

	let {
		credentials = $bindable(),
		issue = $bindable(),
		minimumLength,
	}: {
		credentials: AuthCreateCredentials;
		issue?: string;
		minimumLength?: number;
	} = $props();
</script>

<fieldset class="ridu-document-credentials">
	<legend class="ridu-document-credentials__heading">
		{runtime.i18n.t("documents:credentials")}
	</legend>
	<p class="ridu-field-help">
		{runtime.i18n.t("documents:credentialsDescription")}
	</p>
	<div class="ridu-document-credentials__fields">
		<label class="ridu-document-credentials__field" for="ridu-new-user-password">
			<span class="ridu-field-label">
				{runtime.i18n.t("documents:password")}
				<span class="ridu-field-required" aria-hidden="true">*</span>
			</span>
			<Input
				id="ridu-new-user-password"
				type="password"
				autocomplete="new-password"
				minlength={minimumLength}
				required
				aria-invalid={issue !== undefined}
				aria-describedby="ridu-new-user-password-help"
				bind:value={credentials.password}
				oninput={() => (issue = undefined)}
			/>
		</label>
		<label class="ridu-document-credentials__field" for="ridu-new-user-password-confirmation">
			<span class="ridu-field-label">
				{runtime.i18n.t("documents:confirmPassword")}
				<span class="ridu-field-required" aria-hidden="true">*</span>
			</span>
			<Input
				id="ridu-new-user-password-confirmation"
				type="password"
				autocomplete="new-password"
				required
				aria-invalid={issue !== undefined}
				aria-describedby="ridu-new-user-password-help"
				bind:value={credentials.passwordConfirmation}
				oninput={() => (issue = undefined)}
			/>
		</label>
	</div>
	<p
		id="ridu-new-user-password-help"
		class={issue === undefined ? "ridu-field-help" : "ridu-field-error"}
		role={issue === undefined ? undefined : "alert"}
	>
		{issue ??
			runtime.i18n.t("documents:passwordMinimum", {
				minimum: runtime.i18n.formatNumber(minimumLength ?? 8),
			})}
	</p>
</fieldset>
