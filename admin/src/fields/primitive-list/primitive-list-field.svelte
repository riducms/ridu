<script lang="ts">
	import { tick } from "svelte";
	import type { SchemaField } from "@riducms/protocol";
	import { fieldControlARIA, Button, Input } from "@riducms/ui";
	import PlusIcon from "~icons/lucide/plus";
	import "@admin/fields/primitive-list/primitive-list.scss";
	import ArrowUpIcon from "~icons/lucide/arrow-up";
	import ArrowDownIcon from "~icons/lucide/arrow-down";
	import XIcon from "~icons/lucide/x";

	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { getAdminI18n } from "@riducms/plugin";
	import FieldShell from "@admin/fields/field-shell.svelte";
	import { primitiveNumberInput } from "@admin/fields/primitive-list/primitive-number-input";

	let { field, form }: { field: SchemaField; form: FormController } = $props();
	const i18n = getAdminI18n();

	$effect(() => form.register(field.path));
	const values = $derived.by(() => {
		const value = form.get(field.path);
		return Array.isArray(value) ? value : [];
	});
	const issues = $derived(form.issuesFor(field.path));
	const editingBlocked = $derived(field.admin.readOnly === true || form.editingBlocked);
	const canAdd = $derived(
		!editingBlocked && (!field.list?.maxRows || values.length < field.list.maxRows)
	);
	const minimum = $derived(Math.max(field.required ? 1 : 0, field.list?.minRows ?? 0));
	const feedbackARIA = $derived(
		fieldControlARIA(field.id, field.admin.description !== undefined, issues.length > 0)
	);
	const controlARIA = $derived({
		...feedbackARIA,
		"aria-describedby": [`${field.id}-count`, feedbackARIA["aria-describedby"]]
			.filter(Boolean)
			.join(" "),
	});
	let container: HTMLDivElement;
	let announcement = $state("");
	// Only DOM identity lives here. The form owns all values, including unfinished numeric input.
	let keys = $state.raw<string[]>([]);
	const usedKeys = $derived(new Set(keys));
	function itemKey(index: number) {
		const existing = keys[index];
		if (existing !== undefined) return existing;
		// An external owner can restore a removed item while its surviving key has
		// moved to this index. Give the restored item a distinct render key.
		let key = `initial-${index}`;
		while (usedKeys.has(key)) key += "-restored";
		return key;
	}

	async function focusItem(index: number) {
		// Insertion/removal must render before moving focus to the surviving input or Add button.
		await tick();
		container.querySelector<HTMLInputElement>(`[data-list-item="${index}"]`)?.focus();
		if (index < 0) container.querySelector<HTMLButtonElement>("[data-list-add]")?.focus();
	}
	function update(next: unknown[], nextKeys = values.map((_, index) => itemKey(index))) {
		if (editingBlocked) return;
		keys = nextKeys;
		form.set(field.path, next);
	}
	async function add() {
		if (!canAdd) return;
		const index = values.length;
		update([...values, ""], [...values.map((_, index) => itemKey(index)), crypto.randomUUID()]);
		announcement = i18n.t("fields:listAdded", { number: index + 1 });
		await focusItem(index);
	}
	async function typeNewItem(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		if (!canAdd || input.value === "") return;
		const index = values.length;
		update(
			[...values, field.type === "number-list" ? primitiveNumberInput(input.value) : input.value],
			[...values.map((_, at) => itemKey(at)), crypto.randomUUID()]
		);
		input.value = "";
		await focusItem(index);
		const item = container.querySelector<HTMLInputElement>(`[data-list-item="${index}"]`);
		item?.setSelectionRange(item.value.length, item.value.length);
	}

	function edit(index: number, value: string) {
		if (editingBlocked) return;
		const next = [...values];
		next[index] = field.type === "number-list" ? primitiveNumberInput(value) : value;
		update(next);
	}
	async function remove(index: number) {
		if (editingBlocked) return;
		const next = values.filter((_, at) => at !== index);
		update(
			next,
			values.map((_, at) => itemKey(at)).filter((_, at) => at !== index)
		);
		announcement = i18n.t("fields:listRemoved", { number: index + 1 });
		await focusItem(Math.min(index, next.length - 1));
	}
	async function move(index: number, direction: -1 | 1) {
		const destination = index + direction;
		if (editingBlocked || destination < 0 || destination >= values.length) return;
		const next = [...values];
		const nextKeys = values.map((_, at) => itemKey(at));
		[next[index], next[destination]] = [next[destination], next[index]];
		[nextKeys[index], nextKeys[destination]] = [nextKeys[destination]!, nextKeys[index]!];
		update(next, nextKeys);
		announcement = i18n.t("fields:listMoved", { from: index + 1, to: destination + 1 });
		await focusItem(destination);
	}
	function keyboard(event: KeyboardEvent, index: number) {
		if (event.key === "Enter" && !event.isComposing) {
			event.preventDefault();
			container.querySelector<HTMLInputElement>("[data-list-entry]")?.focus();
			return;
		}
		if (!event.altKey || (event.key !== "ArrowUp" && event.key !== "ArrowDown")) return;
		event.preventDefault();
		return move(index, event.key === "ArrowUp" ? -1 : 1);
	}
</script>

<FieldShell {field} {issues}>
	<div
		bind:this={container}
		class="ridu-primitive-list"
		data-invalid={issues.length > 0}
		data-readonly={editingBlocked}
	>
		<p id="{field.id}-count" class="ridu-primitive-list__count">
			{i18n.t("fields:itemCount", { count: values.length })}
			{#if minimum > 0}
				· {i18n.t("fields:minimum", { count: minimum })}
			{/if}
			{#if field.list?.maxRows}
				· {i18n.t("fields:maximum", { count: field.list.maxRows })}
			{/if}
		</p>
		{#if values.length === 0}
			<p class="ridu-primitive-list__empty">
				{i18n.t("fields:listEmpty")}
			</p>
		{/if}
		<ol class="ridu-primitive-list__items" aria-label={field.admin.label}>
			{#each values as value, index (itemKey(index))}
				<li class="ridu-primitive-list__item">
					<Input
						class="ridu-primitive-list__input"
						style={`width: ${Math.max(1, String(value ?? "").length) + 1}ch`}
						id={index === 0 ? field.id : `${field.id}-item-${itemKey(index)}`}
						data-list-item={index}
						aria-label={i18n.t("fields:listItem", {
							label: field.admin.label,
							number: index + 1,
						})}
						{...controlARIA}
						inputmode={field.type === "number-list" ? "decimal" : undefined}
						placeholder={field.admin.placeholder}
						value={typeof value === "string" || typeof value === "number" ? String(value) : ""}
						readonly={editingBlocked}
						oninput={(event) => edit(index, event.currentTarget.value)}
						onkeydown={(event) => keyboard(event, index)}
					/>
					<Button
						variant="ghost"
						size="icon"
						class="ridu-primitive-list__move"
						aria-label={i18n.t("fields:listMoveUp", { number: index + 1 })}
						disabled={editingBlocked || index === 0}
						onclick={() => move(index, -1)}
					>
						<ArrowUpIcon />
					</Button>
					<Button
						variant="ghost"
						size="icon"
						class="ridu-primitive-list__move"
						aria-label={i18n.t("fields:listMoveDown", { number: index + 1 })}
						disabled={editingBlocked || index === values.length - 1}
						onclick={() => move(index, 1)}
					>
						<ArrowDownIcon />
					</Button>
					<Button
						variant="ghost"
						size="icon"
						class="ridu-primitive-list__remove"
						aria-label={i18n.t("fields:listRemove", { number: index + 1 })}
						disabled={editingBlocked}
						onclick={() => remove(index)}
					>
						<XIcon />
					</Button>
				</li>
			{/each}
		</ol>
		<input
			class="ridu-primitive-list__entry"
			data-list-entry
			aria-label={`${field.admin.label}: ${i18n.t("fields:listAdd")}`}
			placeholder={field.admin.placeholder}
			disabled={!canAdd}
			autocomplete="off"
			oninput={typeNewItem}
		/>
		<Button
			id={values.length === 0 ? field.id : undefined}
			data-list-add
			aria-label={i18n.t("fields:listAdd")}
			{...controlARIA}
			variant="ghost"
			size="icon-sm"
			class="ridu-primitive-list__add"
			disabled={!canAdd}
			onclick={add}
		>
			<PlusIcon aria-hidden="true" />
		</Button>
		<p class="ridu-primitive-list__announcement" role="status">{announcement}</p>
	</div>
</FieldShell>
