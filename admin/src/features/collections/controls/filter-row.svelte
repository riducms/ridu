<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import { Select, SelectContent, SelectItem, SelectTrigger } from "@admin/components/ui/select";
	import { Input } from "@riducms/ui";
	import { DateValueControl } from "@admin/components/ui/date-value-control";
	import Plus from "@admin/components/icons/plus.svelte";
	import X from "@admin/components/icons/x.svelte";
	import FilterFieldPicker from "@admin/features/collections/controls/filter-field-picker.svelte";
	import type { ListFilterFields } from "@admin/features/collections/list-filter-fields";
	import {
		filterOperatorsFor,
		filterOperatorLabel,
		membershipOperator,
		parseReferenceCandidate,
		referenceCandidate,
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
		fields: ListFilterFields;
		onChange: (filter: ListFilter) => void;
		onAdd: () => void;
		onRemove: () => void;
		index: number;
	} = $props();

	const i18n = getAdminI18n();

	const field = $derived(fields.resolve(filter.field)?.field);
	const membership = $derived(membershipOperator(filter.operator));
	const candidates = $derived(Array.isArray(filter.value) ? filter.value : []);
	const text = $derived(typeof filter.value === "string" ? filter.value : (candidates[0] ?? ""));
	// A polymorphic candidate names its collection as well as its document ID.
	const reference = $derived.by(() => {
		const targets = field?.relationship?.polymorphic ? (field.relationship.targets ?? []) : [];
		if (!membership || targets.length === 0) return undefined;
		const current = parseReferenceCandidate(candidates[0] ?? "");
		return {
			targets,
			relationTo: current?.relationTo ?? targets[0]!.collectionSlug,
			id: current?.id ?? "",
		};
	});
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

		return (field?.select?.options ?? []).map((option) => ({
			value: option.value,
			label: i18n.text(option.label, option.labelTranslations),
		}));
	});

	function emptyValue(operator: ListFilterOperator) {
		if (operator === "exists") return "true";
		return membershipOperator(operator) ? [] : "";
	}

	function selectField(path: string) {
		const candidate = fields.resolve(path)?.field;
		const operator = candidate ? filterOperatorsFor(candidate)[0]! : "equals";
		onChange({ field: path, operator, value: emptyValue(operator) });
	}

	function selectOperator(operator: string) {
		const next = operator as ListFilterOperator;
		// Candidates carry over between "is any of" and "is none of".
		const value = membership && membershipOperator(next) ? filter.value : emptyValue(next);
		onChange({ ...filter, operator: next, value });
	}

	function setValue(value: string) {
		onChange({ ...filter, value: membership ? (value === "" ? [] : [value]) : value });
	}

	function setCandidates(value: string[]) {
		onChange({ ...filter, value });
	}

	function setReference(relationTo: string, id: string) {
		onChange({ ...filter, value: [referenceCandidate(relationTo, id)] });
	}
</script>

<div class="ridu-list-filter__row">
	<div class="ridu-list-filter__inputs">
		<FilterFieldPicker {fields} value={filter.field} onValueChange={selectField} />
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
		{#if reference}
			<Select
				type="single"
				value={reference.relationTo}
				onValueChange={(relationTo) => setReference(relationTo, reference.id)}
			>
				<SelectTrigger aria-label={i18n.t("collections:filterCollection")}>
					{fields.collectionLabel(reference.relationTo)}
				</SelectTrigger>
				<SelectContent>
					{#each reference.targets as target (target.collectionSlug)}
						<SelectItem
							value={target.collectionSlug}
							label={fields.collectionLabel(target.collectionSlug)}
						/>
					{/each}
				</SelectContent>
			</Select>
			<Input
				aria-label={i18n.t("collections:filterValue")}
				value={reference.id}
				oninput={(event) => setReference(reference.relationTo, event.currentTarget.value)}
			/>
		{:else if membership && options.length > 0}
			<Select type="multiple" value={candidates} onValueChange={setCandidates}>
				<SelectTrigger aria-label={i18n.t("collections:filterValue")}>
					{options
						.filter((option) => candidates.includes(option.value))
						.map((option) => option.label)
						.join(", ") || i18n.t("collections:chooseValue")}
				</SelectTrigger>
				<SelectContent>
					{#each options as option}
						<SelectItem value={option.value} label={option.label} />
					{/each}
				</SelectContent>
			</Select>
		{:else if options.length > 0}
			<Select type="single" value={text} onValueChange={setValue}>
				<SelectTrigger aria-label={i18n.t("collections:filterValue")}>
					{options.find((option) => option.value === text)?.label ??
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
				value={text}
				label={i18n.t("collections:filterValue")}
				onValueChange={setValue}
			/>
		{:else}
			<Input
				aria-label={i18n.t("collections:filterValue")}
				type={field?.type === "number" || field?.type === "number-list" ? "number" : "text"}
				value={text}
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
