<script lang="ts">
	import type { SchemaDateFormat } from "@riducms/protocol";
	import { fieldControlARIA, Button } from "@riducms/ui";
	import { getAdminI18n } from "@riducms/plugin";

	import { preparedAdminModule } from "@admin/core/bootstrap/admin-route-modules";
	import "@admin/components/ui/date-value-control/date-value-control-loader.scss";

	type ControlSize = "field" | "compact" | "toolbar";
	type DateValueControlProps = {
		id: string;
		name?: string;
		appearance?: SchemaDateFormat;
		value?: string;
		timeZone?: string;
		disabled?: boolean;
		readonly?: boolean;
		required?: boolean;
		invalid?: boolean;
		hasDescription?: boolean;
		label?: string;
		class?: string;
		size?: ControlSize;
		onValueChange: (value: string) => void;
	};

	let props: DateValueControlProps = $props();
	const i18n = getAdminI18n();
	const control =
		preparedAdminModule<
			typeof import("@admin/components/ui/date-value-control/date-value-control.svelte")
		>("date") ?? import("@admin/components/ui/date-value-control/date-value-control.svelte");
	const controlARIA = $derived(
		fieldControlARIA(props.id, props.hasDescription ?? false, props.invalid ?? false)
	);
</script>

{#await control}
	<div
		id={props.id}
		class={["ridu-date-control-loader", props.class]}
		data-ridu-loading-surface="date-control-module"
		role="status"
		aria-label={props.label}
		aria-busy="true"
		{...controlARIA}
	>
		{i18n.t("general:loading")}
	</div>
{:then { default: Control }}
	<Control {...props} />
{:catch}
	<div
		id={props.id}
		class={["ridu-date-control-loader ridu-date-control-loader--failed", props.class]}
		role="alert"
		{...controlARIA}
	>
		<p>{i18n.t("fields:dateControlLoadFailed")}</p>
		<Button
			variant="outline"
			size="sm"
			aria-label={i18n.t("general:reloadPage")}
			onclick={() => window.location.reload()}
		>
			{i18n.t("general:reloadPage")}
		</Button>
	</div>
{/await}
