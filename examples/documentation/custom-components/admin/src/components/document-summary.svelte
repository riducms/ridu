<script lang="ts">
	import type { AdminDocumentExtensionProps } from '@riducms/plugin';

	// This is the saved document; unsaved form edits are not included.
	let { document }: AdminDocumentExtensionProps = $props();
</script>

<section aria-labelledby="document-summary-heading">
	<h2 id="document-summary-heading">Article summary</h2>
	<dl>
		<dt>Title</dt>
		<dd>
			{typeof document.title === 'string'
				? document.title
				: 'Untitled'}
		</dd>
		<dt>Reading time</dt>
		<dd>
			{typeof document.readingMinutes === 'number'
				? `${document.readingMinutes} minutes`
				: 'Not set'}
		</dd>
	</dl>
</section>
