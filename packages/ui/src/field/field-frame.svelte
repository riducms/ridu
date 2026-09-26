<script lang="ts">
	import "@ui/field/field.scss";
	import type { Snippet } from "svelte";

	import FieldFeedback from "@ui/field/field-feedback.svelte";

	let {
		controlID,
		label,
		required = false,
		readOnly = false,
		description,
		errors = [],
		class: className,
		headingAction,
		children,
	}: {
		controlID: string;
		label: string;
		required?: boolean | undefined;
		readOnly?: boolean | undefined;
		description?: string | undefined;
		errors?: readonly string[] | undefined;
		class?: string | undefined;
		headingAction?: Snippet | undefined;
		children: Snippet;
	} = $props();
</script>

<div class={["ridu-field", className]} data-invalid={errors.length > 0 ? "true" : undefined}>
	<div class="ridu-field-heading">
		<label id={`${controlID}-label`} class="ridu-field-label" for={controlID}>
			{label}
			{#if required}
				<span class="ridu-field-required" aria-hidden="true"></span>
			{/if}
		</label>
		{#if headingAction !== undefined}
			<span class="ridu-field-heading-action">{@render headingAction()}</span>
		{/if}
		{#if readOnly}
			<span class="ridu-field-status">Read only</span>
		{/if}
	</div>
	<FieldFeedback {controlID} {description} {errors}>
		{@render children()}
	</FieldFeedback>
</div>
