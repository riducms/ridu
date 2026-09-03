<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import {
		CommandGroup,
		CommandGroupHeading,
		CommandGroupItems,
		CommandItem,
		CommandList,
	} from "@riducms/ui";

	import type { RichTextMenuOption } from "@plugin-richtext/menu/rich-text-options";

	let {
		options,
		selectedIndex = null,
		onHighlight = () => undefined,
		onSelect,
		embedded = false,
		command = false,
		idPrefix = "richtext-menu-item",
	}: {
		options: readonly RichTextMenuOption[];
		selectedIndex?: number | null;
		onHighlight?: (index: number) => void;
		onSelect: (option: RichTextMenuOption) => void;
		embedded?: boolean;
		command?: boolean;
		idPrefix?: string;
	} = $props();
	const i18n = getAdminI18n();
	const menuClasses = $derived([
		"ridu-richtext-menu max-h-80 w-[min(19rem,calc(100vw-2rem))] overflow-y-auto rounded-[4px] border border-control-border bg-popover p-[0.35rem] shadow-[var(--shadow-popover)] animate-[ridu-richtext-menu-in_150ms_ease] motion-reduce:animate-none",
		embedded && "max-h-68 w-full rounded-0 border-0 shadow-none animate-none",
	]);
	const optionClasses =
		"grid min-h-10 cursor-pointer grid-cols-[1.85rem_minmax(0,1fr)_auto] items-center gap-[0.6rem] rounded-[3px] px-[0.55rem] py-[0.35rem] text-foreground-muted outline-none hover:bg-control-hover hover:text-foreground data-[selected]:bg-control-hover data-[selected]:text-foreground";
</script>

{#snippet optionContent(option: RichTextMenuOption)}
	<span
		class="grid size-7 place-items-center rounded-[3px] border border-control-border bg-control font-mono text-[10px] text-foreground-muted"
		aria-hidden="true"
	>
		{option.glyph}
	</span>
	<span class="grid min-w-0 gap-[0.1rem]">
		<strong class="text-[13px] font-medium">{option.label}</strong>
		<small class="text-[11px] text-foreground-faint">{option.description}</small>
	</span>
	<span class="font-mono text-[11px] text-foreground-faint">{option.keyHint}</span>
{/snippet}

{#if command}
	<CommandList class={menuClasses}>
		<CommandGroup>
			<CommandGroupHeading
				class="px-[0.6rem] pt-[0.45rem] pb-[0.35rem] font-mono text-[10px] tracking-[0.13em] text-foreground-faint uppercase"
			>
				{i18n.t("plugin.richtext:editor.insertBlock")}
			</CommandGroupHeading>
			<CommandGroupItems>
				{#each options as option, index (option.key)}
					<CommandItem
						class={optionClasses}
						id={`${idPrefix}-${index}`}
						value={option.label}
						keywords={[...option.keywords]}
						onSelect={() => onSelect(option)}
					>
						{@render optionContent(option)}
					</CommandItem>
				{/each}
			</CommandGroupItems>
		</CommandGroup>
	</CommandList>
{:else}
	<div class={menuClasses} data-richtext-menu-surface>
		<p
			class="px-[0.6rem] pt-[0.45rem] pb-[0.35rem] font-mono text-[10px] tracking-[0.13em] text-foreground-faint uppercase"
		>
			{i18n.t("plugin.richtext:editor.insertBlock")}
		</p>
		<ul class="grid list-none gap-[0.1rem] p-0" role="presentation">
			{#each options as option, index (option.key)}
				<li
					id={`${idPrefix}-${index}`}
					class={[optionClasses, selectedIndex === index && "bg-control-hover text-foreground"]}
					role="option"
					aria-selected={selectedIndex === index}
					tabindex="-1"
					{@attach option.attach}
					onmouseenter={() => onHighlight(index)}
					onmousedown={(event) => event.preventDefault()}
					onclick={() => onSelect(option)}
					onkeydown={(event) => {
						if (event.key === "Enter" || event.key === " ") {
							event.preventDefault();
							onSelect(option);
						}
					}}
				>
					{@render optionContent(option)}
				</li>
			{/each}
		</ul>
	</div>
{/if}
