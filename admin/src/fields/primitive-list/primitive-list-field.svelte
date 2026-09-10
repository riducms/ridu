<script lang="ts">
	import { tick } from "svelte";
	import type { SchemaField } from "@riducms/protocol";
	import { fieldControlARIA } from "@riducms/ui";
	import ArrowUpIcon from "~icons/lucide/arrow-up";
	import ArrowDownIcon from "~icons/lucide/arrow-down";
	import XIcon from "~icons/lucide/x";
	import { Button } from "@admin/components/ui/button";
	import { Input } from "@admin/components/ui/input";
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
	let keys = $state<string[]>([]);
	const itemKey = (index: number) => keys[index] ?? `initial-${index}`;

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
		if (!event.altKey || (event.key !== "ArrowUp" && event.key !== "ArrowDown")) return;
		event.preventDefault();
		return move(index, event.key === "ArrowUp" ? -1 : 1);
	}
</script>

<FieldShell {field} {issues}>
	<div bind:this={container} class="grid gap-2">
		<p id="{field.id}-count" class="text-xs text-foreground-muted">
			{i18n.t("fields:itemCount", { count: values.length })}
			{#if minimum > 0}
				· {i18n.t("fields:minimum", { count: minimum })}{/if}
			{#if field.list?.maxRows}
				· {i18n.t("fields:maximum", { count: field.list.maxRows })}{/if}
		</p>
		{#if values.length === 0}<p class="text-sm text-foreground-muted">
				{i18n.t("fields:listEmpty")}
			</p>{/if}
		<ol class="grid gap-2" aria-label={field.admin.label}>
			{#each values as value, index (itemKey(index))}
				<li class="flex items-center gap-1">
					<Input
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
						aria-label={i18n.t("fields:listMoveUp", { number: index + 1 })}
						disabled={editingBlocked || index === 0}
						onclick={() => move(index, -1)}
					>
						<ArrowUpIcon class="size-4" />
					</Button>
					<Button
						variant="ghost"
						size="icon"
						aria-label={i18n.t("fields:listMoveDown", { number: index + 1 })}
						disabled={editingBlocked || index === values.length - 1}
						onclick={() => move(index, 1)}
					>
						<ArrowDownIcon class="size-4" />
					</Button>
					<Button
						variant="ghost"
						size="icon"
						aria-label={i18n.t("fields:listRemove", { number: index + 1 })}
						disabled={editingBlocked}
						onclick={() => remove(index)}
					>
						<XIcon class="size-4" />
					</Button>
				</li>
			{/each}
		</ol>
		<Button
			id={values.length === 0 ? field.id : undefined}
			data-list-add
			aria-label={i18n.t("fields:listAdd")}
			{...controlARIA}
			variant="outline"
			class="w-fit"
			disabled={!canAdd}
			onclick={add}
		>
			{i18n.t("fields:listAdd")}
		</Button>
		<p class="sr-only" role="status">{announcement}</p>
	</div>
</FieldShell>
