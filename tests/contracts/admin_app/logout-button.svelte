<script lang="ts">
	import type { AdminLogoutButtonProps } from "@riducms/plugin";

	let { host }: AdminLogoutButtonProps = $props();
	let pending = $state(false);

	async function logout() {
		pending = true;
		try {
			await host.logout();
		} finally {
			pending = false;
		}
	}
</script>

<button
	type="button"
	class="h-9 w-full rounded-md px-2.5 text-left text-[12.5px] hover:bg-white/5 disabled:opacity-60"
	disabled={pending}
	onclick={logout}
>
	{pending ? "Leaving the extension…" : "Plugin sign out"}
</button>
