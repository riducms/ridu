<script lang="ts">
	import { AlertDialog as AlertDialogPrimitive } from "bits-ui";

	import { buttonVariants } from "@ui/button/button.svelte";

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
	let cancelButton = $state<HTMLButtonElement | null>(null);

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
			class="fixed inset-0 z-50 bg-backdrop backdrop-blur-[2px]"
		/>
		<AlertDialogPrimitive.Content
			data-slot="confirmation-dialog-content"
			class="ridu-dialog-enter fixed top-1/2 left-1/2 z-50 grid w-full max-w-[calc(100%-2rem)] -translate-x-1/2 -translate-y-1/2 gap-6 rounded-[4px] border border-control-border bg-popover p-6.5 text-[14.5px] text-popover-foreground shadow-[var(--shadow-dialog)] outline-none sm:max-w-[460px]"
			onOpenAutoFocus={focusCancel}
			onEscapeKeydown={(event) => pending && event.preventDefault()}
		>
			<header class="grid gap-2">
				<AlertDialogPrimitive.Title
					class="font-serif text-[28px] leading-tight font-normal text-foreground"
				>
					{title}
				</AlertDialogPrimitive.Title>
				<AlertDialogPrimitive.Description class="text-[13px] leading-5 text-foreground-muted">
					{description}
				</AlertDialogPrimitive.Description>
			</header>
			<footer class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
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
					class={buttonVariants({ variant: destructive ? "destructive" : "default" })}
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
