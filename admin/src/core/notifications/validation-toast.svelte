<script lang="ts">
	let { labels }: { labels: readonly string[] } = $props();
</script>

{#if labels.length === 1}
	<span>
		The following field is invalid: <strong>{labels[0]}</strong>
	</span>
{:else}
	<div>
		<p>The following fields are invalid ({labels.length}):</p>
		<ul class="mt-1 list-[circle] space-y-0.5 ps-5">
			{#each labels as label (label)}
				<li class="ps-0.5">{label}</li>
			{/each}
		</ul>
	</div>
{/if}
