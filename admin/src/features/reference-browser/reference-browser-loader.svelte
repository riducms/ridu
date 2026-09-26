<script lang="ts">
	import { getAdminI18n, type FieldReferenceBrowserProps } from "@riducms/plugin";
	import { Button } from "@riducms/ui";

	let { open = $bindable(false), ...props }: FieldReferenceBrowserProps = $props();
	const i18n = getAdminI18n();
	const browser = import("@admin/features/reference-browser/reference-browser.svelte");

	function close() {
		open = false;
		props.onClose?.();
	}
</script>

{#await browser}
	{#if open}
		<p role="status" aria-busy="true" data-ridu-loading-surface="reference-browser">
			{i18n.t("reference:loadingRelated")}
		</p>
	{/if}
{:then { default: Browser }}
	<Browser bind:open {...props} />
{:catch}
	{#if open}
		<div class="grid gap-3" role="alert">
			<p>{i18n.t("reference:browserLoadFailed")}</p>
			<div class="flex gap-2">
				<Button onclick={() => window.location.reload()}>{i18n.t("general:reloadPage")}</Button>
				<Button variant="outline" onclick={close}>{i18n.t("general:close")}</Button>
			</div>
		</div>
	{/if}
{/await}
