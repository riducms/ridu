<script lang="ts">
	import { setAdminI18n } from "@riducms/plugin";
	import type { RichTextDocument } from "@riducms/sdk/richtext";
	import { TooltipProvider } from "@riducms/ui";
	import type { ComponentProps } from "svelte";

	import "#lib/editor/rich-text-app-editor.scss";
	import RichTextEditor from "#lib/editor/rich-text-editor.svelte";
	import { createRichTextI18n, type RichTextMessages } from "#lib/editor/rich-text-editor-i18n.js";

	let {
		value = $bindable(),
		lang,
		dir,
		messages,
		onchange,
		...props
	}: Omit<ComponentProps<typeof RichTextEditor>, "lang" | "dir" | "extensions" | "onchange"> & {
		/**
		 * The language of the editor's content, such as "fr". The editor's own text uses the
		 * language it mounted with: its catalogue's translation, or English.
		 */
		lang?: string | undefined;
		/** The direction of the editor's content. Its menus use the direction it mounted with. */
		dir?: "ltr" | "rtl" | undefined;
		/** Replaces some of the editor's text, read when it mounts; the rest is its own catalogue's. */
		messages?: RichTextMessages | undefined;
		/**
		 * A changed document, already checked to be one the rich-text field accepts. It holds no
		 * blocks, which only the admin's editor opens.
		 */
		onchange?: (value: RichTextDocument) => void;
	} = $props();

	// An app has no admin translations, so the editor and its menus read its own catalogue from
	// context. Like the document, the text is read once.
	// svelte-ignore state_referenced_locally
	setAdminI18n(createRichTextI18n({ lang, dir, messages }));
</script>

<!-- The toolbars' tooltips need a provider, which the admin has and an app doesn't. -->
<TooltipProvider>
	<!-- An app may pass its document one-way and follow onchange: the editor writes each edit
	through this binding either way. With no extensions, its documents hold no blocks. -->
	<!-- svelte-ignore ownership_invalid_binding -->
	<RichTextEditor
		bind:value
		{...props}
		{lang}
		{dir}
		onchange={(next) => onchange?.(next as RichTextDocument)}
	/>
</TooltipProvider>
