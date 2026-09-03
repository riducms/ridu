<script lang="ts">
	import type { Snippet } from "svelte";

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

	const messageID = $derived(`${controlID}-message`);
</script>

<div class={cn("grid gap-2", className)} data-invalid={errors.length > 0 ? "true" : undefined}>
	<div class="flex min-h-5 min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
		<label id={`${controlID}-label`} class="ridu-field-label min-w-0" for={controlID}>
			{label}
		</label>
		{#if required}<span class="ridu-field-required -ml-1.5" aria-hidden="true">*</span>{/if}
		{#if readOnly}<span class="ridu-field-status">Read only</span>{/if}
	</div>
	{@render children()}
	{#if errors.length > 0}
		<div id={messageID} class="grid gap-1">
			{#each errors as error, index (`${error}:${index}`)}
				<p class="ridu-field-error" role="alert">{error}</p>
			{/each}
		</div>
	{:else if description !== undefined}
		<p id={messageID} class="ridu-field-help">{description}</p>
	{/if}
</div>
