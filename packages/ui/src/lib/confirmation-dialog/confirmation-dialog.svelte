<script lang="ts">
	import { AlertDialog as AlertDialogPrimitive } from "bits-ui";

	import "#lib/confirmation-dialog/confirmation-dialog.scss";
	import { buttonVariants } from "#lib/button/button.svelte";

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
		cancelLabel: string;
		destructive?: boolean;
		disabled?: boolean;
		onconfirm: () => void | Promise<void>;
	} = $props();

	let pending = $state(false);
	// Only the autofocus handler reads this DOM binding.
	// svelte-ignore non_reactive_update
	let cancelButton: HTMLButtonElement | null = null;

	async function submitConfirmation() {
		if (disabled || pending) return;

		pending = true;

		try {
			await onconfirm();
			open = false;
		} finally {
			pending = false;
		}
	}

	function focusCancel(event: Event) {
		event.preventDefault();
		cancelButton?.focus();
	}
</script>

<AlertDialogPrimitive.Root bind:open>
	<AlertDialogPrimitive.Portal>
		<AlertDialogPrimitive.Overlay
			data-slot="confirmation-dialog-overlay"
			class="ridu-confirmation-overlay"
		/>
		<AlertDialogPrimitive.Content
			data-slot="confirmation-dialog-content"
			class="ridu-confirmation ridu-dialog-enter"
			onOpenAutoFocus={focusCancel}
			onEscapeKeydown={(event) => pending && event.preventDefault()}
		>
			<header class="ridu-confirmation__header">
				<AlertDialogPrimitive.Title class="ridu-confirmation__title">
					{title}
				</AlertDialogPrimitive.Title>
				<AlertDialogPrimitive.Description class="ridu-confirmation__description">
					{description}
				</AlertDialogPrimitive.Description>
			</header>
			<footer class="ridu-confirmation__footer">
				<AlertDialogPrimitive.Cancel
					bind:ref={cancelButton}
					type="button"
					class={buttonVariants({ variant: "outline" })}
					disabled={pending}
				>
					{cancelLabel}
				</AlertDialogPrimitive.Cancel>
				<AlertDialogPrimitive.Action
					type="button"
					class={buttonVariants()}
					data-destructive={destructive}
					disabled={disabled || pending}
					aria-busy={pending}
					onclick={submitConfirmation}
				>
					{confirmLabel}
				</AlertDialogPrimitive.Action>
			</footer>
		</AlertDialogPrimitive.Content>
	</AlertDialogPrimitive.Portal>
</AlertDialogPrimitive.Root>
