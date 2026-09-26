<script lang="ts">
	import { Dialog } from "bits-ui";
	import { getAdminI18n } from "@riducms/plugin";
	import type { SchemaBlockType } from "@riducms/protocol";
	import { Input } from "@riducms/ui";
	import { provideDrawerDepth } from "@admin/components/ui/drawer/drawer-depth";
	import SearchIcon from "~icons/lucide/search";
	import XIcon from "~icons/lucide/x";
	import placeholder from "@admin/fields/nested/block-placeholder.svg";
	import "@admin/fields/nested/block-picker.scss";

	let {
		open,
		label,
		blocks,
		onOpenChange,
		onSelect,
	}: {
		open: boolean;
		label: string;
		blocks: readonly SchemaBlockType[];
		onOpenChange: (open: boolean) => void;
		onSelect: (slug: string) => void;
	} = $props();

	const i18n = getAdminI18n();
	const depth = provideDrawerDepth();
	let query = $state("");
	const visible = $derived(
		blocks.filter((block) =>
			block.labels.singular
				.toLocaleLowerCase(i18n.language)
				.includes(query.trim().toLocaleLowerCase(i18n.language))
		)
	);
</script>

<Dialog.Root {open} {onOpenChange}>
	<Dialog.Portal>
		<Dialog.Overlay class="ridu-block-picker-overlay" />
		<Dialog.Content
			class="ridu-block-picker"
			style={`--drawer-depth: ${depth}`}
			dir={i18n.direction}
		>
			<header class="ridu-block-picker__header">
				<Dialog.Title class="ridu-block-picker__title">
					{i18n.t("fields:add", { label })}
				</Dialog.Title>
				<Dialog.Close class="ridu-block-picker__close" aria-label={i18n.t("general:close")}>
					<XIcon />
				</Dialog.Close>
			</header>
			<Dialog.Description class="ridu-block-picker__description">
				{i18n.t("fields:chooseBlockFor", { label })}
			</Dialog.Description>
			<div class="ridu-block-picker__search">
				<SearchIcon />
				<Input
					placeholder={i18n.t("fields:searchBlock")}
					aria-label={i18n.t("fields:searchBlock")}
					bind:value={query}
				/>
			</div>
			<div class="ridu-block-picker__cards">
				{#each visible as block (block.slug)}
					<button
						type="button"
						class="ridu-block-picker__card"
						onclick={() => onSelect(block.slug)}
					>
						<img src={placeholder} alt="" />
						<span>{block.labels.singular}</span>
					</button>
				{:else}
					<p class="ridu-block-picker__empty">{i18n.t("fields:noBlocks")}</p>
				{/each}
			</div>
		</Dialog.Content>
	</Dialog.Portal>
</Dialog.Root>
