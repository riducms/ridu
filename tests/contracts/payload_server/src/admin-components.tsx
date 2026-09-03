"use client";

import { toast, useAuth } from "@payloadcms/ui";
import { useState, type CSSProperties, type ReactNode } from "react";

const accent = "var(--theme-success-500)";
const border = "1px solid var(--theme-elevation-150)";
const panel: CSSProperties = {
	background: "var(--theme-elevation-50)",
	border,
	borderRadius: 6,
	padding: 16,
};
const badge: CSSProperties = {
	background: "var(--theme-elevation-50)",
	border,
	borderRadius: 999,
	bottom: 16,
	color: accent,
	fontSize: 11,
	padding: "6px 10px",
	position: "fixed",
	right: 16,
	zIndex: 30,
};

export function BrandLogo() {
	return (
		<div style={{ display: "grid", gap: 2 }}>
			<strong style={{ fontSize: 25, fontStyle: "italic", fontWeight: 500 }}>
				Payload extension
			</strong>
			<span
				style={{ color: accent, fontSize: 9, letterSpacing: "0.18em", textTransform: "uppercase" }}
			>
				Admin
			</span>
		</div>
	);
}

export function BrandIcon() {
	return (
		<span
			aria-hidden="true"
			style={{
				alignItems: "center",
				display: "inline-flex",
				fontSize: 10,
				fontWeight: 700,
				justifyContent: "center",
			}}
		>
			PX
		</span>
	);
}

export function AccountAvatar() {
	return (
		<span
			aria-hidden="true"
			style={{
				...panel,
				alignItems: "center",
				borderRadius: "50%",
				display: "inline-flex",
				height: 28,
				justifyContent: "center",
				padding: 0,
				width: 28,
			}}
		>
			PX
		</span>
	);
}

export function DashboardPanel() {
	const { user } = useAuth();
	return (
		<section aria-labelledby="payload-editorial-pulse" style={{ ...panel, marginBottom: 24 }}>
			<p
				style={{
					color: accent,
					fontSize: 10,
					letterSpacing: "0.14em",
					margin: 0,
					textTransform: "uppercase",
				}}
			>
				Extension panel
			</p>
			<h2 id="payload-editorial-pulse" style={{ fontSize: 24, margin: "8px 0" }}>
				Editorial pulse
			</h2>
			<p style={{ margin: 0 }}>
				Nine collections are ready for {String(user?.name ?? "the team")}.
			</p>
		</section>
	);
}

export function LoginFrame() {
	return <p style={{ color: accent, textAlign: "center" }}>Custom login wrapper</p>;
}

export function NavigationNote() {
	return (
		<p style={{ ...panel, color: accent, fontSize: 11, margin: "12px 16px", padding: "8px 10px" }}>
			Payload parity fixture plugin shell
		</p>
	);
}

export function PluginRouteLink() {
	return (
		<a
			href="/admin/plugin-contract"
			style={{ display: "block", margin: "8px 20px", textDecoration: "none" }}
		>
			Plugin contract
		</a>
	);
}

export function ShellHeader() {
	return <p style={{ color: accent, fontSize: 12, margin: "0 var(--gutter-h)" }}>Plugin header</p>;
}

export function ShellAction() {
	return (
		<button
			type="button"
			style={{
				background: "transparent",
				border,
				borderRadius: 4,
				color: accent,
				padding: "6px 10px",
			}}
		>
			Plugin action
		</button>
	);
}

export function SettingsMenuItem() {
	return (
		<button
			type="button"
			style={{
				background: "transparent",
				border: 0,
				color: "inherit",
				padding: "8px 12px",
				textAlign: "left",
				width: "100%",
			}}
		>
			Plugin settings
		</button>
	);
}

export function ProviderBoundary({ children }: { children?: ReactNode }) {
	return (
		<>
			<span className="sr-only">Plugin provider boundary</span>
			{children}
		</>
	);
}

export function PluginLogoutButton() {
	const { logOut } = useAuth();
	const [pending, setPending] = useState(false);

	async function logout() {
		setPending(true);
		try {
			await logOut();
		} finally {
			setPending(false);
		}
	}

	return (
		<button
			disabled={pending}
			onClick={() => void logout()}
			type="button"
			style={{
				background: "transparent",
				border: 0,
				color: "inherit",
				padding: "8px 12px",
				textAlign: "left",
				width: "100%",
			}}
		>
			{pending ? "Leaving the extension..." : "Plugin sign out"}
		</button>
	);
}

export function AccountView() {
	const { user } = useAuth();
	return (
		<section style={{ margin: "0 auto", maxWidth: 820, padding: "var(--gutter-h)" }}>
			<p style={{ color: accent }}>
				Custom profile view for {String(user?.name ?? "the signed-in user")}
			</p>
			<h1>Extension account</h1>
			<p>This replacement demonstrates how Payload renders a plugin-owned account route.</p>
		</section>
	);
}

export function PluginRoute() {
	return (
		<section aria-labelledby="payload-plugin-route" style={{ padding: "var(--gutter-h)" }}>
			<h1 id="payload-plugin-route">Plugin route contract</h1>
			<p>
				This route proves Payload custom views render inside its universal admin breadcrumb shell.
			</p>
		</section>
	);
}

export function PostsListFrame() {
	return <p style={badge}>Plugin collectionList view</p>;
}

export function PostDocumentFrame() {
	return <p style={badge}>Plugin collectionEdit view</p>;
}

export function GlobalDocumentFrame() {
	return <p style={badge}>Plugin global view</p>;
}

export function ReadingTimeCell({ cellData }: { cellData?: unknown }) {
	return (
		<span style={{ fontFamily: "monospace", fontSize: 11 }}>
			{typeof cellData === "number" ? `${cellData} min` : "-"}
		</span>
	);
}

export function PostReviewAction() {
	return (
		<button
			onClick={() => toast.success("Editorial review requested")}
			type="button"
			style={{
				background: "transparent",
				border,
				borderRadius: 4,
				color: "inherit",
				padding: "8px 12px",
			}}
		>
			Request review
		</button>
	);
}

export function PostInsightsView() {
	return (
		<section aria-labelledby="payload-post-insights" style={{ padding: "var(--gutter-h)" }}>
			<p
				style={{ color: accent, fontSize: 10, letterSpacing: "0.15em", textTransform: "uppercase" }}
			>
				Custom document view
			</p>
			<h1 id="payload-post-insights">Editorial insights</h1>
			<div style={{ ...panel, marginTop: 24 }}>
				<p style={{ margin: 0 }}>
					Payload renders this as a native document tab supplied by the parity fixture.
				</p>
			</div>
		</section>
	);
}
