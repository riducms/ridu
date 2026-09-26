<script lang="ts">
	import type { PluginFieldProps, FieldDocument } from "@riducms/plugin";
	import { Button, FieldFrame, fieldControlARIA } from "@riducms/ui";
	import ImageIcon from "~icons/lucide/image";
	import PencilIcon from "~icons/lucide/pencil";
	import XIcon from "~icons/lucide/x";

	import { GenerationController } from "@plugin-seo/generation-controller.svelte";
	import "@plugin-seo/seo.scss";

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
	let browserMode = $state<"create" | "inspect" | "select">("select");
	let initialFile = $state.raw<File>();
	let dragging = $state(false);
	let document = $state.raw<FieldDocument>();
	let loadFailed = $state(false);
	const generation = new GenerationController();
	const intakeEnabled = $derived(
		!editingBlocked && target !== undefined && ReferenceBrowser !== undefined
	);
	const canCreateImage = $derived(
		intakeEnabled && target !== undefined && authoring.canCreateDocument(target.slug)
	);

	$effect(() => () => generation.cancel());
	$effect(() => {
		const id = value;
		void authoring.documentRevision;
		void authoring.locale;
		if (id === "" || target === undefined || authoring === undefined) {
			document = undefined;
			loadFailed = false;
			return;
		}
		const request = new AbortController();
		document = undefined;
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
		closeBrowser();
	}

	function closeBrowser() {
		browserOpen = false;
		initialFile = undefined;
		dragging = false;
	}

	function openBrowser(mode: "create" | "inspect" | "select", file?: File) {
		browserMode = mode;
		initialFile = file;
		browserOpen = true;
	}

	function drop(event: DragEvent) {
		event.preventDefault();
		dragging = false;
		const file = event.dataTransfer?.files[0];
		if (canCreateImage && file) openBrowser("create", file);
	}

	function documentLabel() {
		for (const key of ["filename", "title", "name", "alt"] as const) {
			const candidate = document?.[key];
			if (typeof candidate === "string" && candidate.length > 0) return candidate;
		}
		return value;
	}

	function fileSize(bytes: unknown): string | undefined {
		if (typeof bytes !== "number" || !Number.isFinite(bytes) || bytes < 0) return undefined;
		if (bytes < 1024)
			return i18n.formatNumber(bytes, { style: "unit", unit: "byte", unitDisplay: "long" });

		const units = ["KB", "MB", "GB", "TB"];
		const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length);
		return `${i18n.formatNumber(bytes / 1024 ** exponent, { maximumFractionDigits: 0 })}${units[exponent - 1]}`;
	}

	function documentMetadata() {
		if (loadFailed) return i18n.t("plugin.seo:imageUnavailable");
		return [
			fileSize(document?.filesize),
			typeof document?.width === "number" && typeof document?.height === "number"
				? `${document.width}x${document.height}`
				: undefined,
			typeof document?.mimeType === "string" ? document.mimeType : undefined,
		]
			.filter((part): part is string => part !== undefined)
			.join(" — ");
	}
</script>

<div data-field-path={field.path}>
	{#snippet headingAction()}
		<span class="ridu-seo-heading-separator" aria-hidden="true">—</span>
		<Button
			variant="link"
			size="xs"
			class="ridu-seo-generate"
			disabled={editingBlocked || generation.status === "pending"}
			aria-busy={generation.status === "pending"}
			onclick={generate}
		>
			{i18n.t("plugin.seo:autoGenerate")}
		</Button>
	{/snippet}
	<FieldFrame
		controlID={field.id}
		label={field.admin.label}
		required={field.required}
		readOnly={field.admin.readOnly}
		description={field.admin.description}
		errors={issues.map((issue) => issue.message)}
		class="ridu-seo-field"
		headingAction={generationEnabled ? headingAction : undefined}
	>
		{#if generationEnabled}
			<p class="ridu-seo-guidance">{i18n.t("plugin.seo:imageAutoGenerationTip")}</p>
		{/if}
		{#if value === ""}
			<!-- File drops supplement the keyboard-accessible create and browse actions. -->
			<!-- svelte-ignore a11y_no_static_element_interactions -->
			<div
				class="ridu-seo-image-intake"
				role="region"
				aria-label={field.admin.label}
				data-dragging={dragging}
				data-invalid={controlARIA["aria-invalid"]}
				ondragover={(event) => {
					event.preventDefault();
					if (canCreateImage) {
						dragging = true;
						if (event.dataTransfer) event.dataTransfer.dropEffect = "copy";
					}
				}}
				ondragleave={(event) => {
					if (!event.currentTarget.contains(event.relatedTarget as Node | null)) dragging = false;
				}}
				ondrop={drop}
			>
				{#if canCreateImage}
					<Button variant="secondary" size="sm" onclick={() => openBrowser("create")}>
						{i18n.t("plugin.seo:createNewImage")}
					</Button>
					<span>{i18n.t("plugin.seo:or")}</span>
				{/if}
				<Button
					id={field.id}
					aria-label={i18n.t("plugin.seo:chooseExistingImage")}
					{...controlARIA}
					variant="secondary"
					size="sm"
					disabled={editingBlocked || target === undefined || ReferenceBrowser === undefined}
					onclick={() => openBrowser("select")}
				>
					{i18n.t("plugin.seo:chooseExistingImage")}
				</Button>
				{#if canCreateImage}
					<span class="ridu-seo-image-intake-copy">
						{i18n.t("plugin.seo:imageSourceTip")}
					</span>
				{/if}
			</div>
		{:else}
			<div class="ridu-seo-image-selection" aria-invalid={controlARIA["aria-invalid"]}>
				<div class="ridu-seo-image-content">
					<span class="ridu-seo-image-thumbnail">
						{#if typeof document?.url === "string" && String(document?.mimeType ?? "").startsWith("image/")}
							<img src={document.url} alt="" />
						{:else}
							<ImageIcon aria-hidden="true" />
						{/if}
					</span>
					<span class="ridu-seo-image-details">
						{#if typeof document?.url === "string"}
							<a class="ridu-seo-image-name" href={document.url} target="_blank" rel="noreferrer">
								{documentLabel()}
							</a>
						{:else}
							<span class="ridu-seo-image-name">{documentLabel()}</span>
						{/if}
						<span class="ridu-seo-image-metadata">{documentMetadata()}</span>
					</span>
				</div>
				<div class="ridu-seo-image-actions">
					<Button
						id={field.id}
						{...controlARIA}
						variant="ghost"
						size="icon-xs"
						disabled={editingBlocked || target === undefined || ReferenceBrowser === undefined}
						onclick={() => openBrowser("inspect")}
						aria-label={i18n.t("plugin.seo:inspectImage")}
					>
						<PencilIcon />
					</Button>
					{#if !field.admin.readOnly}
						<Button
							variant="ghost"
							size="icon-xs"
							disabled={editingBlocked}
							onclick={() => binding.set(null)}
							aria-label={i18n.t("plugin.seo:removeImage")}
						>
							<XIcon />
						</Button>
					{/if}
				</div>
			</div>
		{/if}
		<div class="ridu-seo-image-status">
			<span class="ridu-seo-pill" data-tone={value === "" ? "danger" : "success"}>
				{value === "" ? i18n.t("plugin.seo:noImage") : i18n.t("plugin.seo:good")}
			</span>
		</div>
		{#if generation.status === "error"}
			<p class="ridu-seo-error" role="alert">
				{i18n.t("plugin.seo:generationFailed")}
			</p>
		{/if}
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
		initialCreate={browserMode === "create"}
		{...initialFile === undefined ? {} : { initialFile }}
		{...browserMode === "inspect" && document !== undefined ? { initialDocument: document } : {}}
		{...browserMode === "inspect" && value !== "" ? { initialDocumentID: value } : {}}
		locale={form.contentLocale ?? ""}
		onCommit={commit}
		onClose={closeBrowser}
	/>
{/if}
