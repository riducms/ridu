<script lang="ts">
	import type { Snippet } from "svelte";

	import { fieldDescriptionID, fieldErrorID } from "@ui/field/field-feedback";
	import { cn } from "@ui/utils";

	let {
		controlID,
		description,
		errors = [],
		class: className,
		children,
	}: {
		controlID: string;
		description?: string | undefined;
		errors?: readonly string[] | undefined;
		class?: string | undefined;
		children: Snippet;
	} = $props();

	const descriptionID = $derived(fieldDescriptionID(controlID));
	const errorID = $derived(fieldErrorID(controlID));
</script>

<div class={cn("grid gap-2", className)}>
	<div class="relative grid gap-2">
		{#if errors.length > 0}
			<div id={errorID} class="ridu-field-error-tooltip" role="alert" aria-atomic="true">
				{#each errors as error, index (`${error}:${index}`)}
					<p>{error}</p>
				{/each}
			</div>
		{/if}
		{@render children()}
	</div>
	{#if description !== undefined}<p id={descriptionID} class="ridu-field-help">
			{description}
		</p>{/if}
</div>
