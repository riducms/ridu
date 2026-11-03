<script lang="ts">
	import { useLocation } from "@hvniel/svelte-router";

	import { adminApplicationName } from "@admin/app-meta";
	import { Banner } from "@admin/components/ui/banner";
	import { Button } from "@riducms/ui";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import AuthField from "@admin/features/auth/auth-field.svelte";
	import AuthFrame from "@admin/features/auth/auth-frame.svelte";
	import { CreateFirstUserController } from "@admin/features/auth/create-first-user-controller.svelte";
	import FieldLayout from "@admin/fields/field-layout.svelte";

	const runtime = getAdminRuntime();
	const location = useLocation();
	const controller = new CreateFirstUserController({
		runtime,
		setupToken: new URLSearchParams(location.current.search).get("setupToken") ?? undefined,
	});

	const { form, fields } = $derived(controller);
	const applicationName = $derived(
		adminApplicationName(runtime.manifest, runtime.i18n, runtime.i18n.t("general:riduApplication"))
	);

	$effect(() => () => controller.destroy());

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		const issuePath = await controller.submit();
		if (issuePath === undefined) return;

		focusIssue(issuePath);
	}

	function focusIssue(path: string) {
		const credentialInputs: Record<string, string> = {
			setupToken: "ridu-first-user-setup-token",
			password: "ridu-first-user-password",
			passwordConfirmation: "ridu-first-user-password-confirmation",
		};
		const credentialInput = credentialInputs[path];
		if (credentialInput !== undefined) {
			document.getElementById(credentialInput)?.focus();
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
							id="ridu-first-user-setup-token"
							label={runtime.i18n.t("auth:setupToken")}
							type="password"
							autocomplete="off"
							bind:value={controller.setupToken}
							description={runtime.i18n.t("auth:setupTokenDescription")}
							errors={controller.credentialIssue?.path === "setupToken"
								? [controller.credentialIssue.message]
								: []}
							oninput={() => {
								if (controller.credentialIssue?.path === "setupToken")
									controller.credentialIssue = undefined;
							}}
							showLabel={runtime.i18n.t("auth:showSetupToken")}
							hideLabel={runtime.i18n.t("auth:hideSetupToken")}
						/>
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
