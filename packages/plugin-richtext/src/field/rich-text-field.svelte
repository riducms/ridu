<script lang="ts">
	import { untrack } from "svelte";
	import type { PluginFieldProps } from "@riducms/plugin";
	import type { RichTextDocument } from "@riducms/sdk/richtext";
	import type { RichTextConfig } from "@plugin-richtext/field/rich-text-config";
	import RichTextEditor from "@plugin-richtext/field/rich-text-editor.svelte";

	let {
		field,
		form,
		config,
		authoring,
		i18n,
	}: PluginFieldProps<RichTextDocument<unknown>, RichTextConfig> = $props();
	let replacement = $state(0);
	function fingerprint(value = field.rawValue) {
		return JSON.stringify([field.schema.id, config, form.contentLocale, form.resource, value]);
	}
	// Local editor writes retain Lexical selection/history. Only an external parent
	// replacement (save normalization, locale, schema, group paste) starts a new editor.
	let accepted = fingerprint();
	$effect(() => {
		const incoming = fingerprint();
		if (incoming !== accepted) {
			accepted = incoming;
			// The replaced editor's unsaved input goes with it.
			untrack(() => field.reportPendingEdit([]));
			replacement++;
		}
	});

	function commitEditorValue(value: RichTextDocument<unknown>) {
		// A parent editor can flush decorators inside this write's callback. Mark
		// our own serialization before that happens so nested editors stay mounted.
		accepted = fingerprint(value);
		try {
			field.set(value);
		} finally {
			accepted = fingerprint();
		}
	}

	function acceptEmbeddedChange() {
		// Inline ordinary fields already wrote to the parent form. Decorator reconciliation
		// flushes Svelte before OnChange serializes Lexical, so acknowledge that local write first.
		accepted = fingerprint();
	}
</script>

{#key replacement}
	<RichTextEditor {field} {config} {authoring} {i18n} {commitEditorValue} {acceptEmbeddedChange} />
{/key}
