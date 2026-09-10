<script lang="ts">
	import { useLexicalComposerContext } from "@hvniel/lexical-svelte";

	let { readOnly = false }: { readOnly?: boolean } = $props();
	const editor = useLexicalComposerContext()[0];

	// A fieldset cannot disable contenteditable; synchronize the host's live editability.
	$effect(() => editor.setEditable(!readOnly));
</script>
