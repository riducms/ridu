<script lang="ts">
	import type { AdminI18n } from "@riducms/plugin";
	import type { SchemaField } from "@riducms/protocol";
	import ListFilterIcon from "~icons/lucide/list-filter";
	import XIcon from "~icons/lucide/x";

	import { Button, buttonVariants } from "@admin/components/ui/button";
	import { DateValueControl } from "@admin/components/ui/date-value-control";
	import { Input } from "@admin/components/ui/input";
	import { Popover, PopoverContent, PopoverTrigger } from "@admin/components/ui/popover";
	import { Select, SelectContent, SelectItem, SelectTrigger } from "@admin/components/ui/select";
	import {
		filterOperatorLabel,
		filterOperatorsFor,
		type ListFilter,
		type ListFilterOperator,
	} from "@admin/features/collections/list-workspace";

	let {
		i18n,
		fields,
		filters,
		canReadField,
		onFiltersChange,
	}: {
		i18n: AdminI18n;
		fields: readonly SchemaField[];
		filters: readonly ListFilter[];
		canReadField: (path: string) => boolean;
		onFiltersChange: (filters: readonly ListFilter[]) => void;
	} = $props();

	let fieldName = $state("");
	let operator = $state<ListFilterOperator>("equals");
	let value = $state("");
	const readableFields = $derived(fields.filter((candidate) => canReadField(candidate.path)));
	const field = $derived(readableFields.find((candidate) => candidate.path === fieldName));
	const selectedFieldLabel = $derived(field?.admin.label ?? i18n.t("collections:chooseField"));
	const selectedOperatorLabel = $derived(
		field === undefined ? i18n.t("collections:chooseOperator") : filterOperatorLabel(operator, i18n)
	);
	const booleanValueOptions = $derived(
		operator === "exists"
			? [
					{ value: "true", label: i18n.t("collections:hasValue") },
					{ value: "false", label: i18n.t("collections:isEmpty") },
				]
			: field?.type === "checkbox"
				? [
						{ value: "true", label: i18n.t("collections:enabled") },
						{ value: "false", label: i18n.t("collections:disabled") },
					]
				: []
	);

	$effect(() => {
		if (fieldName !== "" && !readableFields.some((candidate) => candidate.path === fieldName)) {
			fieldName = "";
			operator = "equals";
			value = "";
		}
	});

	function selectField(name: string) {
		fieldName = name;
		const selected = readableFields.find((candidate) => candidate.path === name);
		operator = selected === undefined ? "equals" : (filterOperatorsFor(selected)[0] ?? "equals");
		value = "";
	}

	function selectOperator(next: string) {
		operator = next as ListFilterOperator;
	}

	function addFilter() {
		if (field === undefined) return;
		const filterValue = operator === "exists" ? value || "true" : value;
		if (operator !== "exists" && filterValue === "") return;
		onFiltersChange([...filters, { field: field.path, operator, value: filterValue }]);
		value = "";
	}

	function removeFilter(index: number) {
		onFiltersChange(filters.filter((_, candidate) => candidate !== index));
	}
</script>

<Popover>
	<PopoverTrigger
		class={buttonVariants({
			variant: "outline",
			class: ["h-[31px] gap-1.75 px-3", filters.length > 0 && "border-primary/30 text-primary"],
		})}
	>
		<ListFilterIcon class="size-3 text-foreground-faint" />
		{i18n.t("collections:filters")}
		{#if filters.length > 0}<span class="font-mono text-[9px]">
				{i18n.formatNumber(filters.length)}
			</span>{/if}
	</PopoverTrigger>
	<PopoverContent align="start" class="w-[360px] gap-2 p-3">
		<p class="font-mono text-[9px] tracking-[0.14em] text-foreground-faint uppercase">
			{i18n.t("collections:filterDocuments")}
		</p>
		<label class="grid gap-1 text-[11px] text-foreground-muted">
			{i18n.t("collections:field")}
			<Select type="single" value={fieldName} onValueChange={selectField}>
				<SelectTrigger size="toolbar" class="w-full" aria-label={i18n.t("collections:filterField")}>
					<span class={fieldName === "" ? "text-foreground-placeholder" : undefined}>
						{selectedFieldLabel}
					</span>
				</SelectTrigger>
				<SelectContent>
					<SelectItem value="" label={i18n.t("collections:chooseField")} />
					{#each readableFields as candidate (candidate.id)}
						<SelectItem value={candidate.path} label={candidate.admin.label} />
					{/each}
				</SelectContent>
			</Select>
		</label>
		{#if field !== undefined}
			<div class="grid grid-cols-[1fr_1fr] gap-2">
				<label class="grid gap-1 text-[11px] text-foreground-muted">
					{i18n.t("collections:operator")}
					<Select type="single" value={operator} onValueChange={selectOperator}>
						<SelectTrigger
							size="toolbar"
							class="w-full"
							aria-label={i18n.t("collections:filterOperator")}
						>
							{selectedOperatorLabel}
						</SelectTrigger>
						<SelectContent>
							{#each filterOperatorsFor(field) as candidate}
								<SelectItem value={candidate} label={filterOperatorLabel(candidate, i18n)} />
							{/each}
						</SelectContent>
					</Select>
				</label>
				<label class="grid gap-1 text-[11px] text-foreground-muted">
					{i18n.t("collections:value")}
					{#if booleanValueOptions.length > 0}
						<Select type="single" {value} onValueChange={(next) => (value = next)}>
							<SelectTrigger
								size="toolbar"
								class="w-full"
								aria-label={i18n.t("collections:filterValue")}
							>
								{booleanValueOptions.find((option) => option.value === value)?.label ??
									booleanValueOptions[0]?.label}
							</SelectTrigger>
							<SelectContent>
								{#each booleanValueOptions as option (option.value)}
									<SelectItem value={option.value} label={option.label} />
								{/each}
							</SelectContent>
						</Select>
					{:else if field.type === "select" || field.type === "radio"}
						<Select type="single" {value} onValueChange={(next) => (value = next)}>
							<SelectTrigger
								size="toolbar"
								class="w-full"
								aria-label={i18n.t("collections:filterValue")}
							>
								<span class={value === "" ? "text-foreground-placeholder" : undefined}>
									{field.select?.choices.find((choice) => choice.value === value)?.label ??
										i18n.t("collections:chooseValue")}
								</span>
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="" label={i18n.t("collections:chooseValue")} />
								{#each field.select?.choices ?? [] as choice}
									<SelectItem value={choice.value} label={choice.label} />
								{/each}
							</SelectContent>
						</Select>
					{:else if field.type === "date"}
						<DateValueControl
							id={`collection-filter-${field.id}`}
							appearance={field.date?.pickerAppearance}
							{value}
							label={i18n.t("collections:filterValue")}
							size="toolbar"
							onValueChange={(next) => (value = next)}
						/>
					{:else}
						<Input
							aria-label={i18n.t("collections:filterValue")}
							type={field.type === "number" ? "number" : "text"}
							class="h-8"
							bind:value
						/>
					{/if}
				</label>
			</div>
			<Button
				size="sm"
				class="w-full"
				disabled={operator !== "exists" && value === ""}
				onclick={addFilter}
			>
				{i18n.t("collections:addFilter")}
			</Button>
		{/if}
		{#if filters.length > 0}
			<div class="grid gap-1 border-t border-control-border pt-2">
				{#each filters as filter, index (`${filter.field}:${filter.operator}:${index}`)}
					<div class="flex items-center gap-2 rounded-[3px] bg-control px-2 py-1.5 text-[11px]">
						<span class="min-w-0 flex-1 truncate">
							{i18n.t("collections:filterSummary", {
								field:
									fields.find((candidate) => candidate.path === filter.field)?.admin.label ??
									filter.field,
								operator: filterOperatorLabel(filter.operator, i18n),
								value:
									filter.operator === "exists"
										? filter.value === "false"
											? i18n.t("general:no")
											: i18n.t("general:yes")
										: filter.value,
							})}
						</span>
						<Button
							variant="ghost"
							size="icon-xs"
							aria-label={i18n.t("collections:removeFilter", {
								index: i18n.formatNumber(index + 1),
							})}
							onclick={() => removeFilter(index)}
						>
							<XIcon class="size-3" />
						</Button>
					</div>
				{/each}
			</div>
		{/if}
	</PopoverContent>
</Popover>
