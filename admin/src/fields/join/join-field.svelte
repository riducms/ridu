<script module lang="ts">
	import type { AdminI18n } from "@riducms/plugin";
	interface JoinColumn {
		path: string;
		label: string;
	}

	function joinDocuments(value: unknown) {
		return Array.isArray(value)
			? value.filter(
					(document): document is AdminDocument =>
						typeof document === "object" && document !== null && typeof document.id === "string"
				)
			: [];
	}

	function joinColumns(
		field: SchemaField,
		collection: SchemaCollection | undefined,
		i18n: AdminI18n
	): JoinColumn[] {
		if (collection === undefined) return [{ path: "id", label: i18n.t("fields:id") }];
		const configured = field.join?.defaultColumns ?? collection.admin.defaultColumns ?? [];
		const fallback =
			collection.admin.useAsTitle ??
			collection.fields.find((candidate) => candidate.type === "text")?.path ??
			"id";
		const paths = configured.length > 0 ? configured : [fallback, "updatedAt"];
		return paths.map((path) => ({
			path,
			label:
				collection.fields.find((candidate) => candidate.path === path)?.admin.label ??
				systemColumnLabel(path, i18n) ??
				humanize(path, i18n.language),
		}));
	}

	function joinDefaultValues(path: string, id: string) {
		const values: Record<string, unknown> = {};
		writePath(values, path, id);
		return values;
	}

	function writePath(values: Record<string, unknown>, path: string, value: unknown) {
		const segments = path.split(".").filter(Boolean);
		let current = values;
		for (const segment of segments.slice(0, -1)) {
			const existing = current[segment];
			if (typeof existing === "object" && existing !== null && !Array.isArray(existing)) {
				current = existing as Record<string, unknown>;
			} else {
				const nested: Record<string, unknown> = {};
				current[segment] = nested;
				current = nested;
			}
		}
		const last = segments.at(-1);
		if (last !== undefined) current[last] = value;
	}

	function readPath(value: unknown, path: string): unknown {
		let current = value;
		for (const segment of path.split(".")) {
			if (typeof current !== "object" || current === null || Array.isArray(current))
				return undefined;
			current = (current as Record<string, unknown>)[segment];
		}
		return current;
	}

	function displayValue(value: unknown, i18n: AdminI18n): string {
		if (value === null || value === undefined || value === "") return "—";
		if (typeof value === "boolean") return i18n.t(value ? "general:yes" : "general:no");
		if (typeof value === "number") return i18n.formatNumber(value);
		if (typeof value === "object") {
			if (Array.isArray(value)) return i18n.t("fields:itemCount", { count: value.length });
			const record = value as Record<string, unknown>;
			return String(
				record.title ?? record.name ?? record.email ?? record.id ?? i18n.t("fields:relatedDocument")
			);
		}
		if (typeof value === "string" && /^\d{4}-\d{2}-\d{2}T/.test(value)) {
			const date = new Date(value);
			if (!Number.isNaN(date.valueOf())) return i18n.formatDate(date, { dateStyle: "medium" });
		}
		return String(value);
	}

	function documentLabel(document: AdminDocument) {
		return String(document.title ?? document.name ?? document.email ?? document.id);
	}

	function sortDocuments(documents: readonly AdminDocument[], sort: string, i18n: AdminI18n) {
		if (sort === "") return [...documents];
		const descending = sort.startsWith("-");
		const path = descending ? sort.slice(1) : sort;
		return [...documents].sort((left, right) => {
			const leftValue = displayValue(readPath(left, path), i18n);
			const rightValue = displayValue(readPath(right, path), i18n);
			const comparison = leftValue.localeCompare(rightValue, i18n.language, { numeric: true });
			return descending ? -comparison : comparison;
		});
	}

	function humanize(value: string, language: string) {
		return value
			.split(".")
			.at(-1)!
			.replace(/([a-z\d])([A-Z])/g, "$1 $2")
			.replaceAll(/[_-]+/g, " ")
			.replace(/\b\w/g, (letter) => letter.toLocaleUpperCase(language));
	}

	function systemColumnLabel(path: string, i18n: AdminI18n) {
		if (path === "id") return i18n.t("fields:id");
		if (path === "_status") return i18n.t("fields:status");
		if (path === "createdAt") return i18n.t("fields:createdAt");
		if (path === "updatedAt") return i18n.t("fields:updatedAt");
		return undefined;
	}
</script>

<script lang="ts">
	import { Link } from "@hvniel/svelte-router";
	import type { FieldAuthoringHost } from "@riducms/plugin";
	import type { SchemaCollection, SchemaField } from "@riducms/protocol";
	import { onDestroy } from "svelte";
	import ArrowDownIcon from "~icons/lucide/arrow-down";
	import ArrowUpIcon from "~icons/lucide/arrow-up";
	import ArrowUpRightIcon from "~icons/lucide/arrow-up-right";
	import ListPlusIcon from "~icons/lucide/list-plus";

	import { Button } from "@admin/components/ui/button";
	import type { AdminDocument } from "@admin/core/api/admin-client";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import { documentPath } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import FieldShell from "@admin/fields/field-shell.svelte";

	let {
		field,
		form,
		authoring,
	}: { field: SchemaField; form: FormController; authoring?: FieldAuthoringHost } = $props();
	const runtime = getAdminRuntime();
	const notifications = getNotificationCenter();
	const ReferenceBrowser = $derived(authoring?.referenceBrowser);
	const targetCollection = $derived(
		authoring?.collections.find((collection) => collection.id === field.join?.collectionId)
	);
	const sourceID = $derived(form.resource?.global ? undefined : form.resource?.id);
	const targetOperations = $derived(
		targetCollection === undefined ? undefined : runtime.collectionOperations[targetCollection.slug]
	);
	const canAttach = $derived(sourceID !== undefined && targetOperations?.update === true);
	const canCreate = $derived(
		sourceID !== undefined && field.join?.allowCreate !== false && targetOperations?.create === true
	);
	const canManage = $derived(!field.admin.readOnly && (canAttach || canCreate));
	const editingBlocked = $derived(form.editingBlocked);
	let browserOpen = $state(false);
	let pending = $state(false);
	let mutationError = $state<string>();
	let mutationController: AbortController | undefined;
	let documents = $state.raw<AdminDocument[]>([]);
	let sortOverride = $state<string>();
	const sort = $derived(sortOverride ?? field.join?.defaultSort ?? "");
	const columns = $derived(joinColumns(field, targetCollection, runtime.i18n));
	const sortedDocuments = $derived(sortDocuments(documents, sort, runtime.i18n));
	onDestroy(() => mutationController?.abort());

	$effect(() => {
		form.revision;
		documents = joinDocuments(form.get(field.path));
	});

	function toggleSort(path: string) {
		sortOverride = sort === path ? `-${path}` : sort === `-${path}` ? "" : path;
	}

	async function commit(ids: string[]) {
		if (editingBlocked) return false;
		const source = form.resource;
		if (
			source?.id === undefined ||
			source.global ||
			sourceID === undefined ||
			targetCollection === undefined ||
			field.join === undefined
		)
			return false;
		pending = true;
		mutationError = undefined;
		const current = new Set(documents.map((document) => document.id));
		const requested = new Set(ids);
		const additions = ids.filter((id) => !current.has(id));
		const removals = documents
			.filter((document) => !requested.has(document.id))
			.map((document) => document.id);
		if (additions.length === 0 && removals.length === 0) {
			pending = false;
			return true;
		}
		mutationController = new AbortController();
		try {
			const result = await runtime.client.mutateJoin(
				source.collection,
				source.id,
				field.path,
				{ additions, removals },
				{ signal: mutationController.signal, locale: form.contentLocale }
			);
			if (mutationController.signal.aborted) return false;
			documents = joinDocuments(readPath(result.doc, field.path));
			runtime.documentsChanged();
			notifications.success({
				title: runtime.i18n.t("fields:updated", { label: field.admin.label }),
				message: runtime.i18n.t("fields:joinChanges", {
					added: result.added,
					removed: result.removed,
				}),
			});
			return true;
		} catch (cause) {
			if (mutationController.signal.aborted) return false;
			mutationError =
				cause instanceof Error ? cause.message : runtime.i18n.t("errors:relatedDocumentsUpdate");
			await refresh().catch(() => {});
			notifications.error({
				title: runtime.i18n.t("errors:updateUnconfirmed", { label: field.admin.label }),
				message: mutationError,
			});
			return false;
		} finally {
			pending = false;
			mutationController = undefined;
		}
	}

	async function refresh() {
		const source = form.resource;
		if (source?.id === undefined) return;
		const document = await runtime.client.find(source.collection, source.id, {
			locale: form.contentLocale,
		});
		documents = joinDocuments(document[field.name]);
	}

	function setBrowserOpen(open: boolean) {
		browserOpen = open;
	}
</script>

<FieldShell
	{field}
	issues={mutationError === undefined
		? []
		: [{ code: "join_update_failed", path: field.path, message: mutationError }]}
>
	<div
		class="overflow-hidden rounded-[4px] border border-control-border"
		data-join-field={field.name}
	>
		<div
			class="flex items-center justify-between gap-3 border-b border-control-border bg-control px-3 py-2"
		>
			<p class="font-mono text-[9.5px] tracking-[0.09em] text-foreground-faint uppercase">
				{runtime.i18n.t("fields:showingRelated", {
					count: documents.length,
					label: targetCollection?.labels.plural ?? field.admin.label,
				})}
			</p>
			{#if canManage}
				<Button
					variant="outline"
					size="sm"
					disabled={pending || editingBlocked}
					onclick={() => (browserOpen = true)}
				>
					<ListPlusIcon class="size-3" />
					{runtime.i18n.t(pending ? "fields:updating" : "fields:manageRelationships")}
				</Button>
			{/if}
		</div>
		{#if sortedDocuments.length > 0}
			<div class="overflow-x-auto">
				<table class="w-full min-w-[520px] border-collapse text-start text-[12px]">
					<thead>
						<tr class="border-b border-control-border bg-control">
							{#each columns as column (column.path)}
								<th class="px-3 py-2 font-medium text-foreground-faint">
									<button
										type="button"
										class="inline-flex items-center gap-1.5 outline-none hover:text-foreground focus-visible:text-primary"
										onclick={() => toggleSort(column.path)}
										aria-label={runtime.i18n.t("fields:sortBy", { label: column.label })}
									>
										{column.label}
										{#if sort === column.path}<ArrowUpIcon
												class="size-3"
											/>{:else if sort === `-${column.path}`}<ArrowDownIcon class="size-3" />{/if}
									</button>
								</th>
							{/each}
							<th class="w-10 px-3 py-2">
								<span class="sr-only">
									{runtime.i18n.t("general:open")}
								</span>
							</th>
						</tr>
					</thead>
					<tbody>
						{#each sortedDocuments as document (document.id)}
							<tr class="border-b border-control-border last:border-b-0 hover:bg-control">
								{#each columns as column (column.path)}
									<td class="max-w-64 px-3 py-2.5 text-foreground-muted">
										<span class="block truncate">
											{displayValue(readPath(document, column.path), runtime.i18n)}
										</span>
									</td>
								{/each}
								<td class="px-3 py-2.5">
									<Link
										class="inline-flex text-foreground-faint hover:text-primary"
										to={documentPath(field.join?.collectionSlug ?? "", document.id)}
										aria-label={runtime.i18n.t("fields:open", {
											label: documentLabel(document),
										})}
									>
										<ArrowUpRightIcon class="size-3.5 rtl:-rotate-90" />
									</Link>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{:else}
			<p class="px-3 py-4 text-[12px] text-foreground-faint">
				{runtime.i18n.t("fields:noRelated", {
					label: (targetCollection?.labels.plural ?? field.admin.label).toLocaleLowerCase(
						runtime.i18n.language
					),
				})}
			</p>
		{/if}
	</div>
</FieldShell>

{#if browserOpen && targetCollection !== undefined && ReferenceBrowser !== undefined && sourceID !== undefined}
	<ReferenceBrowser
		bind:open={() => browserOpen, setBrowserOpen}
		{field}
		collection={targetCollection}
		hasMany={true}
		selectedIDs={documents.map((document) => document.id)}
		readOnly={!canAttach && !canCreate}
		defaultValues={joinDefaultValues(field.join?.on ?? "", sourceID)}
		allowCreate={canCreate}
		locale={form.contentLocale}
		onCommit={commit}
		onClose={() => setBrowserOpen(false)}
	/>
{/if}
