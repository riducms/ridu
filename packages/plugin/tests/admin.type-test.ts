import type { Component } from "svelte";
import type {
	AdminDashboardPanelProps,
	AdminDocumentExtensionProps,
	AdminListCellProps,
} from "../src/plugin";
import { defineAdmin, defineRowLabel, type RowLabelProps } from "../src/admin";
declare const Dashboard: Component<AdminDashboardPanelProps>;
declare const Action: Component<AdminDocumentExtensionProps>;
declare const Cell: Component<AdminListCellProps>;
declare const Label: Component<RowLabelProps<{ title: string }>>;
declare const Wrong: Component<{ value: number }>;
export function componentProbes() {
	defineAdmin({
		dashboard: [{ key: "summary", component: Dashboard }],
		documentActions: [{ key: "review", component: Action }],
		listCells: [
			{ key: "cell", collection: "posts", field: "title", label: "Title", component: Cell },
		],
	});
	// @ts-expect-error dashboard hosts do not provide a document action host
	defineAdmin({ dashboard: [{ key: "summary", component: Action }] });
	// @ts-expect-error wrong component props
	defineAdmin({ providers: [{ key: "provider", component: Wrong }] });
	// @ts-expect-error application field renderers have only the field editor contract
	defineAdmin({ fieldPlugins: [] });
	defineAdmin({
		// @ts-expect-error a core global view cannot select a collection
		views: [{ key: "global", surface: "global", collection: "posts", component: Dashboard }],
	});
	defineRowLabel({ component: Label, decodeConfig: () => ({ title: "Title" }) });
	// @ts-expect-error required config requires a decoder
	defineRowLabel({ component: Label });
	// @ts-expect-error explicit generic cannot erase required decoder
	defineRowLabel<{ title: string }>({ component: Label });
	// @ts-expect-error decoder output disagrees with component config
	defineRowLabel({ component: Label, decodeConfig: () => ({ title: 123 }) });
	// @ts-expect-error wrong props
	defineRowLabel({ component: Wrong });
	// @ts-expect-error malformed row registration
	defineAdmin({ rowLabels: { "app:label": { component: Label } } });
}

function simpleLabelProps(base: Omit<RowLabelProps, "config">) {
	const props: RowLabelProps = base;
	// @ts-expect-error configured row labels require decoded settings
	const configured: RowLabelProps<{ title: string }> = base;
	void [props, configured];
}
void simpleLabelProps;
