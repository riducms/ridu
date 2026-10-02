<script lang="ts">
	import { untrack } from "svelte";
	import type { PluginFieldProps, PluginFieldBinding } from "@riducms/plugin";
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
	const editorField: PluginFieldBinding<RichTextDocument<unknown>, "plugin"> = {
		get schema() {
			return field.schema;
		},
		get value() {
			return field.value;
		},
		get rawValue() {
			return field.rawValue;
		},
		get issues() {
			return field.issues;
		},
		get liveValidation() {
			return field.liveValidation;
		},
		get readOnly() {
			return field.readOnly;
		},
		get stale() {
			return field.stale;
		},
		set: (value) => {
			// A parent editor can flush decorators inside this write's callback. Mark
			// our own serialization before that happens so nested editors stay mounted.
			accepted = fingerprint(value);
			try {
				field.set(value);
			} finally {
				accepted = fingerprint();
			}
		},
		reportPendingEdit: (issues) => field.reportPendingEdit(issues),
	};

	function acceptEmbeddedChange() {
		// Inline ordinary fields already wrote to the parent form. Decorator reconciliation
		// flushes Svelte before OnChange serializes Lexical, so acknowledge that local write first.
		accepted = fingerprint();
	}
</script>

{#key replacement}
	<RichTextEditor field={editorField} {form} {config} {authoring} {i18n} {acceptEmbeddedChange} />
{/key}
