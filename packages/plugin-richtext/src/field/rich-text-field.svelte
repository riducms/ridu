<script lang="ts">
	import type { PluginFieldProps, PluginFieldBinding } from "@riducms/plugin";
	import type { RichTextDocument } from "@plugin-richtext/document";
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
	function fingerprint() {
		return JSON.stringify([
			field.schema.id,
			config,
			form.contentLocale,
			form.resource,
			field.rawValue,
		]);
	}
	// Local editor writes retain Lexical selection/history. Only an external parent
	// replacement (save normalization, locale, schema, group paste) starts a new editor.
	let accepted = fingerprint();
	$effect(() => {
		const incoming = fingerprint();
		if (incoming !== accepted) {
			accepted = incoming;
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
			field.set(value);
			accepted = fingerprint();
		},
	};
</script>

{#key replacement}
	<RichTextEditor field={editorField} {form} {config} {authoring} {i18n} />
{/key}
