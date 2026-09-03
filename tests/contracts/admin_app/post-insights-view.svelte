<script lang="ts">
	import type { AdminDocumentExtensionProps } from "@riducms/plugin";

	let { document }: AdminDocumentExtensionProps = $props();
</script>

<section aria-labelledby="post-insights-title">
	<p class="font-mono text-[9.5px] tracking-[0.15em] text-foreground-faint uppercase">
		Custom document view
	</p>
	<h1 id="post-insights-title" class="font-serif mt-2 text-[38px] leading-none text-foreground">
		Editorial insights
	</h1>
	<dl class="mt-7 grid gap-3 rounded-[9px] border border-white/7 bg-white/[0.02] p-5 text-[13px]">
		<div class="flex justify-between gap-4">
			<dt class="text-foreground-sub">Reading time</dt>
			<dd class="font-mono text-foreground">{String(document.readingMinutes ?? "—")} min</dd>
		</div>
		<div class="flex justify-between gap-4">
			<dt class="text-foreground-sub">Status</dt>
			<dd class="font-mono text-foreground">{document._status ?? "published"}</dd>
		</div>
	</dl>
</section>
