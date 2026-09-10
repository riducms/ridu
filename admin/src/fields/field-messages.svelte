<script lang="ts">
	import type { ValidationIssue } from "@riducms/protocol";
	import { FieldFeedback } from "@riducms/ui";
	import type { Snippet } from "svelte";

	let {
		controlID,
		issues = [],
		description,
		class: className,
		children,
	}: {
		controlID: string;
		issues?: readonly ValidationIssue[] | undefined;
		description?: string | undefined;
		class?: string | undefined;
		children: Snippet;
	} = $props();
	const errors = $derived(issues.map((issue) => issue.message));
</script>

<FieldFeedback {controlID} {description} {errors} class={className}>
	{@render children()}
</FieldFeedback>
