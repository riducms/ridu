import { DefaultTemplate } from "@payloadcms/next/templates";
import type { AdminViewServerProps } from "payload";

export function PluginRoute({
	i18n,
	initPageResult,
	locale,
	params,
	payload,
	permissions,
	searchParams,
	user,
}: AdminViewServerProps) {
	return (
		<DefaultTemplate
			className="plugin-contract-view"
			i18n={i18n}
			locale={locale}
			params={params}
			payload={payload}
			permissions={permissions}
			req={initPageResult.req}
			searchParams={searchParams}
			user={user}
			viewType="plugin-contract"
			visibleEntities={{
				collections: [...initPageResult.visibleEntities.collections],
				globals: [...initPageResult.visibleEntities.globals],
			}}
		>
			<main
				style={{
					margin: "0 auto",
					maxWidth: "72rem",
					padding: "var(--base) calc(var(--base) * 2)",
					width: "100%",
				}}
			>
				<h1 style={{ margin: 0 }}>Plugin route contract</h1>
				<p style={{ color: "var(--theme-elevation-600)" }}>
					This route proves how Payload custom views compose its default admin shell and universal
					breadcrumb.
				</p>
			</main>
		</DefaultTemplate>
	);
}
