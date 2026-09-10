<script lang="ts">
	import { Combobox } from "bits-ui";
	import { tick } from "svelte";
	import ChevronDownIcon from "~icons/lucide/chevron-down";
	import ArrowRightIcon from "~icons/lucide/arrow-right";
	import ImageIcon from "~icons/lucide/image";
	import PencilIcon from "~icons/lucide/pencil";
	import PlusIcon from "~icons/lucide/plus";
	import XIcon from "~icons/lucide/x";
	import { fieldControlARIA, TooltipContent, TooltipRoot, TooltipTrigger } from "@riducms/ui";

	import { Skeleton } from "@admin/components/ui/skeleton";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	import {
		RelationshipQuickPickerController,
		type RelationshipQuickPickerTarget,
	} from "@admin/fields/relationship/relationship-quick-picker-controller.svelte";

	let {
		id,
		label,
		targets,
		selectedTarget,
		selectedID,
		selectedLabel,
		selectedInitials,
		placeholder,
		readOnly = false,
		blocked = false,
		locale,
		onPick,
		onRemove,
		onBrowse,
		onEdit,
		upload = false,
		invalid = false,
		hasDescription = false,
	}: {
		id: string;
		label: string;
		targets: readonly RelationshipQuickPickerTarget[];
		selectedTarget?: string;
		selectedID?: string;
		selectedLabel?: string;
		selectedInitials?: string;
		placeholder?: string;
		readOnly?: boolean;
		blocked?: boolean;
		locale?: string;
		onPick: (target: string, id: string) => void;
		onRemove: () => void;
		onBrowse: (target: string) => void;
		onEdit?: () => void;
		upload?: boolean;
		invalid?: boolean;
		hasDescription?: boolean;
	} = $props();

	const runtime = getAdminRuntime();
	const controller = new RelationshipQuickPickerController({
		runtime,
		get targets() {
			return targets;
		},
		get selectedTarget() {
			return selectedTarget;
		},
		get selectedID() {
			return selectedID;
		},
		get locale() {
			return locale;
		},
		onPick: (target, value) => onPick(target, value),
		onRemove: () => onRemove(),
		onBrowse: (target) => onBrowse(target),
	});
	const { comboboxItems, groups, open, query, selectedValue } = $derived(controller);
	const {
		browseAll,
		changeOpen: setOpen,
		documentLabel,
		initials,
		pick,
		remove: removeSelection,
		setQuery,
		toggleOpen,
	} = controller;
	const selectedCollection = $derived(
		targets.find((target) => target.collection.slug === selectedTarget)?.collection ??
			targets[0]?.collection
	);
	const searchLabel = $derived(
		targets.length === 1 ? (selectedCollection?.labels.plural ?? label) : label
	);
	const controlARIA = $derived(fieldControlARIA(id, hasDescription, invalid));
	const editingBlocked = $derived(readOnly || blocked);
	let triggerElement = $state<HTMLButtonElement | null>(null);
	let searchInput = $state<HTMLInputElement | null>(null);

	$effect(() => {
		if (editingBlocked) setOpen(false);
	});

	function changePickerOpen(next: boolean) {
		if (editingBlocked && next) return;
		setOpen(next);
	}

	async function togglePicker() {
		if (editingBlocked) return;
		if (!toggleOpen()) return;
		await tick();
		searchInput?.focus();
	}

	function beginSearch(event: Event) {
		if (editingBlocked) return;
		setQuery((event.currentTarget as HTMLInputElement).value);
	}

	function choose(value: string) {
		if (editingBlocked) return;
		pick(value);
	}

	function remove(event: MouseEvent) {
		event.stopPropagation();
		if (editingBlocked) return;
		removeSelection();
	}

	function browse(event: MouseEvent, target?: string) {
		event.stopPropagation();
		if (blocked) return;
		browseAll(target);
	}

	function edit(event: MouseEvent) {
		event.stopPropagation();
		if (blocked) return;
		onEdit?.();
	}
</script>

<Combobox.Root
	type="single"
	value={selectedValue}
	inputValue={query}
	items={comboboxItems}
	{open}
	onOpenChange={changePickerOpen}
	onValueChange={choose}
	allowDeselect={false}
	loop
>
	<div
		class="flex h-10.5 min-w-0 rounded-[3px] border border-control-border bg-control transition-[background-color,border-color] duration-150 hover:bg-background focus-within:border-primary/65 aria-invalid:!border-destructive/65"
		aria-invalid={invalid}
	>
		<button
			bind:this={triggerElement}
			{id}
			type="button"
			class="flex min-w-0 flex-1 items-center gap-2 rounded-s-[3px] px-2.75 text-start outline-none disabled:cursor-not-allowed disabled:opacity-45"
			disabled={editingBlocked}
			aria-label={label}
			{...controlARIA}
			aria-haspopup="listbox"
			aria-expanded={open}
			onclick={togglePicker}
		>
			{#if selectedID !== undefined}
				<span
					class="font-mono grid size-[19px] shrink-0 place-items-center rounded-full bg-sidebar-accent text-[8px] font-semibold tracking-[0.04em] text-sidebar-accent-foreground"
					aria-hidden="true"
				>
					{selectedInitials}
				</span>
				<span class="min-w-0 flex-1 truncate text-[13px] text-foreground-strong">
					{selectedLabel}
				</span>
			{:else}
				{#if upload}<ImageIcon class="size-3.5 shrink-0 text-foreground-sub" />{/if}
				<span class="min-w-0 flex-1 truncate text-[13px] text-foreground-placeholder">
					{placeholder ??
						runtime.i18n.t(upload ? "fields:choose" : "fields:select", {
							label: (
								selectedCollection?.labels.singular ??
								runtime.i18n.t(upload ? "fields:asset" : "fields:document")
							).toLocaleLowerCase(runtime.i18n.language),
						})}
				</span>
			{/if}
			<ChevronDownIcon class="size-3.5 shrink-0 text-foreground-sub" aria-hidden="true" />
		</button>

		{#if selectedID !== undefined && onEdit !== undefined}
			<TooltipRoot>
				<TooltipTrigger>
					{#snippet child({ props })}
						<button
							{...props}
							type="button"
							class="grid w-9 shrink-0 place-items-center border-s border-control-border text-foreground-sub outline-none transition-colors hover:bg-background hover:text-foreground focus-visible:text-primary disabled:cursor-not-allowed disabled:opacity-45"
							disabled={blocked}
							onpointerdown={(event) => event.preventDefault()}
							onclick={edit}
							aria-label={runtime.i18n.t("fields:edit", { label: selectedLabel ?? "" })}
						>
							<PencilIcon class="size-3.25" />
						</button>
					{/snippet}
				</TooltipTrigger>
				<TooltipContent>{runtime.i18n.t("fields:editRelationship")}</TooltipContent>
			</TooltipRoot>
		{/if}

		{#if selectedID !== undefined && !readOnly}
			<TooltipRoot>
				<TooltipTrigger>
					{#snippet child({ props })}
						<button
							{...props}
							type="button"
							disabled={editingBlocked}
							class="grid w-9 shrink-0 place-items-center border-s border-control-border text-foreground-sub outline-none transition-colors hover:bg-destructive/7 hover:text-destructive focus-visible:text-destructive"
							onpointerdown={(event) => event.preventDefault()}
							onclick={remove}
							aria-label={runtime.i18n.t("fields:remove", { label: selectedLabel ?? "" })}
						>
							<XIcon class="size-3.5" />
						</button>
					{/snippet}
				</TooltipTrigger>
				<TooltipContent>{runtime.i18n.t("fields:removeRelationship")}</TooltipContent>
			</TooltipRoot>
		{/if}

		<TooltipRoot>
			<TooltipTrigger>
				{#snippet child({ props })}
					<button
						{...props}
						type="button"
						disabled={blocked}
						class="grid w-9 shrink-0 place-items-center rounded-e-[3px] border-s border-control-border text-foreground-sub outline-none transition-colors hover:bg-background hover:text-foreground focus-visible:text-primary"
						onpointerdown={(event) => event.preventDefault()}
						onclick={(event) => browse(event, selectedCollection?.slug)}
						aria-label={runtime.i18n.t("fields:browse", {
							label: (
								selectedCollection?.labels.plural ?? runtime.i18n.t("fields:relationships")
							).toLocaleLowerCase(runtime.i18n.language),
						})}
					>
						<PlusIcon class="size-3.5" />
					</button>
				{/snippet}
			</TooltipTrigger>
			<TooltipContent>{runtime.i18n.t("fields:addRelationship")}</TooltipContent>
		</TooltipRoot>
	</div>

	<Combobox.Portal>
		<Combobox.Content
			customAnchor={triggerElement}
			align="start"
			sideOffset={4}
			class="ridu-popover-enter z-50 flex flex-col overflow-hidden rounded-[4px] border border-control-border bg-popover p-1.5 text-popover-foreground shadow-lg outline-none"
			style="width: var(--bits-combobox-anchor-width);"
		>
			<Combobox.Input
				bind:ref={searchInput}
				disabled={editingBlocked}
				aria-label={runtime.i18n.t("fields:searchLabel", { label: searchLabel })}
				placeholder={runtime.i18n.t("fields:search", {
					label: searchLabel.toLocaleLowerCase(runtime.i18n.language),
				})}
				class="mb-1.25 h-[29px] w-full rounded-[3px] border border-control-border bg-control px-[9px] text-[12.5px] text-foreground caret-primary outline-none placeholder:text-foreground-faint focus-visible:border-primary/55"
				oninput={beginSearch}
			/>

			<Combobox.Viewport class="grid max-h-64 min-h-8 overflow-y-auto">
				{#each groups as group (group.collection.id)}
					<div role="group" aria-label={group.collection.labels.plural}>
						{#if groups.length > 1}
							<p
								class="px-2 pt-2 pb-1 text-[10px] font-medium tracking-[0.08em] text-foreground-faint uppercase"
							>
								{group.collection.labels.plural}
							</p>
						{/if}
						{#if group.status === "initial" || group.status === "loading"}
							<div
								class="grid gap-1 p-0.5"
								aria-label={runtime.i18n.t("fields:loading", {
									label: group.collection.labels.plural,
								})}
							>
								<Skeleton class="h-[31px]" />
								<Skeleton class="h-[31px]" />
							</div>
						{:else if group.error !== undefined}
							<p class="px-3 py-2 text-[12px] leading-4 text-destructive" role="alert">
								{group.error}
							</p>
						{:else if group.docs.length === 0}
							<p class="px-3 py-2 text-[12px] text-foreground-faint">
								{runtime.i18n.t("fields:noMatch", {
									label: group.collection.labels.plural.toLocaleLowerCase(runtime.i18n.language),
								})}
							</p>
						{:else}
							{#each group.docs as document (document.id)}
								{const optionLabel = documentLabel(group.collection, document)}
								<Combobox.Item
									value={JSON.stringify([group.collection.slug, document.id])}
									label={optionLabel}
									disabled={editingBlocked}
									class="flex w-full cursor-default items-center gap-[9px] rounded-[3px] px-2 py-1.5 text-start text-[12.5px] text-foreground-muted outline-none select-none data-[highlighted]:bg-control data-[selected]:text-foreground-strong"
								>
									{#snippet children({ selected })}
										<span
											class="font-mono grid size-[19px] shrink-0 place-items-center rounded-full bg-sidebar-accent text-[8px] font-semibold text-sidebar-accent-foreground"
											aria-hidden="true"
										>
											{initials(optionLabel)}
										</span>
										<span class="min-w-0 flex-1 truncate">{optionLabel}</span>
										<span class="font-mono text-[9.5px] text-primary" aria-hidden="true">
											{selected ? "✓" : ""}
										</span>
									{/snippet}
								</Combobox.Item>
							{/each}
						{/if}
						<button
							type="button"
							disabled={editingBlocked}
							class="mt-0.5 flex w-full items-center gap-1 px-2 py-1.5 text-start text-[11.5px] text-primary outline-none hover:text-primary-hover focus-visible:text-primary-hover"
							onpointerdown={(event) => event.preventDefault()}
							onclick={(event) => browse(event, group.collection.slug)}
						>
							{runtime.i18n.t("fields:browseAll", {
								label: group.collection.labels.plural.toLocaleLowerCase(runtime.i18n.language),
							})}
							<ArrowRightIcon class="size-3 rtl:rotate-180" aria-hidden="true" />
						</button>
					</div>
				{/each}
			</Combobox.Viewport>
		</Combobox.Content>
	</Combobox.Portal>
</Combobox.Root>
