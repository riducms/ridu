<script lang="ts">
	import { untrack } from 'svelte';
	import type { PluginFieldProps } from '@riducms/plugin';
	import { decodeColor, type Color } from './value';
	let { field }: PluginFieldProps<Color> = $props();

	// Unfinished text and the stored value it was typed over.
	let draft = $state<{ text: string; over: typeof field.value }>();

	// Another editor replaced the stored value: drop the draft and its report.
	$effect(() => {
		const value = field.value;
		untrack(() => {
			if (draft === undefined || draft.over === value) return;
			draft = undefined;
			field.reportPendingEdit([]);
		});
	});

	function edit(text: string) {
		try {
			const color = text === '' ? null : decodeColor(text);
			draft = undefined;
			field.reportPendingEdit([]);
			field.set(color);
		} catch {
			draft = { text, over: field.value };
			field.reportPendingEdit([
				{
					code: 'unfinished_color',
					path: field.schema.path,
					message: 'Enter six hexadecimal digits, such as #1a2b3c.'
				}
			]);
		}
	}
</script>

<input
	aria-label={field.schema.admin.label}
	value={draft?.text ?? field.value ?? ''}
	disabled={field.readOnly}
	oninput={(event) => edit(event.currentTarget.value)}
/>
