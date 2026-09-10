<script lang="ts">
	import type { PluginFieldProps, FieldDocument } from "@riducms/plugin";
	import { Button, FieldFrame, fieldControlARIA } from "@riducms/ui";
	import ImageIcon from "~icons/lucide/image";
	import SearchIcon from "~icons/lucide/search";
	import XIcon from "~icons/lucide/x";

	import { GenerationController } from "@plugin-seo/generation-controller.svelte";

	let {
		field: binding,
		form,
		config,
		i18n,
		authoring,
	}: PluginFieldProps<string, { generate: boolean }, "upload"> = $props();
	const field = $derived(binding.schema);
	const editingBlocked = $derived(binding.readOnly);
	const generationEnabled = $derived(config.generate);
	const value = $derived(String(binding.value ?? ""));
	const issues = $derived(binding.issues);
	const controlARIA = $derived(
		fieldControlARIA(field.id, field.admin.description !== undefined, issues.length > 0)
	);
	const target = $derived(
		authoring?.collections.find(
			(collection) =>
				collection.slug === field.upload?.collectionSlug ||
				collection.id === field.upload?.collectionId
		)
	);
	const ReferenceBrowser = $derived(authoring?.referenceBrowser);
	let browserOpen = $state(false);
	let browserMode = $state<"inspect" | "select">("select");
	let document = $state.raw<FieldDocument>();
	let loadFailed = $state(false);
	const generation = new GenerationController();

	$effect(() => () => generation.cancel());
	$effect(() => {
		const id = value;
		if (id === "" || target === undefined || authoring === undefined) {
			document = undefined;
			loadFailed = false;
			return;
		}
		const request = new AbortController();
		loadFailed = false;
		authoring
			.findDocument(target.slug, id, request.signal)
			.then((result) => {
				if (!request.signal.aborted) document = result;
			})
			.catch(() => {
				if (!request.signal.aborted) {
					document = undefined;
					loadFailed = true;
				}
			});
		return () => request.abort();
	});

	async function generate() {
		const result = await generation.run(authoring, form, "generate-image");
		if (result !== undefined) binding.set(result);
	}

	function commit(ids: string[]) {
		binding.set(ids[0] ?? null);
		browserOpen = false;
	}

	function openBrowser(mode: "inspect" | "select") {
		browserMode = mode;
		browserOpen = true;
	}

	function documentLabel() {
		for (const key of ["filename", "title", "name", "alt"] as const) {
			const candidate = document?.[key];
			if (typeof candidate === "string" && candidate.length > 0) return candidate;
		}
		return value;
	}
</script>

<div data-field-path={field.path}>
	<FieldFrame
		controlID={field.id}
		label={field.admin.label}
		required={field.required}
		readOnly={field.admin.readOnly}
		description={field.admin.description}
		errors={issues.map((issue) => issue.message)}
	>
		{#if generationEnabled}<div
				class="-mt-1 flex flex-wrap items-center gap-1 text-[11.5px] text-foreground-muted"
			>
				<span>{i18n.t("plugin.seo:imageAutoGenerationTip")}</span>
				<Button
					variant="link"
					size="xs"
					class="h-auto px-0"
					disabled={editingBlocked || generation.status === "pending"}
					aria-busy={generation.status === "pending"}
					onclick={generate}
				>
					{i18n.t("plugin.seo:autoGenerate")}
				</Button>
			</div>{/if}
		{#if value === ""}
			<Button
				id={field.id}
				aria-label={i18n.t("plugin.seo:selectImage")}
				{...controlARIA}
				variant="outline"
				class="aria-invalid:!border-destructive"
				disabled={editingBlocked || target === undefined || ReferenceBrowser === undefined}
				onclick={() => openBrowser("select")}
			>
				<SearchIcon />
				{i18n.t("plugin.seo:selectImage")}
			</Button>
		{:else}
			<div
				class="flex min-w-0 items-center gap-2.5 rounded-[4px] border border-control-border bg-control p-2 aria-invalid:!border-destructive/65"
				aria-invalid={controlARIA["aria-invalid"]}
			>
				<button
					id={field.id}
					aria-label={i18n.t("plugin.seo:inspectImage")}
					{...controlARIA}
					type="button"
					class="flex min-w-0 flex-1 items-center gap-2.5 rounded-[3px] text-start outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed"
					disabled={editingBlocked || target === undefined || ReferenceBrowser === undefined}
					onclick={() => openBrowser("inspect")}
				>
					<span
						class="grid size-11 shrink-0 place-items-center overflow-hidden rounded-[3px] bg-background text-foreground-faint"
					>
						{#if typeof document?.url === "string" && String(document?.mimeType ?? "").startsWith("image/")}
							<img class="size-full object-cover" src={document.url} alt="" />
						{:else}<ImageIcon aria-hidden="true" />{/if}
					</span>
					<span class="min-w-0 flex-1">
						<span class="block truncate text-[13px] text-foreground-strong">{documentLabel()}</span>
						<span class="block truncate font-mono text-[10px] text-foreground-faint">{value}</span>
						{#if loadFailed}<span class="block text-[11px] text-destructive">
								{i18n.t("plugin.seo:imageUnavailable")}
							</span>{/if}
					</span>
					<span class="inline-flex shrink-0 items-center gap-1 text-[11px] font-medium">
						<SearchIcon aria-hidden="true" />
						{i18n.t("plugin.seo:inspectImage")}
					</span>
				</button>
				{#if !field.admin.readOnly}<div class="flex shrink-0 gap-1">
						<Button
							variant="outline"
							size="xs"
							disabled={editingBlocked}
							onclick={() => openBrowser("select")}
						>
							<SearchIcon aria-hidden="true" />
							{i18n.t("plugin.seo:replaceImage")}
						</Button>
						<Button
							variant="ghost"
							size="icon-xs"
							disabled={editingBlocked}
							onclick={() => binding.set(null)}
							aria-label={i18n.t("plugin.seo:removeImage")}
						>
							<XIcon />
						</Button>
					</div>{/if}
			</div>
		{/if}
		<div class="flex items-center gap-2">
			<span
				class={[
					"rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide",
					value === "" ? "bg-destructive/10 text-destructive" : "bg-success/12 text-success",
				]}
			>
				{value === "" ? i18n.t("plugin.seo:noImage") : i18n.t("plugin.seo:good")}
			</span>
		</div>
		{#if generation.status === "error"}<p class="text-[12px] text-destructive" role="alert">
				{i18n.t("plugin.seo:generationFailed")}
			</p>{/if}
	</FieldFrame>
</div>

{#if browserOpen && ReferenceBrowser !== undefined && target !== undefined}
	<ReferenceBrowser
		open
		{field}
		collection={target}
		hasMany={false}
		selectedIDs={value === "" ? [] : [value]}
		readOnly={browserMode === "inspect" ? (field.admin.readOnly ?? false) : false}
		{...browserMode === "inspect" && document !== undefined ? { initialDocument: document } : {}}
		{...browserMode === "inspect" && value !== "" ? { initialDocumentID: value } : {}}
		locale={form.contentLocale ?? ""}
		onCommit={commit}
		onClose={() => (browserOpen = false)}
	/>
{/if}
