<script lang="ts">
	import Chevron from "@admin/components/icons/chevron.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { documentLabel } from "@admin/features/documents/document-title";
	import {
		formatVersionValue,
		versionTextDiff,
		versionReferences,
		type VersionDiffRow,
	} from "@admin/features/versions/version-diff";
	import type { AdminDocument } from "@admin/core/api/admin-client";

	let {
		rows,
		modifiedOnly,
		references,
	}: {
		rows: VersionDiffRow[];
		modifiedOnly: boolean;
		references: ReadonlyMap<string, AdminDocument>;
	} = $props();
	const runtime = getAdminRuntime();
	const i18n = runtime.i18n;
</script>

{#snippet fields(items: VersionDiffRow[])}
	{#each items.filter((item) => !modifiedOnly || item.changed) as row (row.path)}
		{#if row.children && !row.field?.localized}
			<details open class="ridu-version-diff__group">
				<summary class="ridu-version-diff__group-label">
					<Chevron />{row.label}
					{#if row.locale}
						<span class="ridu-version-diff__locale">{row.locale}</span>
					{/if}
				</summary>
				<div class="ridu-version-diff__group-content">
					{#if row.children.length}
						{@render fields(row.children)}
					{:else}
						<p class="ridu-version-diff__empty">
							{i18n.t("versions:noRows")}
						</p>
					{/if}
				</div>
			</details>
		{:else if row.children}
			{@render fields(row.children)}
		{:else}
			{const oldText = $derived(formatVersionValue(row.before, i18n, row))}
			{const newText = $derived(formatVersionValue(row.after, i18n, row))}
			{const textDiff = $derived(versionTextDiff(oldText, newText))}
			<div class="ridu-version-diff__field" data-field-path={row.path}>
				<div class="ridu-version-diff__label">
					{row.label}
					{#if row.locale}
						<span class="ridu-version-diff__locale">
							{row.locale}
						</span>
					{/if}
				</div>
				<div class="ridu-version-diff__values">
					{#each ["before", "after"] as side}
						{const value = $derived(side === "before" ? row.before : row.after)}
						{const related = $derived(versionReferences(row, value))}
						<div
							class={[
								"ridu-version-diff__value",
								row.changed &&
									(side === "before"
										? "ridu-version-diff__value--removed"
										: "ridu-version-diff__value--added"),
								(row.field?.relationship || row.field?.upload) &&
									"ridu-version-diff__value--references",
							]}
						>
							{#if row.field?.relationship || row.field?.upload}
								{#each related as reference (reference.key)}
									{const document = $derived(references.get(reference.key))}
									{const collection = $derived(
										runtime.manifest?.collections.find((item) => item.slug === reference.collection)
									)}
									<div class="ridu-version-diff__reference">
										{#if row.field.upload && typeof document?.url === "string" && typeof document?.mimeType === "string" && document.mimeType.startsWith("image/")}
											<img src={document.url} alt="" />
										{:else if !row.field.relationship?.hasMany && collection}
											<span class="ridu-version-diff__reference-type">
												{i18n.text(
													collection.labels.singular,
													collection.labels.singularTranslations
												)}
											</span>
										{/if}
										<strong>
											{document
												? row.field.upload
													? String(document.filename ?? document.id)
													: documentLabel(collection, document)
												: reference.id}
										</strong>
									</div>
								{/each}
							{:else}
								<p>
									{#each side === "before" ? textDiff.before : textDiff.after as part}
										{#if part.changed && side === "before"}
											<del>
												{part.text}
											</del>
										{:else if part.changed}
											<ins>
												{part.text}
											</ins>
										{:else}
											{part.text}
										{/if}
									{/each}
								</p>
								{#if row.changed && oldText === newText && typeof value === "object" && value !== null}
									<pre>{JSON.stringify(value, null, 2)}</pre>
								{/if}
							{/if}
						</div>
					{/each}
				</div>
			</div>
		{/if}
	{/each}
{/snippet}

<div class="ridu-version-diff">
	{#if modifiedOnly && !rows.some((row) => row.changed)}
		<p class="ridu-versions__empty">{i18n.t("versions:noModifiedFields")}</p>
	{:else}
		{@render fields(rows)}
	{/if}
</div>
