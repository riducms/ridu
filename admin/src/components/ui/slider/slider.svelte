<script lang="ts">
	import type { Component } from "svelte";

	type SliderProps = {
		id?: string;
		value: number;
		min?: number;
		max?: number;
		step?: number;
		disabled?: boolean;
		label: string;
		describedBy?: string;
		class?: string;
		onValueChange: (value: number) => void;
	};

	let props: SliderProps = $props();
	let Control = $state<Component<SliderProps> | undefined>(undefined);

	void import("@admin/components/ui/slider/slider-content.svelte").then(
		({ default: component }) => (Control = component)
	);
</script>

{#if Control !== undefined}
	<Control {...props} />
{:else}
	<div
		class={props.class ??
			"relative flex h-5 w-full animate-pulse items-center rounded-full bg-control-disabled"}
		role="status"
		aria-label={props.label}
		aria-busy="true"
	></div>
{/if}
