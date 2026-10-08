<script lang="ts">
	import { Toolbar } from "bits-ui";

	import "#lib/toolbar/toolbar.scss";

	let { ref = $bindable(null), class: className, ...restProps }: Toolbar.RootProps = $props();
</script>

<Toolbar.Root bind:ref data-slot="toolbar" class={["ridu-toolbar", className]} {...restProps} />
