<script lang="ts">
	import type { PluginFieldProps } from "@riducms/plugin/authoring/v1";
	let { field, config }: PluginFieldProps<{ items: string[] }, { limit: number }> = $props();
</script>

<button type="button" disabled={field.readOnly} onclick={() => field.set({ items: ["one"] })}>
	{field.value?.items.length ?? 0} / {config.limit}
</button>
