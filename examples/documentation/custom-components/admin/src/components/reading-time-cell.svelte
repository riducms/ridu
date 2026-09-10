<script lang="ts">
	import type { AdminListCellProps } from '@riducms/plugin';

	let { value }: AdminListCellProps = $props();
</script>

<span>
	{typeof value === 'number' ? `${value} min read` : 'Not set'}
</span>
