<script lang="ts">
	import type { SchemaDatePickerAppearance } from "@riducms/protocol";
	import type { Component } from "svelte";

	type ControlSize = "field" | "compact" | "toolbar";
	type DateValueControlProps = {
		id: string;
		name?: string;
		appearance?: SchemaDatePickerAppearance;
		value?: string;
		disabled?: boolean;
		readonly?: boolean;
		required?: boolean;
		invalid?: boolean;
		label?: string;
		describedBy?: string;
		class?: string;
		size?: ControlSize;
		onValueChange: (value: string) => void;
	};

	let props: DateValueControlProps = $props();
	let Control = $state<Component<DateValueControlProps> | undefined>(undefined);

	void import("@admin/components/ui/date-value-control/date-value-control.svelte").then(
		({ default: component }) => (Control = component)
	);
</script>

{#if Control !== undefined}
	<Control {...props} />
{:else}
	<div id={props.id} class={props.class} role="status" aria-label={props.label} aria-busy="true">
		Loading date control…
	</div>
{/if}
