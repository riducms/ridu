<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";

	import { Button } from "@admin/components/ui/button";
	import { DateValueControl } from "@admin/components/ui/date-value-control";
	import {
		Dialog,
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle,
	} from "@admin/components/ui/dialog";
	import { Input } from "@admin/components/ui/input";
	import { Select, SelectContent, SelectItem, SelectTrigger } from "@admin/components/ui/select";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	const runtime = getAdminRuntime();

	let {
		open = $bindable(false),
		selectedCount,
		fields,
		pending = false,
		onApply,
	}: {
		open?: boolean;
		selectedCount: number;
		fields: readonly SchemaField[];
		pending?: boolean;
		onApply: (field: SchemaField, value: unknown) => Promise<boolean>;
	} = $props();

	let fieldName = $state("");
	let value = $state("");
	let wasOpen = $state(false);
	const field = $derived(fields.find((candidate) => candidate.name === fieldName));
	const fieldLabel = $derived(field?.admin.label ?? runtime.i18n.t("collections:chooseField"));
	const selectedChoiceLabel = $derived(
		field?.select?.choices.find((choice) => choice.value === value)?.label ??
			(value === "" ? runtime.i18n.t("collections:clearValue") : value)
	);

	$effect(() => {
		if (open && !wasOpen) {
			fieldName = fields[0]?.name ?? "";
			value = "";
		}
		if (fieldName !== "" && !fields.some((candidate) => candidate.name === fieldName)) {
			fieldName = fields[0]?.name ?? "";
			value = "";
		}
		wasOpen = open;
	});

	function selectField(next: string) {
		fieldName = next;
		value = "";
	}

	async function submit() {
		if (field === undefined) return;
		let next: unknown = value;
		if (field.type === "number") {
			next = Number(value);
			if (!Number.isFinite(next)) return;
		} else if (field.type === "checkbox") {
			next = value === "true";
		}
		if (await onApply(field, next)) open = false;
	}
</script>

<Dialog bind:open>
	<DialogContent>
		<DialogHeader>
			<DialogTitle class="font-serif text-[28px] leading-tight font-normal">
				{runtime.i18n.t("collections:bulkEditSelected", {
					count: selectedCount,
					formattedCount: runtime.i18n.formatNumber(selectedCount),
				})}
			</DialogTitle>
			<DialogDescription>
				{runtime.i18n.t("collections:bulkEditDescription")}
			</DialogDescription>
		</DialogHeader>
		<div class="grid gap-4 py-2">
			<label class="grid gap-1.5 text-[12px] text-foreground-muted">
				{runtime.i18n.t("collections:field")}
				<Select type="single" value={fieldName} onValueChange={selectField}>
					<SelectTrigger class="w-full" aria-label={runtime.i18n.t("collections:field")}>
						{fieldLabel}
					</SelectTrigger>
					<SelectContent>
						{#each fields as candidate (candidate.id)}
							<SelectItem value={candidate.name} label={candidate.admin.label} />
						{/each}
					</SelectContent>
				</Select>
			</label>
			<label class="grid gap-1.5 text-[12px] text-foreground-muted">
				{runtime.i18n.t("collections:value")}
				{#if field?.type === "select" || field?.type === "radio"}
					<Select type="single" {value} onValueChange={(next) => (value = next)}>
						<SelectTrigger class="w-full" aria-label={runtime.i18n.t("collections:value")}>
							<span class={value === "" ? "text-foreground-placeholder" : undefined}>
								{selectedChoiceLabel}
							</span>
						</SelectTrigger>
						<SelectContent>
							<SelectItem value="" label={runtime.i18n.t("collections:clearValue")} />
							{#each field.select?.choices ?? [] as choice (choice.value)}
								<SelectItem value={choice.value} label={choice.label} />
							{/each}
						</SelectContent>
					</Select>
				{:else if field?.type === "checkbox"}
					<Select type="single" {value} onValueChange={(next) => (value = next)}>
						<SelectTrigger class="w-full" aria-label={runtime.i18n.t("collections:value")}>
							{value === "true"
								? runtime.i18n.t("collections:enabled")
								: runtime.i18n.t("collections:disabled")}
						</SelectTrigger>
						<SelectContent>
							<SelectItem value="false" label={runtime.i18n.t("collections:disabled")} />
							<SelectItem value="true" label={runtime.i18n.t("collections:enabled")} />
						</SelectContent>
					</Select>
				{:else if field?.type === "date"}
					<DateValueControl
						id={`bulk-edit-${field.id}`}
						appearance={field.date?.pickerAppearance}
						{value}
						label={runtime.i18n.t("collections:value")}
						onValueChange={(next) => (value = next)}
					/>
				{:else}
					<Input
						aria-label={runtime.i18n.t("collections:value")}
						type={field?.type === "number" ? "number" : "text"}
						bind:value
					/>
				{/if}
			</label>
		</div>
		<DialogFooter>
			<Button variant="outline" onclick={() => (open = false)}>
				{runtime.i18n.t("general:cancel")}
			</Button>
			<Button disabled={pending || field === undefined} onclick={submit}>
				{pending
					? runtime.i18n.t("collections:applying")
					: runtime.i18n.t("collections:applyChange")}
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>
