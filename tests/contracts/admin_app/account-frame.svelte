<script lang="ts">
	import type { AdminAccountComponentProps } from "@riducms/plugin";

	let { defaultView, surface, user }: AdminAccountComponentProps = $props();
</script>

<aside class="mx-auto mt-8 w-full max-w-[820px] rounded-lg border border-primary/20 px-4 py-3">
	<p class="text-[12px] text-primary">
		Custom {surface} wrapper for {String(user?.name ?? "the signed-in user")}
	</p>
</aside>
{@render defaultView()}
