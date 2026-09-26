<script lang="ts">
	import type { SchemaCollection, SchemaManifest } from "@riducms/protocol";
	import { getAdminI18n } from "@riducms/plugin";
	import { Button } from "@riducms/ui";
	import CopyIcon from "~icons/lucide/copy";
	import CheckIcon from "~icons/lucide/check";

	import { Tabs, TabsList, TabsTrigger, TabsContent } from "@admin/components/ui/tabs";
	import APIReferenceCode from "@admin/features/api-reference/api-reference-code.svelte";
	import JsonViewer from "@admin/components/json-tree/json-viewer.svelte";
	import { CopyFeedback } from "@admin/core/clipboard/copy-feedback.svelte";
	import { collectionReferenceSchema } from "@admin/features/api-reference/api-reference-schema";
	import {
		referenceCode,
		referenceOperations,
		referenceParameters,
		referenceRequest,
		referenceResponses,
		type ReferenceLanguage,
		type ReferenceOperation,
	} from "@admin/features/api-reference/api-reference-examples";
	import "@admin/features/api-reference/api-reference.scss";

	let {
		collection,
		manifest,
		documentID,
	}: { collection: SchemaCollection; manifest: SchemaManifest; documentID?: string } = $props();

	const i18n = getAdminI18n();
	const languages: { value: ReferenceLanguage; label: string }[] = [
		{ value: "typescript", label: "TypeScript SDK" },
		{ value: "curl", label: "cURL" },
		{ value: "go", label: "Go local API" },
	];

	let operation = $state<ReferenceOperation>("list");
	let language = $state<ReferenceLanguage>("typescript");
	let response = $state("success");
	// svelte-ignore non_reactive_update
	let body: HTMLDivElement;
	const copyFeedback = new CopyFeedback(2_000);

	const schema = $derived(collectionReferenceSchema(collection, manifest, i18n));
	const context = $derived({
		collection,
		manifest,
		schema,
		documentID,
		origin: window.location.origin,
	});
	const request = $derived(referenceRequest(context, operation));
	const code = $derived(referenceCode(context, operation, language));
	const responses = $derived(referenceResponses(context, operation));
	const parameters = $derived(referenceParameters(context, operation, i18n));
	const mutation = $derived(operation === "create" || operation === "update");
	const fields = $derived(schema.fields.filter((field) => !mutation || field.writable));
	const currentCopy = $derived(
		copyFeedback.result?.source === code ? copyFeedback.result : undefined
	);
	const selectedResponse = $derived(
		response === "success"
			? responses[0]
			: (responses.find((item) => String(item.status) === response) ?? responses[0])
	);

	function selectOperation(next: ReferenceOperation) {
		operation = next;
		response = "success";
		body?.scrollTo({ top: 0 });
	}
</script>

<div class="ridu-api-reference">
	<nav class="ridu-api-reference__operations" aria-label={i18n.t("apiReference:operations")}>
		{#each referenceOperations as item (item)}
			<button
				type="button"
				aria-current={operation === item ? "page" : undefined}
				onclick={() => selectOperation(item)}
			>
				{i18n.t(`apiReference:${item}`)}
			</button>
		{/each}
	</nav>

	<div class="ridu-api-reference__body" bind:this={body}>
		<section class="ridu-api-reference__section">
			<h2>
				{i18n.t(`apiReference:${operation}`)}
				<span>({collection.slug})</span>
			</h2>
			<div class="ridu-api-reference__endpoint" dir="ltr">
				<strong>{request.method}</strong>
				<code>{request.url}</code>
			</div>
			<p>{i18n.t("apiReference:guidance")}</p>
			{#if operation === "delete"}
				<p>
					{i18n.t(
						collection.capabilities.trash ? "apiReference:trashNote" : "apiReference:deleteNote"
					)}
				</p>
			{/if}
			{#if operation === "update"}
				<p>{i18n.t("apiReference:updateNote")}</p>
			{/if}
			{#if mutation && collection.capabilities.upload}
				<p>
					{i18n.t("apiReference:uploadNote")}
				</p>
			{/if}
			{#if operation === "create" && collection.authSettings}
				<p>
					{i18n.t("apiReference:authNote", { value: collection.authSettings.passwordMinLength })}
				</p>
			{/if}
		</section>

		<section class="ridu-api-reference__section" aria-label={i18n.t("apiReference:examples")}>
			<Tabs bind:value={language}>
				<div class="ridu-api-reference__code-toolbar">
					<TabsList variant="line" aria-label={i18n.t("apiReference:languages")}>
						{#each languages as item}
							<TabsTrigger value={item.value}>
								{item.label}
							</TabsTrigger>
						{/each}
					</TabsList>
					<Button
						variant="ghost"
						size="icon-sm"
						onclick={() => copyFeedback.copy(code)}
						aria-label={i18n.t("apiReference:copy")}
					>
						{#if currentCopy?.copied}
							<CheckIcon />
						{:else}
							<CopyIcon />
						{/if}
					</Button>
				</div>
				<TabsContent value={language}>
					<APIReferenceCode {code} {language} />
				</TabsContent>
			</Tabs>
			<span class="ridu-api-reference__copy-status" role="status">
				{currentCopy
					? i18n.t(currentCopy.copied ? "general:copied" : "general:copyUnavailable")
					: ""}
			</span>
			{#if language !== "curl"}
				<p>
					{i18n.t(language === "go" ? "apiReference:goOptions" : "apiReference:credentials")}
				</p>
			{/if}
			{#if mutation && schema.omitted.length}
				<p class="ridu-api-reference__notice">
					{i18n.t("apiReference:omitted", { fields: schema.omitted.join(", ") })}
				</p>
			{/if}
		</section>

		{#if parameters.length}
			<section class="ridu-api-reference__section">
				<h3>{i18n.t("apiReference:query")}</h3>
				<dl class="ridu-api-reference__parameters">
					{#each parameters as parameter (parameter.name)}
						<div>
							<dt><code>{parameter.name}</code></dt>
							<dd>{parameter.description}</dd>
						</div>
					{/each}
				</dl>
			</section>
		{/if}

		{#if operation !== "delete"}
			<section class="ridu-api-reference__section">
				<h3>{i18n.t("apiReference:fields")}</h3>
				<p>{i18n.t("apiReference:metadata")}</p>
				{#if fields.length}
					<!-- svelte-ignore a11y_no_noninteractive_tabindex (Wide schema tables must be keyboard-scrollable.) -->
					<div
						class="ridu-api-reference__table-scroll"
						tabindex="0"
						role="region"
						aria-label={i18n.t("apiReference:fields")}
					>
						<table>
							<thead>
								<tr>
									<th>{i18n.t("apiReference:field")}</th>
									<th>{i18n.t("apiReference:type")}</th>
									<th>{i18n.t("apiReference:details")}</th>
								</tr>
							</thead>
							<tbody>
								{#each fields as field (field.path)}
									<tr>
										<td><code>{field.path}</code></td>
										<td><code>{mutation ? field.type : field.outputType}</code></td>
										<td>
											<div class="ridu-api-reference__badges">
												{#if field.requiredOnCreate && field.writable}
													<span>
														{i18n.t("apiReference:required")}
													</span>
												{/if}
												{#if !field.writable}
													<span>{i18n.t("apiReference:readOnly")}</span>
												{/if}
												{#if field.localized}
													<span>{i18n.t("apiReference:localized")}</span>
												{/if}
											</div>
											{#if field.description}
												<p>{field.description}</p>
											{/if}
											{#each field.constraints as constraint}
												<p>{constraint}</p>
											{/each}
											{#if field.jsonSchema !== undefined}
												<details>
													<summary>{i18n.t("apiReference:jsonSchema")}</summary>
													<pre dir="ltr">{JSON.stringify(field.jsonSchema, null, 2)}</pre>
												</details>
											{/if}
										</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				{:else}
					<p>{i18n.t("apiReference:noFields")}</p>
				{/if}
			</section>
		{/if}

		<section class="ridu-api-reference__section">
			<h3>{i18n.t("apiReference:responses")}</h3>
			<p>{i18n.t("apiReference:responseNote")}</p>
			<Tabs bind:value={response}>
				<TabsList variant="line" aria-label={i18n.t("apiReference:responses")}>
					{#each responses as item, index}
						<TabsTrigger value={index === 0 ? "success" : String(item.status)}>
							{item.status}
						</TabsTrigger>
					{/each}
				</TabsList>
				<TabsContent value={response}>
					<JsonViewer value={selectedResponse?.value} />
				</TabsContent>
			</Tabs>
		</section>
	</div>
</div>
