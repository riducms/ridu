<script lang="ts">
	import { DropdownMenu } from "bits-ui";
	import "#lib/menu/menu.scss";
	let { ref = $bindable(null), class: className, ...restProps }: DropdownMenu.ItemProps = $props();
</script>

<DropdownMenu.Item bind:ref class={["ridu-menu-item", className]} {...restProps} />
