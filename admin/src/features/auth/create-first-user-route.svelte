<script lang="ts">
	import { useNavigate } from "@hvniel/svelte-router";
	import EyeIcon from "~icons/lucide/eye";
	import EyeOffIcon from "~icons/lucide/eye-off";
	import { tick } from "svelte";

	import { Banner } from "@admin/components/ui/banner";
	import { Button } from "@admin/components/ui/button";
	import { Input } from "@admin/components/ui/input";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import AuthBrand from "@admin/features/auth/auth-brand.svelte";
	import AuthFrame from "@admin/features/auth/auth-frame.svelte";
	import { CreateFirstUserController } from "@admin/features/auth/create-first-user-controller.svelte";
	import FieldLayout from "@admin/fields/field-layout.svelte";

	const runtime = getAdminRuntime();
	const notifications = getNotificationCenter();
	const navigate = useNavigate();
	const controller = new CreateFirstUserController({ runtime, notifications, navigate });
	const { form, fields } = $derived(controller);
	const applicationName = $derived(
		runtime.manifest?.application.name ?? runtime.i18n.t("general:riduApplication")
	);
	let passwordVisible = $state(false);
	let confirmationVisible = $state(false);

	$effect(() => () => controller.destroy());

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		const issuePath = await controller.submit();
		if (issuePath === undefined) return;
		await tick();
		focusIssue(issuePath);
	}

	function focusIssue(path: string) {
		if (path === "password") {
			document.getElementById("ridu-first-user-password")?.focus();
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

{#snippet brand()}
	<AuthBrand />
{/snippet}

<AuthFrame {brand} wide>
	<header class="grid gap-2">
		<h1 class="text-[28px] leading-tight font-semibold tracking-[-0.015em] text-foreground">
			{runtime.i18n.t("auth:welcome", { application: applicationName })}
		</h1>
		<p class="max-w-[590px] text-[13.5px] leading-5.5 text-foreground-muted">
			{runtime.i18n.t("auth:firstUserDescription")}
		</p>
	</header>

	<form class="mt-8 grid gap-7" onsubmit={handleSubmit}>
		<fieldset class="contents" disabled={controller.pending}>
			<FieldLayout {fields} {form} />

			<fieldset class="grid gap-4 rounded-[4px] border border-control-border bg-control p-4.5">
				<legend class="px-1 text-[13px] font-semibold text-foreground">
					{runtime.i18n.t("auth:credentials")}
				</legend>
				<div class="grid gap-4 sm:grid-cols-2">
					<div class="grid gap-2">
						<label class="ridu-field-label" for="ridu-first-user-password">
							{runtime.i18n.t("auth:password")}
							<span class="ridu-field-required" aria-hidden="true">*</span>
						</label>
						<span class="relative block">
							<Input
								id="ridu-first-user-password"
								class="pe-11"
								type={passwordVisible ? "text" : "password"}
								autocomplete="new-password"
								required
								aria-invalid={controller.credentialIssue !== undefined}
								aria-describedby="ridu-first-user-password-help"
								bind:value={controller.password}
								oninput={() => (controller.credentialIssue = undefined)}
							/>
							<Button
								variant="ghost"
								size="icon-sm"
								class="absolute top-1/2 end-2.5 -translate-y-1/2"
								onclick={() => (passwordVisible = !passwordVisible)}
								aria-label={passwordVisible
									? runtime.i18n.t("auth:hidePassword")
									: runtime.i18n.t("auth:showPassword")}
							>
								{#if passwordVisible}
									<EyeOffIcon class="size-3.5" />
								{:else}
									<EyeIcon class="size-3.5" />
								{/if}
							</Button>
						</span>
					</div>

					<div class="grid gap-2">
						<label class="ridu-field-label" for="ridu-first-user-password-confirmation">
							{runtime.i18n.t("auth:confirmPassword")}
							<span class="ridu-field-required" aria-hidden="true">*</span>
						</label>
						<span class="relative block">
							<Input
								id="ridu-first-user-password-confirmation"
								class="pe-11"
								type={confirmationVisible ? "text" : "password"}
								autocomplete="new-password"
								required
								aria-invalid={controller.credentialIssue !== undefined}
								aria-describedby="ridu-first-user-password-help"
								bind:value={controller.passwordConfirmation}
								oninput={() => (controller.credentialIssue = undefined)}
							/>
							<Button
								variant="ghost"
								size="icon-sm"
								class="absolute top-1/2 end-2.5 -translate-y-1/2"
								onclick={() => (confirmationVisible = !confirmationVisible)}
								aria-label={confirmationVisible
									? runtime.i18n.t("auth:hidePasswordConfirmation")
									: runtime.i18n.t("auth:showPasswordConfirmation")}
							>
								{#if confirmationVisible}
									<EyeOffIcon class="size-3.5" />
								{:else}
									<EyeIcon class="size-3.5" />
								{/if}
							</Button>
						</span>
					</div>
				</div>
				<p
					id="ridu-first-user-password-help"
					class={controller.credentialIssue === undefined ? "ridu-field-help" : "ridu-field-error"}
					role={controller.credentialIssue === undefined ? undefined : "alert"}
				>
					{controller.credentialIssue ?? controller.passwordHelp}
				</p>
			</fieldset>
		</fieldset>

		{#if controller.error !== undefined}
			<Banner tone="destructive">{controller.error}</Banner>
		{/if}

		<Button class="h-10 w-full" type="submit" size="lg" disabled={controller.pending}>
			{controller.pending
				? runtime.i18n.t("auth:creatingAccount")
				: runtime.i18n.t("auth:createAccount")}
		</Button>
	</form>
</AuthFrame>
