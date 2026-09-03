<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import { ConfirmationDialog } from "@riducms/ui";

	let {
		open = $bindable(false),
		title,
		description,
		confirmLabel,
		cancelLabel,
		destructive = false,
		disabled = false,
		onconfirm,
	}: {
		open?: boolean;
		title: string;
		description: string;
		confirmLabel: string;
		cancelLabel?: string;
		destructive?: boolean;
		disabled?: boolean;
		onconfirm: () => void | Promise<void>;
	} = $props();
	const i18n = getAdminI18n();
	const resolvedCancelLabel = $derived(cancelLabel ?? i18n.t("general:cancel"));
</script>

<ConfirmationDialog
	bind:open
	{title}
	{description}
	{confirmLabel}
	cancelLabel={resolvedCancelLabel}
	{destructive}
	{disabled}
	{onconfirm}
/>
