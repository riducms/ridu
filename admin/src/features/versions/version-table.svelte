<script lang="ts">
	import { Link } from "@hvniel/svelte-router";
	import { getAdminI18n } from "@riducms/plugin";
	import type { AdminDocument, AdminVersion } from "@admin/core/api/admin-client";
	import SortHeading from "@admin/features/collections/controls/sort-heading.svelte";
	import ListPagination from "@admin/features/collections/controls/list-pagination.svelte";
	import {
		versionDate,
		versionListPage,
		versionStatus,
	} from "@admin/features/versions/version-history";
	import "@admin/components/data/list-table.scss";

	let {
		versions,
		document,
		search,
		onQueryChange,
		versionPath,
		onSelect,
	}: {
		versions: readonly AdminVersion[];
		document?: AdminDocument;
		search: URLSearchParams;
		onQueryChange: (changes: Record<string, string>) => void;
		versionPath?: (version: AdminVersion) => string;
		onSelect?: (version: AdminVersion) => void;
	} = $props();

	const i18n = getAdminI18n();
	const view = $derived(versionListPage(versions, search));
</script>

{#if versions.length === 0}
	<p class="ridu-versions__empty">{i18n.t("versions:noRevisions")}</p>
{:else}
	<div class="ridu-versions__table-scroll">
		<table class="ridu-list-table ridu-versions__table" aria-label={i18n.t("versions:history")}>
			<thead>
				<tr>
					<SortHeading
						column={{ path: "updatedAt", label: i18n.t("versions:updatedAt") }}
						sort={view.sort}
						onSort={(path, descending) =>
							onQueryChange({ sort: descending ? `-${path}` : path, page: "1" })}
					/>
					<th scope="col">{i18n.t("versions:versionID")}</th>
					<th scope="col">{i18n.t("versions:status")}</th>
				</tr>
			</thead>
			<tbody>
				{#each view.docs as version (version.ID)}
					{const status = $derived(versionStatus(version, document))}
					<tr>
						<td>
							{#if onSelect}
								<button
									type="button"
									class="ridu-versions__select"
									onclick={() => onSelect?.(version)}
								>
									{versionDate(version.CreatedAt, i18n)}
								</button>
							{:else if versionPath}
								<Link to={versionPath(version)}>{versionDate(version.CreatedAt, i18n)}</Link>
							{/if}
						</td>
						<td>{version.ID}</td>
						<td>
							<span
								class={[
									"ridu-version-status",
									status === "currentlyPublished" && "ridu-version-status--published",
								]}
							>
								{i18n.t(`versions:${status}`)}
							</span>
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>

	<ListPagination
		pagination={view.pagination}
		pageSize={view.pagination.limit}
		onPageChange={(page) => onQueryChange({ page: String(page) })}
		onPageSizeChange={(limit) => onQueryChange({ limit: String(limit), page: "1" })}
	/>
{/if}
