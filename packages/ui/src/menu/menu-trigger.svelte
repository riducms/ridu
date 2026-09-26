<script lang="ts">
	import { DropdownMenu } from "bits-ui";
	import "@ui/menu/menu.scss";
	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: DropdownMenu.TriggerProps = $props();
</script>

<DropdownMenu.Trigger bind:ref class={["ridu-menu-trigger", className]} {...restProps} />
