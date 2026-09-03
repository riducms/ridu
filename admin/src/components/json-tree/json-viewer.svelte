<script lang="ts">
	import { onDestroy } from "svelte";
	import CheckIcon from "~icons/lucide/check";
	import CopyIcon from "~icons/lucide/copy";
	import Maximize2Icon from "~icons/lucide/maximize-2";
	import Minimize2Icon from "~icons/lucide/minimize-2";
	import { getAdminI18n } from "@riducms/plugin";

	import JsonTreeNode from "@admin/components/json-tree/json-tree-node.svelte";
	import { Button, buttonVariants } from "@admin/components/ui/button";
	import {
		Dialog,
		DialogClose,
		DialogContent,
		DialogDescription,
		DialogTitle,
		DialogTrigger,
	} from "@admin/components/ui/dialog";

	let { value, class: className = "" }: { value: unknown; class?: string } = $props();
	const i18n = getAdminI18n();
	let copied = $state(false);
	let copyFailed = $state(false);
	let copyResetTimer: ReturnType<typeof setTimeout> | undefined;

	onDestroy(() => clearTimeout(copyResetTimer));

	async function copy() {
		try {
			await navigator.clipboard.writeText(JSON.stringify(value, null, 2));
			copied = true;
			copyFailed = false;
		} catch {
			copyFailed = true;
		}
		clearTimeout(copyResetTimer);
		copyResetTimer = window.setTimeout(() => {
			copied = false;
			copyFailed = false;
		}, 1_500);
	}
</script>

{#snippet viewer(expanded: boolean)}
	<div
		class={[
			"flex min-h-0 flex-col overflow-hidden rounded-[4px] border border-control-border bg-control",
			!expanded && className,
			expanded && "h-full rounded-none border-0 bg-background",
		]}
	>
		<div class="flex items-center justify-between border-b border-control-border px-3 py-2">
			<span class="font-mono text-[9.5px] tracking-[0.12em] text-foreground-faint uppercase">
				JSON
			</span>
			<div class="flex items-center gap-1">
				{#if expanded}
					<DialogClose
						class={buttonVariants({ variant: "ghost", size: "icon-xs" })}
						aria-label={i18n.t("general:exitExpandedJSONView")}
						title={i18n.t("general:exitExpandedJSONView")}
					>
						<Minimize2Icon class="size-3.5" />
					</DialogClose>
				{:else}
					<DialogTrigger
						class={buttonVariants({ variant: "ghost", size: "icon-xs" })}
						aria-label={i18n.t("general:expandJSONView")}
						title={i18n.t("general:expandJSONView")}
					>
						<Maximize2Icon class="size-3.5" />
					</DialogTrigger>
				{/if}
				<Button
					variant="ghost"
					size="xs"
					onclick={copy}
					aria-label={i18n.t("general:copyJSON")}
					tooltip={copied
						? i18n.t("general:copied")
						: copyFailed
							? i18n.t("general:copyUnavailable")
							: i18n.t("general:copyJSON")}
				>
					{#if copied}<CheckIcon class="size-3" /> {i18n.t("general:copied")}{:else}<CopyIcon
							class="size-3"
						/>
						{copyFailed ? i18n.t("general:copyUnavailable") : i18n.t("general:copy")}{/if}
				</Button>
			</div>
		</div>
		<section
			class="min-h-72 flex-1 overflow-auto p-4 font-mono text-[11.5px] leading-6"
			aria-label={i18n.t("general:jsonData")}
		>
			<JsonTreeNode {value} />
		</section>
	</div>
{/snippet}

<Dialog>
	{@render viewer(false)}
	<DialogContent
		showCloseButton={false}
		class="inset-3 top-3 left-3 h-auto w-auto max-w-none translate-x-0 translate-y-0 gap-0 overflow-hidden p-0 sm:max-w-none"
	>
		<DialogTitle class="sr-only">{i18n.t("general:expandedJSONResponse")}</DialogTitle>
		<DialogDescription class="sr-only">
			{i18n.t("general:expandedJSONDescription")}
		</DialogDescription>
		{@render viewer(true)}
	</DialogContent>
</Dialog>
