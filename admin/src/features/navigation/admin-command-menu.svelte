<script lang="ts">
	import { useNavigate } from "@hvniel/svelte-router";
	import type { SchemaCollection } from "@riducms/protocol";
	import SearchIcon from "~icons/lucide/search";

	import {
		CommandEmpty,
		CommandGroup,
		CommandGroupHeading,
		CommandGroupItems,
		CommandInput,
		CommandItem,
		CommandList,
		CommandRoot,
		CommandSeparator,
		CommandViewport,
	} from "@riducms/ui";
	import {
		Dialog,
		DialogContent,
		DialogDescription,
		DialogTitle,
	} from "@admin/components/ui/dialog";
	import { Kbd } from "@admin/components/ui/kbd";
	import type { AdminDocument } from "@admin/core/api/admin-client";
	import {
		collectionPath,
		createDocumentPath,
		documentPath,
	} from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	interface CommandDocument {
		collection: SchemaCollection;
		document: AdminDocument;
		title: string;
	}

	interface Props {
		open?: boolean;
		collections: readonly SchemaCollection[];
	}

	let { open = $bindable(false), collections }: Props = $props();
	const runtime = getAdminRuntime();
	const navigate = useNavigate();
	let query = $state("");
	let documents = $state.raw<CommandDocument[]>([]);
	let documentsLoading = $state(false);

	$effect(() => {
		if (!open || collections.length === 0) return;
		const request = new AbortController();
		loadDocuments(collections, request.signal);
		return () => request.abort();
	});

	function handleKeydown(event: KeyboardEvent) {
		if (event.key.toLocaleLowerCase() !== "k" || (!event.metaKey && !event.ctrlKey)) return;
		event.preventDefault();
		open = !open;
	}

	function go(path: string) {
		open = false;
		query = "";
		navigate(path);
	}

	function singularLabel(collection: SchemaCollection) {
		return runtime.i18n.text(collection.labels.singular, collection.labels.singularTranslations);
	}

	function pluralLabel(collection: SchemaCollection) {
		return runtime.i18n.text(collection.labels.plural, collection.labels.pluralTranslations);
	}

	async function loadDocuments(
		availableCollections: readonly SchemaCollection[],
		signal: AbortSignal
	) {
		documentsLoading = true;
		try {
			const pages = await Promise.all(
				availableCollections.map(async (collection) => {
					const titleField =
						collection.fields.find((field) => field.type === "text") ??
						collection.fields.find((field) => field.type === "select");
					const page = await runtime.client.list(collection.slug, { limit: 6, signal });
					return page.docs.map((document) => ({
						collection,
						document,
						title: String(
							(titleField === undefined ? undefined : document[titleField.name]) ?? document.id
						),
					}));
				})
			);
			if (!signal.aborted) documents = pages.flat().slice(0, 8);
		} catch {
			if (!signal.aborted) documents = [];
		} finally {
			if (!signal.aborted) documentsLoading = false;
		}
	}
</script>

<svelte:document onkeydown={handleKeydown} />

<Dialog bind:open>
	<DialogContent
		class="ridu-command-dialog top-[16vh] w-[min(calc(100vw-32px),560px)] max-w-none -translate-y-0 gap-0 overflow-hidden p-0 sm:max-w-none"
		showCloseButton={false}
	>
		<DialogTitle class="sr-only">{runtime.i18n.t("navigation:navigateRidu")}</DialogTitle>
		<DialogDescription class="sr-only">
			{runtime.i18n.t("navigation:commandDescription")}
		</DialogDescription>

		<CommandRoot>
			<div class="flex items-center gap-2.5 border-b border-control-border px-4">
				<SearchIcon
					class="pointer-events-none size-3.75 shrink-0 text-foreground-tag"
					aria-hidden="true"
				/>
				<CommandInput
					bind:value={query}
					class="h-12 text-[13px]"
					placeholder={runtime.i18n.t("navigation:commandSearchPlaceholder")}
					aria-label={runtime.i18n.t("navigation:commandSearch")}
				/>
				<Kbd class="shrink-0">{runtime.i18n.t("general:escape")}</Kbd>
			</div>

			<CommandList class="max-h-[350px] p-2">
				<CommandViewport>
					{const emptyMessage = $derived(
						query === ""
							? runtime.i18n.t("navigation:nothingMatches")
							: runtime.i18n.t("navigation:nothingMatchesQuery", { query })
					)}

					<CommandEmpty>{emptyMessage}</CommandEmpty>

					{#if documents.length > 0}
						<CommandGroup>
							<CommandGroupItems class="grid gap-0.5">
								{#each documents as result (result.collection.id + ":" + result.document.id)}
									<CommandItem
										class="min-h-9 px-2.5 py-1.5 text-[13px]"
										value="{result.title} {pluralLabel(result.collection)} {runtime.i18n.t(
											'navigation:document'
										)}"
										onSelect={() => go(documentPath(result.collection.slug, result.document.id))}
									>
										<span
											class="font-mono w-9 shrink-0 rounded-[4px] border border-control-border py-0.5 text-center text-[8.5px] tracking-[0.08em] text-foreground-tag"
										>
											{runtime.i18n.t("navigation:documentBadge")}
										</span>
										<span class="min-w-0 flex-1 truncate">{result.title}</span>
										<span class="text-[11px] text-foreground-faint">
											{pluralLabel(result.collection)}
										</span>
									</CommandItem>
								{/each}
							</CommandGroupItems>
						</CommandGroup>
						<CommandSeparator class="my-2 h-px bg-control-border" />
					{:else if documentsLoading}
						<p
							class="font-mono px-2.5 py-3 text-[9px] tracking-[0.12em] text-foreground-faint uppercase"
						>
							{runtime.i18n.t("navigation:loadingDocuments")}
						</p>
					{/if}

					<CommandGroup>
						<CommandGroupHeading
							class="font-mono px-2.5 pt-2 pb-1.5 text-[9.5px] tracking-[0.14em] text-foreground-faint uppercase"
						>
							{runtime.i18n.t("navigation:navigate")}
						</CommandGroupHeading>
						<CommandGroupItems class="grid gap-0.5">
							{#each collections as collection (collection.id)}
								<CommandItem
									class="min-h-9 px-2.5 py-1.5 text-[13px]"
									value="{pluralLabel(collection)} {collection.slug} {runtime.i18n.t(
										'navigation:collection'
									)}"
									onSelect={() => go(collectionPath(collection.slug))}
								>
									<span
										class="font-mono w-10 shrink-0 rounded-[4px] border border-control-border py-0.5 text-center text-[9.5px] tracking-[0.08em] text-foreground-tag"
									>
										{runtime.i18n.t("navigation:collectionBadge")}
									</span>
									<span class="min-w-0 flex-1 truncate">{pluralLabel(collection)}</span>
									<span class="text-[12px] text-foreground-faint">
										{runtime.i18n.t("navigation:collection")}
									</span>
								</CommandItem>
							{/each}
						</CommandGroupItems>
					</CommandGroup>

					<CommandSeparator class="my-2 h-px bg-control-border" />

					<CommandGroup>
						<CommandGroupHeading
							class="font-mono px-2.5 pt-1 pb-1.5 text-[9.5px] tracking-[0.14em] text-foreground-faint uppercase"
						>
							{runtime.i18n.t("general:create")}
						</CommandGroupHeading>
						<CommandGroupItems class="grid gap-0.5">
							{#each collections as collection (collection.id)}
								<CommandItem
									class="min-h-9 px-2.5 py-1.5 text-[13px]"
									value="{runtime.i18n.t('navigation:newBadge')} {runtime.i18n.t(
										'general:create'
									)} {singularLabel(collection)} {collection.slug}"
									onSelect={() => go(createDocumentPath(collection.slug))}
								>
									<span
										class="font-mono w-10 shrink-0 rounded-[4px] border border-primary/25 py-0.5 text-center text-[9.5px] tracking-[0.08em] text-primary"
									>
										{runtime.i18n.t("navigation:newBadge")}
									</span>
									<span class="min-w-0 flex-1 truncate">
										{runtime.i18n.t("collections:createNew", {
											label: singularLabel(collection).toLocaleLowerCase(runtime.i18n.language),
										})}
									</span>
									<span class="text-[12px] text-foreground-faint">
										{runtime.i18n.t("general:create")}
									</span>
								</CommandItem>
							{/each}
						</CommandGroupItems>
					</CommandGroup>
				</CommandViewport>
			</CommandList>
		</CommandRoot>
	</DialogContent>
</Dialog>
