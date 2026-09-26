<script lang="ts">
	import { tick } from "svelte";

	import { adminApplicationName } from "@admin/app-meta";
	import { Banner } from "@admin/components/ui/banner";
	import { Button } from "@riducms/ui";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import AuthField from "@admin/features/auth/auth-field.svelte";
	import AuthFrame from "@admin/features/auth/auth-frame.svelte";
	import { CreateFirstUserController } from "@admin/features/auth/create-first-user-controller.svelte";
	import FieldLayout from "@admin/fields/field-layout.svelte";

	const runtime = getAdminRuntime();
	const controller = new CreateFirstUserController({ runtime });

	const { form, fields } = $derived(controller);
	const applicationName = $derived(
		adminApplicationName(runtime.manifest, runtime.i18n, runtime.i18n.t("general:riduApplication"))
	);

	$effect(() => () => controller.destroy());

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		const issuePath = await controller.submit();
		if (issuePath === undefined) return;

		await tick();
		focusIssue(issuePath);
	}

	function focusIssue(path: string) {
		if (path === "password" || path === "passwordConfirmation") {
			document
				.getElementById(
					path === "password" ? "ridu-first-user-password" : "ridu-first-user-password-confirmation"
				)
				?.focus();
			return;
		}

		const segments = path.split(".");

		while (segments.length > 0) {
			const candidate = segments.join(".");
			const container = document.querySelector<HTMLElement>(
				`[data-field-path="${CSS.escape(candidate)}"]`
			);
			if (container !== null) {
				container.scrollIntoView({ behavior: "smooth", block: "center" });
				container
					.querySelector<HTMLElement>("input, textarea, button, [tabindex]:not([tabindex='-1'])")
					?.focus({ preventScroll: true });
				return;
			}

			segments.pop();
		}
	}
</script>

<AuthFrame>
	<div class="ridu-auth__stack">
		<header class="ridu-auth__header">
			<h1 class="ridu-auth__heading">
				{runtime.i18n.t("auth:welcome", { application: applicationName })}
			</h1>
			<p class="ridu-auth__description">
				{runtime.i18n.t("auth:firstUserDescription")}
			</p>
		</header>

		<form class="ridu-auth__form" onsubmit={handleSubmit} novalidate>
			<fieldset
				class="ridu-auth__fieldset"
				disabled={controller.pending}
				aria-busy={controller.pending}
			>
				<FieldLayout {fields} {form} />

				<fieldset class="ridu-auth__credentials">
					<legend class="ridu-auth__sr-only">
						{runtime.i18n.t("auth:credentials")}
					</legend>
					<div class="ridu-auth__fields">
						<AuthField
							id="ridu-first-user-password"
							label={runtime.i18n.t("auth:password")}
							type="password"
							autocomplete="new-password"
							bind:value={controller.password}
							description={controller.passwordHelp}
							errors={controller.credentialIssue?.path === "password"
								? [controller.credentialIssue.message]
								: []}
							oninput={() => (controller.credentialIssue = undefined)}
						/>
						<AuthField
							id="ridu-first-user-password-confirmation"
							label={runtime.i18n.t("auth:confirmPassword")}
							type="password"
							autocomplete="new-password"
							bind:value={controller.passwordConfirmation}
							errors={controller.credentialIssue?.path === "passwordConfirmation"
								? [controller.credentialIssue.message]
								: []}
							oninput={() => {
								if (controller.credentialIssue?.path === "passwordConfirmation")
									controller.credentialIssue = undefined;
							}}
							showLabel={runtime.i18n.t("auth:showPasswordConfirmation")}
							hideLabel={runtime.i18n.t("auth:hidePasswordConfirmation")}
						/>
					</div>
				</fieldset>
			</fieldset>

			{#if controller.error !== undefined}
				<Banner tone="destructive">{controller.error}</Banner>
			{/if}

			<Button
				class="ridu-auth__submit"
				type="submit"
				size="lg"
				disabled={controller.pending}
				aria-busy={controller.pending}
			>
				{controller.pending
					? runtime.i18n.t("auth:creatingAccount")
					: runtime.i18n.t("auth:createAccount")}
			</Button>
		</form>
	</div>
</AuthFrame>
