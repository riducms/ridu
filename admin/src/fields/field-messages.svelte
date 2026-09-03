<script lang="ts">
	import type { ValidationIssue } from "@riducms/protocol";
	import CircleAlertIcon from "~icons/lucide/circle-alert";

	let {
		id,
		issues = [],
		description,
		class: className,
	}: {
		id: string;
		issues?: readonly ValidationIssue[];
		description?: string;
		class?: string;
	} = $props();
</script>

{#if issues.length > 0}
	<div {id} class={["flex items-start gap-1.5", className]} role="alert" aria-atomic="true">
		<CircleAlertIcon class="mt-0.5 size-3.5 shrink-0 text-destructive" aria-hidden="true" />
		<ul class="grid gap-0.5">
			{#each issues as issue (`${issue.path}:${issue.code}:${issue.message}`)}
				<li class="ridu-field-error">{issue.message}</li>
			{/each}
		</ul>
	</div>
{:else if description !== undefined}
	<p {id} class={["ridu-field-help", className]}>{description}</p>
{/if}
