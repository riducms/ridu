<script lang="ts">
	import type { Snippet } from "svelte";

	import FieldFeedback from "@ui/field/field-feedback.svelte";
	import { cn } from "@ui/utils";

	let {
		controlID,
		label,
		required = false,
		readOnly = false,
		description,
		errors = [],
		class: className,
		children,
	}: {
		controlID: string;
		label: string;
		required?: boolean | undefined;
		readOnly?: boolean | undefined;
		description?: string | undefined;
		errors?: readonly string[] | undefined;
		class?: string | undefined;
		children: Snippet;
	} = $props();
</script>

<div class={cn("grid gap-2", className)} data-invalid={errors.length > 0 ? "true" : undefined}>
	<div class="flex min-h-5 min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
		<label id={`${controlID}-label`} class="ridu-field-label min-w-0" for={controlID}>
			{label}
		</label>
		{#if required}<span class="ridu-field-required -ml-1.5" aria-hidden="true">*</span>{/if}
		{#if readOnly}<span class="ridu-field-status">Read only</span>{/if}
	</div>
	<FieldFeedback {controlID} {description} {errors}>
		{@render children()}
	</FieldFeedback>
</div>
