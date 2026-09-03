<script lang="ts">
	import type { RowLabelComponentProps, RowLabelValue } from "@riducms/plugin";

	let { field, row, rowNumber, i18n, config }: RowLabelComponentProps = $props();
	const label = $derived(compositeLabel(row, rowNumber, config, i18n.formatNumber));

	function compositeLabel(
		currentRow: Readonly<Record<string, RowLabelValue>>,
		currentRowNumber: number,
		currentConfig: unknown,
		formatNumber: (value: number) => string
	) {
		if (!isRecord(currentConfig)) return `Row ${formatNumber(currentRowNumber)}`;
		if (currentConfig.kind === "typedOrder") {
			const typePath = stringProperty(currentConfig, "typePath");
			const orderPath = stringProperty(currentConfig, "orderPath");
			const type = typePath === undefined ? undefined : stringValue(readPath(currentRow, typePath));
			const targetPaths = isRecord(currentConfig.targets) ? currentConfig.targets : undefined;
			const targetPath = type === undefined ? undefined : stringValue(targetPaths?.[type]);
			const target =
				targetPath === undefined ? undefined : referenceID(readPath(currentRow, targetPath));
			const configuredOrder = orderPath === undefined ? undefined : readPath(currentRow, orderPath);
			const order = typeof configuredOrder === "number" ? configuredOrder : currentRowNumber;
			if (type !== undefined && target !== undefined) {
				return `${titleCase(type)}: ${target} (${formatNumber(order)})`;
			}
			return `${type === undefined ? "Node" : titleCase(type)} ${formatNumber(currentRowNumber)}`;
		}
		if (currentConfig.kind === "keyLabel") {
			const keyPath = stringProperty(currentConfig, "keyPath");
			const labelPath = stringProperty(currentConfig, "labelPath");
			const key = keyPath === undefined ? undefined : stringValue(readPath(currentRow, keyPath));
			const text =
				labelPath === undefined ? undefined : stringValue(readPath(currentRow, labelPath));
			return key === undefined ? `Item ${formatNumber(currentRowNumber)}` : `${key}: ${text ?? ""}`;
		}
		return `Row ${formatNumber(currentRowNumber)}`;
	}

	function readPath(value: Readonly<Record<string, RowLabelValue>>, path: string): RowLabelValue {
		let current: RowLabelValue = value;
		for (const segment of path.split(".")) {
			if (!isRecord(current)) return undefined;
			current = current[segment];
		}
		return current;
	}

	function referenceID(value: RowLabelValue) {
		if (typeof value === "string" && value.trim() !== "") return value;
		if (!isRecord(value)) return undefined;
		return stringValue(value.id);
	}

	function stringProperty(value: Readonly<Record<string, unknown>>, key: string) {
		return stringValue(value[key]);
	}

	function stringValue(value: unknown) {
		return typeof value === "string" && value.trim() !== "" ? value : undefined;
	}

	function isRecord(value: unknown): value is Readonly<Record<string, RowLabelValue>> {
		return typeof value === "object" && value !== null && !Array.isArray(value);
	}

	function titleCase(value: string) {
		return value.charAt(0).toLocaleUpperCase() + value.slice(1);
	}
</script>

<span data-contract-row-label={field.path}>{label}</span>
