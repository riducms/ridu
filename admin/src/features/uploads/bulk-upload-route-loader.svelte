<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import { Button } from "@riducms/ui";
	import { preparedAdminModule } from "@admin/core/bootstrap/admin-route-modules";
	import { registerAdminScrollPage } from "@admin/core/routing/admin-scroll.svelte";

	registerAdminScrollPage({ ready: () => false });
	const i18n = getAdminI18n();
	const route =
		preparedAdminModule<typeof import("@admin/features/uploads/bulk-upload-route.svelte")>(
			"bulk-upload"
		) ?? import("@admin/features/uploads/bulk-upload-route.svelte");
</script>

{#await route}
	<section
		class="ridu-bulk-upload-module"
		data-ridu-loading-surface="bulk-upload-module"
		aria-busy="true"
		aria-label={i18n.t("general:loading")}
	>
		<p>{i18n.t("general:loading")}</p>
	</section>
{:then { default: BulkUploadRoute }}
	<BulkUploadRoute />
{:catch}
	<section class="ridu-bulk-upload-module ridu-bulk-upload-module--error" role="alert">
		<p>{i18n.t("uploads:workspaceLoadFailed")}</p>
		<Button onclick={() => window.location.reload()}>{i18n.t("general:reloadPage")}</Button>
	</section>
{/await}

<style lang="scss">
	@layer ridu.components {
		.ridu-bulk-upload-module {
			display: grid;
			place-content: center;
			justify-items: center;
			gap: 20px;
			min-block-size: 288px;
			padding: 20px;
			color: var(--foreground-muted);
			font-size: var(--font-size-body);

			&--error {
				color: var(--destructive);
			}

			p {
				margin: 0;
			}
		}
	}
</style>
