<script lang="ts">
	import type { HTMLInputAttributes } from "svelte/elements";
	import { FieldFrame, Input, PasswordInput, fieldControlARIA } from "@riducms/ui";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	let {
		id,
		label,
		type = "email",
		value = $bindable(""),
		required = true,
		description,
		errors = [],
		showLabel,
		hideLabel,
		oninput,
		...inputProps
	}: Pick<HTMLInputAttributes, "autocomplete" | "disabled" | "minlength" | "oninput"> & {
		id: string;
		label: string;
		type?: "email" | "password";
		value?: string;
		required?: boolean;
		description?: string;
		errors?: readonly string[];
		showLabel?: string;
		hideLabel?: string;
	} = $props();

	const { i18n } = getAdminRuntime();

	let validationMessage = $state<string>();
	let validationExposed = false;

	const messages = $derived(validationMessage === undefined ? errors : [validationMessage]);

	function invalid(event: Event & { currentTarget: HTMLInputElement }) {
		// Keep native constraint validation, but render its result through Ridu's field feedback.
		event.preventDefault();
		const input = event.currentTarget;
		validationExposed = true;
		validationMessage = constraintMessage(input);
		input.form?.querySelector<HTMLElement>("input:invalid")?.focus();
	}

	function constraintMessage(input: HTMLInputElement) {
		if (input.validity.valid) return undefined;

		if (input.validity.tooShort) {
			return i18n.t("auth:passwordTooShort", { minimum: i18n.formatNumber(input.minLength) });
		}

		return input.validity.valueMissing
			? i18n.t("errors:required")
			: input.validity.typeMismatch
				? i18n.t("auth:invalidEmail")
				: input.validationMessage;
	}

	function input(event: Event & { currentTarget: HTMLInputElement }) {
		if (validationExposed) validationMessage = constraintMessage(event.currentTarget);

		oninput?.(event);
	}
</script>

<FieldFrame controlID={id} {label} required={required === true} {description} errors={messages}>
	{#if type === "password"}
		<PasswordInput
			{...inputProps}
			{...fieldControlARIA(id, description !== undefined, messages.length > 0)}
			{id}
			{required}
			bind:value
			showLabel={showLabel ?? i18n.t("auth:showPassword")}
			hideLabel={hideLabel ?? i18n.t("auth:hidePassword")}
			oninvalid={invalid}
			oninput={input}
		/>
	{:else}
		<Input
			{...inputProps}
			{...fieldControlARIA(id, description !== undefined, messages.length > 0)}
			{id}
			{type}
			{required}
			bind:value
			oninvalid={invalid}
			oninput={input}
		/>
	{/if}
</FieldFrame>
