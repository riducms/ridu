<script lang="ts">
	import "#lib/field/field.scss";
	import type { Snippet } from "svelte";

	import { fieldDescriptionID, fieldErrorID } from "#lib/field/field-feedback.js";

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

<div class={["ridu-field-feedback", className]}>
	<div class="ridu-field-control">
		{#if errors.length > 0}
			<div id={errorID} class="ridu-field-error-tooltip" role="alert" aria-atomic="true">
				{#each errors as error, index (`${error}:${index}`)}
					<p>{error}</p>
				{/each}
			</div>
		{/if}
		{@render children()}
	</div>
	{#if description !== undefined}
		<p id={descriptionID} class="ridu-field-help">
			{description}
		</p>
	{/if}
</div>
