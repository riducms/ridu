<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import type { AdminDocument } from "@admin/core/api/admin-client";

	let {
		renditions,
		document,
	}: {
		renditions: [string, Record<string, unknown>][];
		document: AdminDocument;
	} = $props();
	const i18n = getAdminI18n();
	let selected = $state(0);
	const current = $derived(renditions[selected] ?? renditions[0]);

	function filename(rendition: Record<string, unknown>) {
		return String(
			rendition.filename ??
				String(rendition.objectKey ?? "")
					.split("/")
					.at(-1) ??
				document.filename
		);
	}

	function metadata(rendition: Record<string, unknown>) {
		return `${i18n.formatNumber(Number(rendition.filesize ?? 0) / 1024, { maximumFractionDigits: 0 })}KB · ${rendition.width} × ${rendition.height} · ${rendition.mimeType}`;
	}
</script>

{#if current}
	<div class="ridu-upload-renditions">
		<section class="ridu-rendition-preview" aria-label={current[0]}>
			<header>
				<p>{current[0]}</p>
				<a href={String(current[1].url)} target="_blank" rel="noreferrer">
					{filename(current[1])}
				</a>
				<p>{metadata(current[1])}</p>
			</header>
			<div class="ridu-rendition-image">
				<img src={String(current[1].url)} alt={String(document.alt ?? current[0])} />
			</div>
		</section>

		<div class="ridu-rendition-list" aria-label={i18n.t("uploads:imageSizes")}>
			{#each renditions as [name, rendition], index (name)}
				<button
					type="button"
					class="ridu-rendition-option"
					aria-pressed={selected === index}
					onclick={() => (selected = index)}
				>
					<img src={String(rendition.url)} alt="" />
					<span>
						<span class="ridu-rendition-label">{name}</span>
						<strong>{filename(rendition)}</strong>
						<span class="ridu-rendition-label">{metadata(rendition)}</span>
					</span>
				</button>
			{/each}
		</div>
	</div>
{/if}
