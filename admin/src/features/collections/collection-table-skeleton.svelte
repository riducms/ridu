<script lang="ts">
	import { Skeleton } from "@admin/components/ui/skeleton";
	import { Table, TableBody, TableCell, TableRow } from "@admin/components/ui/table";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	const runtime = getAdminRuntime();

	let {
		showStatus,
		showID,
		showCreated,
		showUpdated,
	}: {
		showStatus: boolean;
		showID: boolean;
		showCreated: boolean;
		showUpdated: boolean;
	} = $props();
	const widths = ["w-[46%]", "w-[61%]", "w-[38%]", "w-[54%]", "w-[43%]"];
</script>

<Table
	class="min-w-[640px] border-collapse text-start"
	aria-label={runtime.i18n.t("collections:loadingDocuments")}
>
	<TableBody>
		{#each widths as width (width)}
			<TableRow>
				<TableCell class="w-7 px-2.5"><Skeleton class="size-4" /></TableCell>
				<TableCell>
					<Skeleton class="h-3.5 {width}" />
				</TableCell>
				{#if showStatus}
					<TableCell class="w-36"><Skeleton class="h-3 w-20" /></TableCell>
				{/if}
				{#if showID}<TableCell><Skeleton class="h-2.5 w-20" /></TableCell>{/if}
				{#if showCreated}<TableCell><Skeleton class="h-2.5 w-16" /></TableCell>{/if}
				{#if showUpdated}
					<TableCell class="w-28"><Skeleton class="ms-auto h-2.5 w-16" /></TableCell>
				{/if}
			</TableRow>
		{/each}
	</TableBody>
</Table>
