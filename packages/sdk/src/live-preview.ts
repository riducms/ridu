/** Message discriminator shared by the admin and a live-preview renderer. */
export const RIDU_LIVE_PREVIEW_MESSAGE = "ridu-live-preview" as const;
/** URL query parameter carrying the ephemeral live-preview messaging channel. */
export const RIDU_LIVE_PREVIEW_CHANNEL_PARAM = "__ridu_preview" as const;
/** URL query parameter carrying the server-issued preview read token. */
export const RIDU_PREVIEW_TOKEN_PARAM = "__ridu_preview_token" as const;

/** Readiness message a preview renderer sends to its trusted admin window. */
export interface RiduLivePreviewReadyMessage {
	/** Identifies this message as part of Ridu's live-preview protocol. */
	type: typeof RIDU_LIVE_PREVIEW_MESSAGE;
	/** Distinguishes readiness announcements from document updates. */
	ready: true;
	/** Ephemeral channel copied from `RIDU_LIVE_PREVIEW_CHANNEL_PARAM`. */
	channel: string;
}

/** Unsaved document state accepted after the receiver validates its browser channel and target. */
export interface RiduLivePreviewUpdateMessage<Data extends object> {
	/** Identifies this message as part of Ridu's live-preview protocol. */
	type: typeof RIDU_LIVE_PREVIEW_MESSAGE;
	/** Ephemeral channel that binds the admin and renderer. */
	channel: string;
	/** Monotonically increasing update number for the current preview session. */
	sequence: number;
	/** Kind of resource represented by `data`. */
	resource: "collection" | "global";
	/** Collection or global slug expected by the renderer. */
	slug: string;
	/** Collection context supplied by the admin. */
	collection: string;
	/** Document identity, or the global slug for a global target. */
	id: string;
	/** Current unsaved form state; render defensively because it may not be valid yet. */
	data: Data;
}

/** Resource identity a live-preview connection accepts updates for. */
export interface LivePreviewTarget {
	/** Whether the target is a collection document or global. */
	resource: "collection" | "global";
	/** Collection or global slug. */
	slug: string;
	/** Document ID, or the global slug for a global target. */
	id: string;
}

/** Controls the lifecycle of an active live-preview message receiver. */
export interface LivePreviewConnection<Data extends object> {
	/** Re-announces readiness after an application-side route or renderer reset. */
	ready: () => void;
	/** Remove all listeners installed by `connectLivePreview`. */
	disconnect: () => void;
}

/**
 * Receive validated unsaved form updates from the Ridu admin.
 *
 * Call this in browser code loaded by a live-preview page. The connection accepts messages only
 * from the configured admin origin, source window, ephemeral URL channel, and expected resource.
 * It announces readiness immediately and after browser lifecycle resets; disconnect it when the
 * renderer unmounts.
 *
 * @returns A connection that can re-announce readiness or remove its listeners.
 * @throws If the page has no preview channel or is not inside an iframe or opener.
 * @example
 * ```ts
 * import { connectLivePreview } from "@riducms/sdk";
 * import type { Posts } from "./generated/ridu.generated";
 *
 * export function connectPostPreview(id: string, render: (post: Posts) => void) {
 *   const connection = connectLivePreview<Posts>({
 *     adminOrigin: "https://cms.example.com",
 *     target: { resource: "collection", slug: "posts", id },
 *     onUpdate: ({ data }) => render(data)
 *   });
 *
 *   // Call the returned function from the component or router teardown hook.
 *   return () => connection.disconnect();
 * }
 * ```
 */
export function connectLivePreview<Data extends object>({
	adminOrigin,
	target: expectedTarget,
	onUpdate,
	window: injectedWindow = window,
}: {
	/** Exact origin of the Ridu admin that opened or embedded this preview page. */
	adminOrigin: string;
	/** Resource identity that every accepted update must match. */
	target: LivePreviewTarget;
	/** Receives current unsaved form state after the message passes all checks. */
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
