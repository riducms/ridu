<script lang="ts">
	import type { HTMLInputAttributes } from "svelte/elements";
	import EyeIcon from "~icons/lucide/eye";
	import EyeOffIcon from "~icons/lucide/eye-off";

	import Input from "@ui/input/input.svelte";
	import Button from "@ui/button/button.svelte";
	import "@ui/input/password-input.scss";

	let {
		value = $bindable(""),
		showLabel,
		hideLabel,
		disabled,
		class: className,
		...restProps
	}: Omit<HTMLInputAttributes, "type" | "value" | "files"> & {
		value?: string;
		showLabel: string;
		hideLabel: string;
	} = $props();

	let visible = $state(false);
</script>

<div class="ridu-password-input">
	<Input
		{...restProps}
		class={["ridu-password-input__control", className]}
		type={visible ? "text" : "password"}
		bind:value
		{disabled}
	/>
	<Button
		variant="ghost"
		size="icon-sm"
		class="ridu-password-input__toggle"
		{disabled}
		aria-label={visible ? hideLabel : showLabel}
		aria-pressed={visible}
		onclick={() => (visible = !visible)}
	>
		{const Icon = $derived(visible ? EyeOffIcon : EyeIcon)}
		<Icon />
	</Button>
</div>
