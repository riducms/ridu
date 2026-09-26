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
		<ul class="ridu-validation-toast__fields">
			{#each labels as label (label)}
				<li>{label}</li>
			{/each}
		</ul>
	</div>
{/if}
