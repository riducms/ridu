<script lang="ts">
	import type { AdminListCellRendererProps } from '@riducms/plugin';

	let { value }: AdminListCellRendererProps = $props();
</script>

<span>
	{typeof value === 'number' ? `${value} min read` : 'Not set'}
</span>
