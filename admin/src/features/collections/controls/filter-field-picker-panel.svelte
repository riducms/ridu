<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import {
		CommandInput,
		CommandItem,
		CommandList,
		CommandRoot,
		CommandViewport,
	} from "@riducms/ui";
	import CheckIcon from "~icons/lucide/check";
	import ChevronLeftIcon from "~icons/lucide/chevron-left";
	import ChevronRightIcon from "~icons/lucide/chevron-right";
	import SearchIcon from "@admin/components/icons/search.svelte";
	import type {
		ListFilterEntry,
		ListFilterFields,
	} from "@admin/features/collections/list-filter-fields";
	import "@admin/features/collections/controls/filter-field-picker-panel.scss";

	let {
		fields,
		value,
		onSelect,
	}: {
		fields: ListFilterFields;
		value: string;
		onSelect: (path: string) => void;
	} = $props();

	const i18n = getAdminI18n();
	const kindLabels = {
		group: "collections:filterFieldGroup",
		array: "collections:filterFieldArray",
		blocks: "collections:filterFieldBlocks",
		block: "collections:filterFieldBlock",
	} as const;

	// The popover mounts this panel each time it opens, so its first level is captured once: beside
	// the chosen field, keeping its siblings and the selection in view.
	// svelte-ignore state_referenced_locally
	const initial = openingLevel(fields, value);
	let scope = $state(initial.scope);
	let query = $state("");
	let highlighted = $state(initial.highlighted);
	let input = $state<HTMLInputElement | null>(null);

	// A schema refresh can remove the open level; fall back to the collection root.
	const level = $derived(fields.level(scope) ?? fields.level(""));
	const levelLabel = $derived(
		level === undefined || level.path === ""
			? i18n.t("collections:allFields")
			: level.trail.join(" > ")
	);
	const search = $derived(
		query.trim() === "" || level === undefined ? undefined : fields.search(query, level.path)
	);
	const entries = $derived(search?.entries ?? level?.entries ?? []);

	$effect(() => {
		const field = input;
		if (field === null) return;
		// The popover positions and reveals its content in the next frame; focusing earlier is
		// ignored while the content is still hidden.
		const frame = requestAnimationFrame(() => field.focus());
		return () => cancelAnimationFrame(frame);
	});

	function openingLevel(index: ListFilterFields, path: string) {
		const chosen = index.resolve(path);
		const parent = parentPath(path);
		return {
			scope: chosen !== undefined && index.level(parent) !== undefined ? parent : "",
			highlighted: chosen === undefined ? "" : entryKey(chosen),
		};
	}

	function entryKey(entry: ListFilterEntry) {
		return `${entry.kind === "field" ? "field" : "open"}:${entry.path}`;
	}

	function parentPath(path: string) {
		return path.slice(0, Math.max(0, path.lastIndexOf(".")));
	}

	function enter(path: string) {
		scope = path;
		query = "";
		input?.focus();
	}

	function leave() {
		if (level === undefined || level.path === "") return;
		enter(parentPath(level.path));
	}

	function activate(entry: ListFilterEntry) {
		if (entry.kind !== "field") {
			enter(entry.path);
			return;
		}
		onSelect(entry.path);
	}

	function handleKeydown(event: KeyboardEvent) {
		const forward = i18n.direction === "rtl" ? "ArrowLeft" : "ArrowRight";
		const back = i18n.direction === "rtl" ? "ArrowRight" : "ArrowLeft";
		const target = event.target;
		const caretAtEnd =
			!(target instanceof HTMLInputElement) ||
			(target.selectionStart === target.value.length &&
				target.selectionEnd === target.value.length);

		if (event.key === forward && caretAtEnd) {
			const entry = entries.find((candidate) => entryKey(candidate) === highlighted);
			if (entry === undefined || entry.kind === "field") return;
			event.preventDefault();
			enter(entry.path);
		} else if ((event.key === back || event.key === "Backspace") && query === "") {
			if (level?.path === "") return;
			event.preventDefault();
			leave();
		}
	}
</script>

<CommandRoot
	bind:value={highlighted}
	shouldFilter={false}
	loop
	onkeydown={handleKeydown}
	class="ridu-filter-field-picker__command"
>
	<div class="ridu-filter-field-picker__search">
		<SearchIcon />
		<CommandInput
			bind:ref={input}
			bind:value={query}
			aria-label={i18n.t("collections:searchFilterFields")}
			placeholder={i18n.t("collections:searchFilterFields")}
		/>
	</div>
	{#if level !== undefined && level.path !== ""}
		<div class="ridu-filter-field-picker__level">
			<button
				type="button"
				class="ridu-filter-field-picker__back"
				aria-label={i18n.t("collections:filterFieldBack", {
					label:
						parentPath(level.path) === ""
							? i18n.t("collections:allFields")
							: level.trail.slice(0, -1).join(" > "),
				})}
				onclick={leave}
			>
				<ChevronLeftIcon />
			</button>
			<!-- The status below announces the level; this copy is visual. -->
			<span aria-hidden="true">{levelLabel}</span>
		</div>
	{/if}
	<p class="sr-only" role="status">{levelLabel}</p>
	<CommandList aria-label={levelLabel} class="ridu-filter-field-picker__list">
		<CommandViewport>
			{#each entries as entry (entryKey(entry))}
				{const prefix = $derived(
					search === undefined || level === undefined
						? []
						: entry.trail.slice(level.trail.length, -1)
				)}
				<CommandItem
					value={entryKey(entry)}
					onSelect={() => activate(entry)}
					class="ridu-filter-field-picker__option"
				>
					<span class="ridu-filter-field-picker__name">
						{#if prefix.length > 0}
							<span class="ridu-filter-field-picker__trail">{prefix.join(" > ")} &gt;</span>
							{" "}
						{/if}
						<span class="ridu-filter-field-picker__label">{entry.trail.at(-1)}</span>
					</span>
					{#if entry.kind !== "field"}
						<!-- The kind stays in the option's name, separated for assistive technology. -->
						{" "}
						<span class="ridu-filter-field-picker__kind">
							{i18n.t(kindLabels[entry.kind])}
						</span>
						<ChevronRightIcon class="ridu-filter-field-picker__open" />
					{:else if entry.path === value}
						<CheckIcon class="ridu-filter-field-picker__check" />
					{/if}
				</CommandItem>
			{/each}
		</CommandViewport>
	</CommandList>
	{#if entries.length === 0}
		<p class="ridu-filter-field-picker__message">{i18n.t("collections:noFieldsFound")}</p>
	{:else if search?.truncated}
		<p class="ridu-filter-field-picker__message">
			{i18n.t("collections:filterFieldMoreMatches", {
				count: i18n.formatNumber(entries.length),
			})}
		</p>
	{/if}
</CommandRoot>
