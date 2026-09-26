<script lang="ts">
	import "@admin/features/uploads/upload-metadata-fields.scss";
	import { getAdminI18n } from "@riducms/plugin";
	import type { SchemaField, ValidationIssue } from "@riducms/protocol";

	import { Input } from "@riducms/ui";
	import { Select, SelectContent, SelectItem, SelectTrigger } from "@admin/components/ui/select";

	interface Props {
		fields: readonly SchemaField[];
		values: Readonly<Record<string, string>>;
		issues: readonly ValidationIssue[];
		idPrefix: string;
		disabled?: boolean;
		labelPrefix?: string;
		onValueChange: (field: SchemaField, value: string) => void;
	}

	let {
		fields,
		values,
		issues,
		idPrefix,
		disabled = false,
		labelPrefix,
		onValueChange,
	}: Props = $props();
	const i18n = getAdminI18n();

	const metadataEntries = $derived(
		fields.map((field) => {
			const fieldIssues = issues.filter(
				(issue) => issue.path === field.path || issue.path === field.name
			);
			return {
				field,
				value: values[field.name] ?? "",
				fieldIssues,
				issueID: `${idPrefix}-${field.id}-issue`,
				inputID: `${idPrefix}-${field.id}`,
			};
		})
	);

	function selectedLabel(field: SchemaField, value: string) {
		const option = field.select?.options.find((candidate) => candidate.value === value);
		return option === undefined
			? i18n.t("uploads:chooseValue")
			: i18n.text(option.label, option.labelTranslations);
	}

	function controlLabel(field: SchemaField) {
		const label = i18n.text(field.admin.label, field.admin.labelTranslations);
		return labelPrefix === undefined ? label : `${labelPrefix} ${label}`;
	}
</script>

<div class="ridu-upload-metadata">
	{#each metadataEntries as entry (entry.field.id)}
		<label class="ridu-upload-metadata__field" for={entry.inputID}>
			<span>
				{i18n.text(entry.field.admin.label, entry.field.admin.labelTranslations)}{entry.field
					.required
					? " *"
					: ""}
			</span>
			{#if entry.field.type === "select"}
				<Select
					type="single"
					value={entry.value}
					onValueChange={(value) => onValueChange(entry.field, value)}
					{disabled}
				>
					<SelectTrigger
						id={entry.inputID}
						class="ridu-upload-metadata__select"
						aria-label={controlLabel(entry.field)}
						aria-invalid={entry.fieldIssues.length > 0}
						aria-describedby={entry.fieldIssues.length > 0 ? entry.issueID : undefined}
					>
						<span class={entry.value === "" ? "ridu-upload-metadata__placeholder" : undefined}>
							{selectedLabel(entry.field, entry.value)}
						</span>
					</SelectTrigger>
					<SelectContent>
						<SelectItem value="" label={i18n.t("uploads:chooseValue")} />
						{#each entry.field.select?.options ?? [] as option (option.value)}
							<SelectItem
								value={option.value}
								label={i18n.text(option.label, option.labelTranslations)}
							/>
						{/each}
					</SelectContent>
				</Select>
			{:else}
				<Input
					id={entry.inputID}
					aria-label={controlLabel(entry.field)}
					aria-invalid={entry.fieldIssues.length > 0}
					aria-describedby={entry.fieldIssues.length > 0 ? entry.issueID : undefined}
					value={entry.value}
					{disabled}
					oninput={(event) => onValueChange(entry.field, event.currentTarget.value)}
				/>
			{/if}
			{#if entry.fieldIssues.length > 0}
				<span id={entry.issueID} class="ridu-field-error" role="alert">
					{entry.fieldIssues.map((issue) => issue.message).join(" ")}
				</span>
			{/if}
		</label>
	{/each}
</div>
