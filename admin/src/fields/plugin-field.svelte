<script lang="ts">
	import { onDestroy, type Component } from "svelte";
	import { SvelteMap } from "svelte/reactivity";
	import type {
		EmbeddedSchemaDraft,
		EmbeddedSchemaDraftEditorProps,
		EmbeddedSchemaFormScope,
		EmbeddedSchemaHeaderProps,
		FieldAuthoringHost,
		PluginFieldProps,
		ResolvedPluginField,
	} from "@riducms/plugin";
	import type { SchemaField, FieldType } from "@riducms/protocol";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { PluginFieldBinding } from "@admin/core/forms/plugin-field-binding";
	import { guardPluginAuthoring } from "@admin/core/forms/plugin-field-authoring";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { localizeSchemaCollection } from "@admin/core/i18n/localized-schema";
	import {
		createEmbeddedSchemaDraft,
		copyEmbeddedSchemaPayload,
		type HostedSchemaDraft,
	} from "@admin/core/forms/embedded-schema-draft.svelte";
	import EmbeddedSchemaFields from "@admin/fields/embedded-schema-fields.svelte";
	import EmbeddedSchemaDraftEditor from "@admin/fields/embedded-schema-draft-editor.svelte";
	import BlockHeader from "@admin/fields/nested/block-header.svelte";
	import ReferenceBrowser from "@admin/features/reference-browser/reference-browser-loader.svelte";
	let {
		schema,
		form,
		extension,
	}: { schema: SchemaField; form: FormController; extension: ResolvedPluginField } = $props();
	const runtime = getAdminRuntime();
	// This host is keyed by form, baseline, manifest and logical field identity.
	// svelte-ignore state_referenced_locally
	const config = extension.registration.decodeConfig(schema);
	// svelte-ignore state_referenced_locally
	const field = new PluginFieldBinding(
		form,
		() => schema,
		extension.registration,
		() => runtime.manifestRevision
	);
	// Registration validates correlation; component erasure is recovered only at this render boundary.
	// svelte-ignore state_referenced_locally
	const Editor = extension.registration.component as Component<
		Omit<PluginFieldProps<unknown, unknown, FieldType>, "config"> & { config?: unknown }
	>;
	onDestroy(field.destroy);
	const drafts = new SvelteMap<EmbeddedSchemaDraft, HostedSchemaDraft>();
	const draftAliases = new WeakMap<EmbeddedSchemaDraft, EmbeddedSchemaDraft>();
	onDestroy(() => {
		for (const draft of drafts.keys()) draft.discard();
	});
	const authoring: FieldAuthoringHost = {
		schemaForm,
		schemaHeader,
		schemaDraftEditor,
		beginSchemaDraft: (scope) => {
			const session = createEmbeddedSchemaDraft(
				form,
				() => field.schema,
				scope,
				runtime.i18n,
				() => !field.stale
			);
			const discard = session.draft.discard;
			session.draft.discard = () => {
				discard();
				drafts.delete(session.draft);
			};
			drafts.set(session.draft, session);
			return session.draft;
		},
		schemaIssues: (scope) => {
			const occurrence = embeddedIndex.get(JSON.stringify([scope.treeKey, scope.identity]));
			return occurrence === undefined ? [] : form.issuesFor(occurrence.path);
		},
		copySchemaPayload: (scope, payload) => copyEmbeddedSchemaPayload(field.schema, scope, payload),
		get collections() {
			return (
				runtime.manifest?.collections.map((collection) =>
					localizeSchemaCollection(collection, runtime.i18n)
				) ?? []
			);
		},
		get documentRevision() {
			return runtime.documentRevision;
		},
		get locale() {
			return form.contentLocale;
		},
		referenceBrowser: ReferenceBrowser,
		findDocument: (collection, id, signal) =>
			runtime.client.find(collection, id, { signal, locale: form.contentLocale }),
		async requestPlugin<Result>(path: string, body: unknown, signal?: AbortSignal) {
			const owner = extension.owner;
			if (owner === undefined) {
				throw new Error("This field renderer does not have an owning plugin");
			}
			return runtime.client.requestPlugin<Result>(owner, path, body, { signal });
		},
	};
	const embedded = $derived(form.embeddedFields(schema));
	const embeddedIndex = $derived(
		new Map(
			embedded.occurrences.map((occurrence) => [
				JSON.stringify([occurrence.tree.key, occurrence.identity]),
				occurrence,
			])
		)
	);

	const guardedAuthoring = guardPluginAuthoring(authoring, field, (draft, raw) => {
		draftAliases.set(draft, raw);
	});
	function headerDraftOpen(options: EmbeddedSchemaHeaderProps) {
		return [...drafts.values()].some(
			({ draft, treeKey }) =>
				treeKey === options.treeKey && draft.identity === options.identity && !draft.stale
		);
	}
	function headerLocked(options: EmbeddedSchemaHeaderProps) {
		return field.readOnly || options.readOnly === true || headerDraftOpen(options);
	}
	function draftOptions(options: EmbeddedSchemaDraftEditorProps) {
		field.assertActive();
		const session = drafts.get(draftAliases.get(options.draft) ?? options.draft);
		if (!session) throw new Error("This draft belongs to another field host.");
		return {
			session,
			options: {
				...options,
				draft: session.draft,
				onApply: (payload: Record<string, unknown>) => {
					field.assertEditable();
					if (options.draft.stale) throw new Error("This embedded schema draft is stale.");
					options.onApply(payload);
				},
				onCancel: () => {
					field.assertActive();
					options.onCancel();
				},
			},
		};
	}
</script>

{#snippet schemaHeader(options: EmbeddedSchemaHeaderProps)}
	{const occurrence = $derived(
		embeddedIndex.get(JSON.stringify([options.treeKey, options.identity]))
	)}
	{const locked = $derived(headerLocked(options))}
	{#if !field.stale && embedded.issues.length === 0 && occurrence !== undefined}
		{#key form.rowMountKey(occurrence.payload)}
			<BlockHeader
				block={occurrence.block}
				path={occurrence.path}
				instance={`${schema.id}-${options.treeKey}-${options.identity}`}
				{form}
				readOnly={locked}
				disabled={headerDraftOpen(options)}
				onChange={(change) => {
					field.assertEditable();
					if (headerLocked(options)) throw new Error("This embedded header is read-only.");
					options.onChange(change);
				}}
			/>
		{/key}
	{/if}
{/snippet}

{#snippet schemaForm(scope: EmbeddedSchemaFormScope)}
	<EmbeddedSchemaFields
		field={field.schema}
		{form}
		{scope}
		occurrence={embeddedIndex.get(JSON.stringify([scope.treeKey, scope.identity]))}
		issues={embedded.issues}
	/>
{/snippet}
{#snippet schemaDraftEditor(options: EmbeddedSchemaDraftEditorProps)}
	{const current = draftOptions(options)}
	<EmbeddedSchemaDraftEditor session={current.session} options={current.options} />
{/snippet}
<Editor
	{field}
	{...config === undefined ? {} : { config }}
	form={field.form}
	authoring={guardedAuthoring}
	i18n={runtime.i18n}
/>
