"use client";

import { useEffect, useState } from "react";

type PreviewData = Record<string, unknown>;

export default function PreviewPage() {
	const [data, setData] = useState<PreviewData>({});

	useEffect(() => {
		const receive = (event: MessageEvent) => {
			if (event.origin !== window.location.origin || typeof event.data !== "object") return;
			const message = event.data as { type?: string; data?: unknown };
			if (message.type !== "payload-live-preview" || typeof message.data !== "object") return;
			setData((message.data ?? {}) as PreviewData);
		};
		window.addEventListener("message", receive);
		return () => window.removeEventListener("message", receive);
	}, []);

	const title = typeof data.title === "string" && data.title !== "" ? data.title : "Live preview";
	const summary =
		typeof data.summary === "string" && data.summary !== ""
			? data.summary
			: "Edit the Payload form to stream the current draft into this reference viewport.";

	return (
		<main
			style={{
				background: "#f4f0e8",
				color: "#181714",
				fontFamily: "ui-sans-serif, system-ui, sans-serif",
				minHeight: "100vh",
				padding: "clamp(2rem, 8vw, 8rem)",
			}}
		>
			<p style={{ fontSize: 12, letterSpacing: "0.16em", textTransform: "uppercase" }}>
				Payload comparison preview
			</p>
			<h1 style={{ fontFamily: "Georgia, serif", fontSize: "clamp(3rem, 9vw, 8rem)", margin: "1rem 0" }}>
				{title}
			</h1>
			<p style={{ fontSize: "clamp(1rem, 2vw, 1.5rem)", lineHeight: 1.6, maxWidth: 760 }}>
				{summary}
			</p>
		</main>
	);
}
