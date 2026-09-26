<script lang="ts">
	import type { Snippet } from "svelte";
	import {
		Combobox,
		ComboboxInput,
		ComboboxTrigger,
		ComboboxPortal,
		ComboboxViewport,
		ComboboxContent,
		ComboboxItem,
	} from "@admin/components/ui/combobox";
	import ChevronDownIcon from "~icons/lucide/chevron-down";
	import ArrowRightIcon from "~icons/lucide/arrow-right";
	import PencilIcon from "~icons/lucide/pencil";
	import PlusIcon from "~icons/lucide/plus";
	import SearchIcon from "~icons/lucide/search";
	import XIcon from "~icons/lucide/x";
	import { fieldControlARIA } from "@riducms/ui";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import {
		RelationshipQuickPickerController,
		type RelationshipQuickPickerTarget,
	} from "@admin/fields/relationship/relationship-quick-picker-controller.svelte";
	import "@admin/components/ui/combobox/combobox.scss";

	let {
		id,
		label,
		targets,
		selectedTarget,
		selectedID,
		selectedLabel,
		placeholder,
		readOnly = false,
		blocked = false,
		locale,
		onPick,
		onRemove,
		onBrowse,
		onCreate,
		onEdit,
		upload = false,
		invalid = false,
		hasDescription = false,
		multiple = false,
		values = [],
		onValuesChange,
		selections,
	}: {
		id: string;
		label: string;
		targets: readonly RelationshipQuickPickerTarget[];
		selectedTarget?: string;
		selectedID?: string;
		selectedLabel?: string;
		placeholder?: string;
		readOnly?: boolean;
		blocked?: boolean;
		locale?: string;
		onPick: (target: string, id: string) => void;
		onRemove: () => void;
		onBrowse: (target: string) => void;
		onCreate?: (target: string) => void;
		onEdit?: () => void;
		upload?: boolean;
		invalid?: boolean;
		hasDescription?: boolean;
		multiple?: boolean;
		values?: string[];
		onValuesChange?: (values: string[]) => void;
		selections?: Snippet;
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
			return multiple ? undefined : selectedID;
		},
		get excludedValues() {
			return multiple ? values : [];
		},
		get locale() {
			return locale;
		},
		onPick: (target, value) => onPick(target, value),
		onRemove: () => onRemove(),
		onBrowse: (target) => onBrowse(target),
	});
	const { comboboxItems, groups, open, query, selectedValue } = $derived(controller);
	const { browseAll, changeOpen, documentLabel, pick, remove, setQuery } = controller;
	let anchor = $state<HTMLDivElement | null>(null);

	const selectedCollection = $derived(
		targets.find((target) => target.collection.slug === selectedTarget)?.collection ??
			targets[0]?.collection
	);
	const editingBlocked = $derived(readOnly || blocked);
	const canCreate = $derived(
		!editingBlocked &&
			onCreate !== undefined &&
			selectedCollection !== undefined &&
			runtime.collectionOperations[selectedCollection.slug]?.create === true
	);
	const controlARIA = $derived(fieldControlARIA(id, hasDescription, invalid));
	const inputValue = $derived(open || multiple ? query : (selectedLabel ?? ""));
	const inputPlaceholder = $derived(
		placeholder ??
			runtime.i18n.t(upload ? "fields:choose" : "fields:select", {
				label: (
					selectedCollection?.labels.singular ??
					runtime.i18n.t(upload ? "fields:asset" : "fields:document")
				).toLocaleLowerCase(runtime.i18n.language),
			})
	);

	const selection = $derived(
		multiple
			? {
					type: "multiple" as const,
					value: values,
					onValueChange: (next: string[]) => {
						if (!editingBlocked) onValuesChange?.(next);
						setQuery("");
					},
				}
			: { type: "single" as const, value: selectedValue, onValueChange: pick }
	);

	$effect(() => {
		if (editingBlocked) changeOpen(false);
	});
</script>

<Combobox
	{...selection}
	{inputValue}
	items={comboboxItems}
	bind:open={() => open, changeOpen}
	disabled={editingBlocked}
	allowDeselect={false}
	loop
>
	<div
		bind:this={anchor}
		class="ridu-combobox"
		aria-invalid={invalid}
		data-disabled={editingBlocked}
	>
		<div class="ridu-combobox-value">
			{@render selections?.()}
			<ComboboxInput
				{id}
				class="ridu-combobox-input"
				autocomplete="off"
				autocorrect="off"
				autocapitalize="none"
				spellcheck={false}
				aria-label={label}
				{...controlARIA}
				placeholder={selectedID || values.length > 0 ? "" : inputPlaceholder}
				clearOnDeselect
				onpointerdown={() => changeOpen(true)}
				oninput={(event) => {
					setQuery(event.currentTarget.value);
					changeOpen(true);
				}}
			>
				{#snippet child({ props })}
					<!-- Keep the visible search controlled: Bits writes the last picked label internally. -->
					<input {...props} value={inputValue} />
				{/snippet}
			</ComboboxInput>
			{#if !multiple && selectedID && !query}
				<div class="ridu-combobox-selection">
					<span>{selectedLabel}</span>
					{#if onEdit}
						<button
							type="button"
							class="ridu-combobox-icon"
							disabled={blocked}
							aria-label={runtime.i18n.t("fields:edit", { label: selectedLabel ?? "" })}
							onpointerdown={(event) => event.preventDefault()}
							onclick={onEdit}
						>
							<PencilIcon />
						</button>
					{/if}
				</div>
			{/if}
		</div>
		{#if (selectedID || values.length > 0) && !readOnly}
			<button
				type="button"
				class="ridu-combobox-icon"
				disabled={editingBlocked}
				aria-label={multiple
					? runtime.i18n.t("general:clearSelection")
					: runtime.i18n.t("fields:remove", { label: selectedLabel ?? "" })}
				onclick={() => (multiple ? onValuesChange?.([]) : remove())}
			>
				<XIcon />
			</button>
		{/if}
		<ComboboxTrigger
			type="button"
			class="ridu-combobox-icon ridu-combobox-trigger"
			aria-label={inputPlaceholder}
		>
			<ChevronDownIcon />
		</ComboboxTrigger>
		{#if canCreate && selectedCollection}
			<button
				type="button"
				class="ridu-combobox-icon ridu-combobox-add"
				aria-label={runtime.i18n.t("fields:add", { label: selectedCollection.labels.singular })}
				onclick={() => {
					changeOpen(false);
					onCreate?.(selectedCollection!.slug);
				}}
			>
				<PlusIcon />
			</button>
		{:else if upload || readOnly}
			<button
				type="button"
				class="ridu-combobox-icon ridu-combobox-add"
				disabled={blocked}
				aria-label={runtime.i18n.t("fields:browse", {
					label: (readOnly
						? (selectedCollection?.labels.plural ?? label)
						: label
					).toLocaleLowerCase(runtime.i18n.language),
				})}
				onclick={() => browseAll(selectedCollection?.slug)}
			>
				{#if readOnly}
					<SearchIcon />
				{:else}
					<PlusIcon />
				{/if}
			</button>
		{/if}
	</div>

	<ComboboxPortal>
		<ComboboxContent customAnchor={anchor} align="start" sideOffset={0} class="ridu-combobox-popup">
			<ComboboxViewport>
				{#each groups as group (group.collection.id)}
					<div role="group" aria-label={group.collection.labels.plural}>
						{#if groups.length > 1}
							<p class="ridu-combobox-group">
								{group.collection.labels.plural}
							</p>
						{/if}
						{#if group.status === "initial" || group.status === "loading"}
							<p
								class="ridu-combobox-message"
								role="status"
								data-ridu-loading-surface="relationship-options"
							>
								{runtime.i18n.t("fields:loading", { label: group.collection.labels.plural })}
							</p>
						{:else if group.error}
							<p class="ridu-combobox-message" role="alert">{group.error}</p>
						{:else if !group.docs.length && query.trim()}
							<p class="ridu-combobox-message">
								{runtime.i18n.t("fields:noMatch", {
									label: group.collection.labels.plural.toLocaleLowerCase(runtime.i18n.language),
								})}
							</p>
						{:else}
							{#each group.docs as document (document.id)}
								{const optionLabel = documentLabel(group.collection, document)}
								<ComboboxItem
									value={JSON.stringify([group.collection.slug, document.id])}
									label={optionLabel}
									disabled={editingBlocked}
									class="ridu-combobox-option"
								>
									{optionLabel}
								</ComboboxItem>
							{/each}
						{/if}
						<button
							type="button"
							class="ridu-combobox-browse"
							disabled={blocked}
							onpointerdown={(event) => event.preventDefault()}
							onclick={() => browseAll(group.collection.slug)}
						>
							{runtime.i18n.t("fields:browseAll", {
								label: group.collection.labels.plural.toLocaleLowerCase(runtime.i18n.language),
							})}<ArrowRightIcon />
						</button>
					</div>
				{/each}
			</ComboboxViewport>
		</ComboboxContent>
	</ComboboxPortal>
</Combobox>
