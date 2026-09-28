<script lang="ts">
	import "./playground.scss";

	import type { EditorView } from "@codemirror/view";
	import type { AdminExtensionProps, AdminLoaderProps } from "@riducms/plugin";
	import { Button } from "@riducms/ui";
	import { buildSchema, type GraphQLSchema } from "graphql";
	import { untrack } from "svelte";

	import {
		configureEditor,
		createJSONEditor,
		createQueryEditor,
		replaceDocument,
		setQuerySchema,
	} from "./editor";
	import type { PlaygroundData } from "./loader";
	import { PlaygroundController } from "./playground-controller.svelte";
	import SchemaExplorer from "./schema-explorer.svelte";
	import { starterQuery } from "./starter-query";

	let { data, i18n }: AdminExtensionProps & AdminLoaderProps<PlaygroundData> = $props();

	const schema: GraphQLSchema | undefined = $derived.by(() => {
		try {
			return buildSchema(data.schema);
		} catch {
			return undefined;
		}
	});

	// The endpoint is fixed for the application's lifetime, so the controller keeps its first value.
	// svelte-ignore state_referenced_locally
	const controller = new PlaygroundController(data.endpoint, starterQuery(schema));
	$effect(() => () => controller.dispose());

	let queryView: EditorView | undefined;
	let variablesView: EditorView | undefined;
	let responseView: EditorView | undefined;

	const status = $derived.by(() => {
		const result = controller.result;
		if (result.kind === "running") return i18n.t("plugin.graphql:running");
		if (result.kind === "response")
			return i18n.t("plugin.graphql:status", {
				status: result.response.status,
				duration: result.response.durationMs,
			});
		return "";
	});
	const announcement = $derived.by(() => {
		const result = controller.result;
		if (result.kind === "response")
			return i18n.t("plugin.graphql:responseReady", { status: result.response.status });
		if (result.kind === "failed")
			return i18n.t("plugin.graphql:requestFailed", { message: result.message });
		return "";
	});

	function responseText() {
		const result = controller.result;
		return result.kind === "response" ? result.response.body : "";
	}

	// Each attachment creates its editor once; later changes flow through the effects below.
	function mountQuery(node: HTMLElement) {
		const view = createQueryEditor(node, {
			doc: controller.query,
			schema: untrack(() => schema),
			label: untrack(() => i18n.t("plugin.graphql:query")),
			onChange: (value) => {
				controller.query = value;
			},
			onRun: run,
			onShowInDocs: (type) => controller.openDocs(type),
		});
		queryView = view;
		return () => {
			view.destroy();
			queryView = undefined;
		};
	}

	function mountVariables(node: HTMLElement) {
		const view = createJSONEditor(node, {
			doc: controller.variables,
			readOnly: false,
			label: untrack(() => i18n.t("plugin.graphql:variables")),
			placeholder: untrack(() => i18n.t("plugin.graphql:variablesPlaceholder")),
			onChange: (value) => {
				controller.variables = value;
				controller.variablesEdited();
			},
			onRun: run,
		});
		variablesView = view;
		return () => {
			view.destroy();
			variablesView = undefined;
		};
	}

	function mountResponse(node: HTMLElement) {
		const view = createJSONEditor(node, {
			doc: untrack(responseText),
			readOnly: true,
			label: untrack(() => i18n.t("plugin.graphql:response")),
			onRun: run,
		});
		responseView = view;
		return () => {
			view.destroy();
			responseView = undefined;
		};
	}

	// The response and schema belong to different editors and change independently, so each external
	// synchronization boundary remains explicit rather than coupling their update schedules.
	$effect(() => {
		const text = responseText();
		if (responseView) replaceDocument(responseView, text);
	});
	$effect(() => {
		const current = schema;
		if (queryView) setQuerySchema(queryView, current);
	});
	$effect(() => {
		const query = i18n.t("plugin.graphql:query");
		const variables = i18n.t("plugin.graphql:variables");
		const variablesPlaceholder = i18n.t("plugin.graphql:variablesPlaceholder");
		const response = i18n.t("plugin.graphql:response");
		if (queryView) configureEditor(queryView, { label: query });
		if (variablesView)
			configureEditor(variablesView, { label: variables, placeholder: variablesPlaceholder });
		if (responseView) configureEditor(responseView, { label: response });
	});

	function run() {
		// The operation under the cursor runs when the document holds several.
		return controller.run(queryView?.state.selection.main.head ?? 0);
	}

	function toggleDocs() {
		if (controller.docs === undefined) controller.openDocs();
		else controller.closeDocs();
	}
</script>

<section class="ridu-graphql" aria-labelledby="ridu-graphql-title">
	<header class="ridu-graphql__header">
		<div class="ridu-graphql__intro">
			<h1 id="ridu-graphql-title" class="ridu-graphql__title">{i18n.t("plugin.graphql:title")}</h1>
			<p class="ridu-graphql__description">
				<code dir="ltr">{data.endpoint}</code>
				<span>{i18n.t("plugin.graphql:description")}</span>
			</p>
		</div>
		<div class="ridu-graphql__actions">
			{#if schema}
				<Button
					variant="outline"
					aria-expanded={controller.docs !== undefined}
					aria-controls="ridu-graphql-docs"
					onclick={toggleDocs}
				>
					{i18n.t("plugin.graphql:docs")}
				</Button>
			{/if}
			{#if controller.running}
				<Button variant="secondary" onclick={() => controller.cancel()}>
					{i18n.t("plugin.graphql:cancel")}
				</Button>
			{/if}
			<Button onclick={run} disabled={controller.running}>
				{i18n.t("plugin.graphql:run")}
				<kbd class="ridu-graphql__shortcut">{i18n.t("plugin.graphql:runShortcut")}</kbd>
			</Button>
		</div>
	</header>

	{#if schema === undefined}
		<p class="ridu-graphql__notice" role="status">{i18n.t("plugin.graphql:schemaUnavailable")}</p>
	{/if}
	{#if controller.variablesInvalid}
		<p class="ridu-graphql__notice ridu-graphql__notice--error" role="alert">
			{i18n.t("plugin.graphql:invalidVariables")}
		</p>
	{/if}

	<div
		class="ridu-graphql__workspace"
		data-docs={controller.docs === undefined ? "closed" : "open"}
	>
		<div class="ridu-graphql__request">
			<section
				class="ridu-graphql__pane ridu-graphql__pane--query"
				aria-labelledby="ridu-graphql-query"
			>
				<h2 id="ridu-graphql-query" class="ridu-graphql__pane-title">
					{i18n.t("plugin.graphql:query")}
				</h2>
				<div class="ridu-graphql__editor" dir="ltr" {@attach mountQuery}></div>
			</section>
			<section
				class="ridu-graphql__pane ridu-graphql__pane--variables"
				aria-labelledby="ridu-graphql-variables"
			>
				<h2 id="ridu-graphql-variables" class="ridu-graphql__pane-title">
					{i18n.t("plugin.graphql:variables")}
				</h2>
				<div class="ridu-graphql__editor" dir="ltr" {@attach mountVariables}></div>
			</section>
		</div>

		<section
			class="ridu-graphql__pane ridu-graphql__pane--response"
			aria-labelledby="ridu-graphql-response"
			aria-busy={controller.running}
		>
			<div class="ridu-graphql__pane-heading">
				<h2 id="ridu-graphql-response" class="ridu-graphql__pane-title">
					{i18n.t("plugin.graphql:response")}
				</h2>
				{#if status}<span class="ridu-graphql__status">{status}</span>{/if}
			</div>
			{#if controller.result.kind === "failed"}
				<p class="ridu-graphql__notice ridu-graphql__notice--error">{announcement}</p>
			{:else if controller.result.kind === "empty"}
				<p class="ridu-graphql__empty">{i18n.t("plugin.graphql:noResponse")}</p>
			{/if}
			<div
				class="ridu-graphql__editor"
				dir="ltr"
				hidden={controller.result.kind !== "response"}
				{@attach mountResponse}
			></div>
			<p class="ridu-graphql__announcement" aria-live="polite">{announcement}</p>
		</section>

		{#if schema && controller.docs !== undefined}
			<SchemaExplorer
				id="ridu-graphql-docs"
				{schema}
				trail={controller.docs}
				{i18n}
				onOpen={(type) => controller.openDocs(type)}
				onBack={() => controller.docsBack()}
				onClose={() => controller.closeDocs()}
			/>
		{/if}
	</div>
</section>
