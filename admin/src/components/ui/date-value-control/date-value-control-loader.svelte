<script lang="ts">
	import type { SchemaDateFormat } from "@riducms/protocol";
	import { fieldControlARIA } from "@riducms/ui";
	import type { Component } from "svelte";

	type ControlSize = "field" | "compact" | "toolbar";
	type DateValueControlProps = {
		id: string;
		name?: string;
		appearance?: SchemaDateFormat;
		value?: string;
		disabled?: boolean;
		readonly?: boolean;
		required?: boolean;
		invalid?: boolean;
		hasDescription?: boolean;
		label?: string;
		class?: string;
		size?: ControlSize;
		onValueChange: (value: string) => void;
	};

	let props: DateValueControlProps = $props();
	let Control = $state<Component<DateValueControlProps> | undefined>(undefined);
	const controlARIA = $derived(
		fieldControlARIA(props.id, props.hasDescription ?? false, props.invalid ?? false)
	);

	void import("@admin/components/ui/date-value-control/date-value-control.svelte").then(
		({ default: component }) => (Control = component)
	);
</script>

{#if Control !== undefined}
	<Control {...props} />
{:else}
	<div
		id={props.id}
		class={props.class}
		role="status"
		aria-label={props.label}
		aria-busy="true"
		{...controlARIA}
	>
		Loading date control…
	</div>
{/if}
