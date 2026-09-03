export const RIDU_LIVE_PREVIEW_MESSAGE = "ridu-live-preview" as const;
export const RIDU_LIVE_PREVIEW_CHANNEL_PARAM = "__ridu_preview" as const;
export const RIDU_PREVIEW_TOKEN_PARAM = "__ridu_preview_token" as const;

export interface RiduLivePreviewReadyMessage {
	type: typeof RIDU_LIVE_PREVIEW_MESSAGE;
	ready: true;
	channel: string;
}

export interface RiduLivePreviewUpdateMessage<Data extends object> {
	type: typeof RIDU_LIVE_PREVIEW_MESSAGE;
	channel: string;
	sequence: number;
	resource: "collection" | "global";
	slug: string;
	collection: string;
	id: string;
	data: Data;
}

export interface LivePreviewTarget {
	resource: "collection" | "global";
	slug: string;
	id: string;
}

export interface LivePreviewConnection<Data extends object> {
	/** Re-announces readiness after an application-side route or renderer reset. */
	ready: () => void;
	disconnect: () => void;
}

export function connectLivePreview<Data extends object>({
	adminOrigin,
	target: expectedTarget,
	onUpdate,
	window: injectedWindow = window,
}: {
	adminOrigin: string;
	target: LivePreviewTarget;
	onUpdate: (message: RiduLivePreviewUpdateMessage<Data>) => void;
	/** Browser window override used by embedded renderers and tests. */
	window?: object;
}): LivePreviewConnection<Data> {
	// Keep the browser-only Window type inside the implementation so importing the SDK's
	// declarations from Node does not require TypeScript's DOM library.
	const previewWindow = injectedWindow as Window;
	const expectedOrigin = new URL(adminOrigin).origin;
	const channel = new URL(previewWindow.location.href).searchParams.get(
		RIDU_LIVE_PREVIEW_CHANNEL_PARAM
	);
	if (channel === null || channel === "") {
		throw new Error(`Live preview URL is missing ${RIDU_LIVE_PREVIEW_CHANNEL_PARAM}`);
	}
	const target = previewWindow.opener ?? previewWindow.parent;
	if (target === previewWindow) throw new Error("Live preview requires a parent iframe or opener");

	const ready = () => {
		target.postMessage(
			{
				type: RIDU_LIVE_PREVIEW_MESSAGE,
				ready: true,
				channel,
			} satisfies RiduLivePreviewReadyMessage,
			expectedOrigin
		);
	};
	const handleMessage = (event: MessageEvent) => {
		if (event.origin !== expectedOrigin || event.source !== target) return;
		if (!isLivePreviewUpdate<Data>(event.data, channel, expectedTarget)) return;
		onUpdate(event.data);
	};
	previewWindow.addEventListener("message", handleMessage);
	previewWindow.addEventListener("pageshow", ready);
	previewWindow.addEventListener("focus", ready);
	previewWindow.addEventListener("online", ready);
	ready();

	return {
		ready,
		disconnect: () => {
			previewWindow.removeEventListener("message", handleMessage);
			previewWindow.removeEventListener("pageshow", ready);
			previewWindow.removeEventListener("focus", ready);
			previewWindow.removeEventListener("online", ready);
		},
	};
}

function isLivePreviewUpdate<Data extends object>(
	value: unknown,
	channel: string,
	target: LivePreviewTarget
): value is RiduLivePreviewUpdateMessage<Data> {
	if (value === null || typeof value !== "object") return false;
	const candidate = value as Partial<RiduLivePreviewUpdateMessage<Data>>;
	return (
		candidate.type === RIDU_LIVE_PREVIEW_MESSAGE &&
		candidate.channel === channel &&
		typeof candidate.sequence === "number" &&
		Number.isSafeInteger(candidate.sequence) &&
		candidate.sequence >= 0 &&
		candidate.resource === target.resource &&
		candidate.slug === target.slug &&
		candidate.id === target.id &&
		typeof candidate.collection === "string" &&
		candidate.data !== null &&
		typeof candidate.data === "object" &&
		!Array.isArray(candidate.data)
	);
}
