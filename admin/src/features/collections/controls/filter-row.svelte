<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";
	import { getAdminI18n } from "@riducms/plugin";
	import { Select, SelectContent, SelectItem, SelectTrigger } from "@admin/components/ui/select";
	import { Input } from "@riducms/ui";
	import { DateValueControl } from "@admin/components/ui/date-value-control";
	import Plus from "@admin/components/icons/plus.svelte";
	import X from "@admin/components/icons/x.svelte";
	import {
		filterOperatorsFor,
		filterOperatorLabel,
		type ListFilter,
		type ListFilterOperator,
	} from "@admin/features/collections/list-workspace";

	let {
		filter,
		fields,
		onChange,
		onAdd,
		onRemove,
		index,
	}: {
		filter: ListFilter;
		fields: readonly SchemaField[];
		onChange: (filter: ListFilter) => void;
		onAdd: () => void;
		onRemove: () => void;
		index: number;
	} = $props();

	const i18n = getAdminI18n();

	const field = $derived(fields.find((candidate) => candidate.path === filter.field));
	const options = $derived.by(() => {
		if (filter.operator === "exists")
			return [
				{ value: "true", label: i18n.t("collections:hasValue") },
				{ value: "false", label: i18n.t("collections:isEmpty") },
			];
		if (field?.type === "checkbox")
			return [
				{ value: "true", label: i18n.t("collections:enabled") },
				{ value: "false", label: i18n.t("collections:disabled") },
			];

		return field?.select?.options ?? [];
	});

	function selectField(path: string) {
		const candidate = fields.find((candidate) => candidate.path === path);
		onChange({
			field: path,
			operator: candidate ? filterOperatorsFor(candidate)[0]! : "equals",
			value: "",
		});
	}

	function selectOperator(operator: string) {
		onChange({
			...filter,
			operator: operator as ListFilterOperator,
			value: operator === "exists" ? "true" : "",
		});
	}

	function setValue(value: string) {
		onChange({ ...filter, value });
	}
</script>

<div class="ridu-list-filter__row">
	<div class="ridu-list-filter__inputs">
		<Select type="single" value={filter.field} onValueChange={selectField}>
			<SelectTrigger aria-label={i18n.t("collections:filterField")}>
				{field?.admin.label ?? i18n.t("collections:chooseField")}
			</SelectTrigger>
			<SelectContent>
				{#each fields as candidate (candidate.path)}
					<SelectItem value={candidate.path} label={candidate.admin.label} />
				{/each}
			</SelectContent>
		</Select>
		<Select type="single" value={filter.operator} onValueChange={selectOperator}>
			<SelectTrigger aria-label={i18n.t("collections:filterOperator")}>
				{filterOperatorLabel(filter.operator, i18n)}
			</SelectTrigger>
			<SelectContent>
				{#each field ? filterOperatorsFor(field) : [] as operator}
					<SelectItem value={operator} label={filterOperatorLabel(operator, i18n)} />
				{/each}
			</SelectContent>
		</Select>
		{#if options.length > 0}
			<Select type="single" value={filter.value} onValueChange={setValue}>
				<SelectTrigger aria-label={i18n.t("collections:filterValue")}>
					{options.find((option) => option.value === filter.value)?.label ??
						i18n.t("collections:chooseValue")}
				</SelectTrigger>
				<SelectContent>
					{#each options as option}
						<SelectItem value={option.value} label={option.label} />
					{/each}
				</SelectContent>
			</Select>
		{:else if field?.type === "date"}
			<DateValueControl
				id={`list-filter-${index}`}
				appearance={field.date?.format}
				value={filter.value}
				label={i18n.t("collections:filterValue")}
				onValueChange={setValue}
			/>
		{:else}
			<Input
				aria-label={i18n.t("collections:filterValue")}
				type={field?.type === "number" || field?.type === "number-list" ? "number" : "text"}
				value={filter.value}
				oninput={(event) => setValue(event.currentTarget.value)}
			/>
		{/if}
	</div>
	<div class="ridu-list-filter__actions">
		<button
			type="button"
			class="ridu-list__circle"
			aria-label={i18n.t("collections:removeFilter", { index: i18n.formatNumber(index + 1) })}
			data-filter-remove
			onclick={onRemove}
		>
			<X />
		</button>
		<button
			type="button"
			class="ridu-list__circle"
			aria-label={i18n.t("collections:addAndFilter")}
			onclick={onAdd}
		>
			<Plus />
		</button>
	</div>
</div>
