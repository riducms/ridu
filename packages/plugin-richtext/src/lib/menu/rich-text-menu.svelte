<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import {
		CommandGroup,
		CommandGroupHeading,
		CommandGroupItems,
		CommandItem,
		CommandList,
	} from "@riducms/ui";

	import type { RichTextMenuOption } from "#lib/menu/rich-text-options.js";
	import ToolbarIcon from "#lib/toolbar/toolbar-icon.svelte";
	import "#lib/menu/rich-text-menu.scss";

	let {
		options,
		selectedIndex = null,
		onHighlight = () => undefined,
		onSelect,
		command = false,
		idPrefix = "richtext-menu-item",
	}: {
		options: readonly RichTextMenuOption[];
		selectedIndex?: number | null;
		onHighlight?: (index: number) => void;
		onSelect: (option: RichTextMenuOption) => void;
		command?: boolean;
		idPrefix?: string;
	} = $props();
	const i18n = getAdminI18n();
	const groups = $derived(
		[...new Set(options.map((option) => option.group))].map((name) => ({
			name,
			items: options
				.map((option, index) => ({ option, index }))
				.filter(({ option }) => option.group === name),
		}))
	);
</script>

{#snippet optionContent(option: RichTextMenuOption)}
	<ToolbarIcon name={option.icon} />
	<span class="ridu-richtext-menu__label">{option.label}</span>
{/snippet}

{#if command}
	<CommandList class="ridu-richtext-menu">
		{#each groups as group (group.name)}
			<CommandGroup class="ridu-richtext-menu__group">
				<CommandGroupHeading class="ridu-richtext-menu__heading">
					{i18n.t(`plugin.richtext:menu.${group.name}`)}
				</CommandGroupHeading>
				<CommandGroupItems class="ridu-richtext-menu__items">
					{#each group.items as { option, index } (option.key)}
						<CommandItem
							class="ridu-richtext-menu__option"
							id={`${idPrefix}-${index}`}
							value={option.key}
							keywords={[option.label, ...option.keywords]}
							onSelect={() => onSelect(option)}
						>
							{@render optionContent(option)}
						</CommandItem>
					{/each}
				</CommandGroupItems>
			</CommandGroup>
		{/each}
	</CommandList>
{:else}
	<div class="ridu-richtext-menu" data-richtext-menu-surface>
		{#each groups as group (group.name)}
			<div class="ridu-richtext-menu__group" role="presentation">
				<p class="ridu-richtext-menu__heading">{i18n.t(`plugin.richtext:menu.${group.name}`)}</p>
				<ul class="ridu-richtext-menu__items" role="presentation">
					{#each group.items as { option, index } (option.key)}
						<li
							id={`${idPrefix}-${index}`}
							class={["ridu-richtext-menu__option", selectedIndex === index && "is-selected"]}
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
		{/each}
	</div>
{/if}
