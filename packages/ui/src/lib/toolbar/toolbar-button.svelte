<script lang="ts">
	import { Toolbar } from "bits-ui";

	import "#lib/toolbar/toolbar.scss";

	let {
		ref = $bindable(null),
		class: className,
		active = false,
		...restProps
	}: Toolbar.ButtonProps & { active?: boolean } = $props();
</script>

<Toolbar.Button
	bind:ref
	data-slot="toolbar-button"
	data-active={active || undefined}
	class={["ridu-toolbar__button", className]}
	{...restProps}
/>
