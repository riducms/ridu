import { describe, expect, it } from "bun:test";

import {
	connectLivePreview,
	RIDU_LIVE_PREVIEW_CHANNEL_PARAM,
	RIDU_LIVE_PREVIEW_MESSAGE,
} from "../src";

describe("live preview receiver", () => {
	it("authenticates the parent origin, source window, and ephemeral channel", () => {
		const harness = createWindowHarness(
			`https://preview.example.test/article?${RIDU_LIVE_PREVIEW_CHANNEL_PARAM}=channel-1`
		);
		const updates: unknown[] = [];
		const connection = connectLivePreview({
			adminOrigin: "https://admin.example.test/path",
			target: { resource: "collection", slug: "posts", id: "post-1" },
			onUpdate: (message) => updates.push(message),
			window: harness.window,
		});

		expect(harness.posts).toEqual([
			{
				message: { type: RIDU_LIVE_PREVIEW_MESSAGE, ready: true, channel: "channel-1" },
				targetOrigin: "https://admin.example.test",
			},
		]);
		harness.message({
			origin: "https://evil.example.test",
			source: harness.parent,
			data: update("channel-1"),
		});
		harness.message({
			origin: "https://admin.example.test",
			source: {} as MessageEventSource,
			data: update("channel-1"),
		});
		harness.message({
			origin: "https://admin.example.test",
			source: harness.parent,
			data: update("wrong-channel"),
		});
		harness.message({
			origin: "https://admin.example.test",
			source: harness.parent,
			data: { ...update("channel-1"), id: "post-2" },
		});
		harness.message({
			origin: "https://admin.example.test",
			source: harness.parent,
			data: { ...update("channel-1"), resource: "global", slug: "settings" },
		});
		expect(updates).toEqual([]);

		const accepted = update("channel-1");
		harness.message({
			origin: "https://admin.example.test",
			source: harness.parent,
			data: accepted,
		});
		expect(updates).toEqual([accepted]);

		connection.ready();
		expect(harness.posts).toHaveLength(2);
		connection.disconnect();
		harness.message({
			origin: "https://admin.example.test",
			source: harness.parent,
			data: accepted,
		});
		expect(updates).toEqual([accepted]);
	});

	it("requires an explicit channel capability", () => {
		const harness = createWindowHarness("https://preview.example.test/article");
		expect(() =>
			connectLivePreview({
				adminOrigin: "https://admin.example.test",
				target: { resource: "collection", slug: "posts", id: "post-1" },
				onUpdate: () => undefined,
				window: harness.window,
			})
		).toThrow(RIDU_LIVE_PREVIEW_CHANNEL_PARAM);
	});
});

function update(channel: string) {
	return {
		type: RIDU_LIVE_PREVIEW_MESSAGE,
		channel,
		sequence: 1,
		resource: "collection" as const,
		slug: "posts",
		collection: "posts",
		id: "post-1",
		data: { title: "Draft" },
	};
}

function createWindowHarness(href: string) {
	type Listener = EventListenerOrEventListenerObject;
	const listeners = new Map<string, Set<Listener>>();
	const posts: { message: unknown; targetOrigin: string }[] = [];
	const parent = {
		postMessage(message: unknown, targetOrigin: string) {
			posts.push({ message, targetOrigin });
		},
	} as unknown as Window;
	const previewWindow = {
		location: { href },
		opener: null,
		parent,
		addEventListener(type: string, listener: Listener) {
			const entries = listeners.get(type) ?? new Set<Listener>();
			entries.add(listener);
			listeners.set(type, entries);
		},
		removeEventListener(type: string, listener: Listener) {
			listeners.get(type)?.delete(listener);
		},
	} as unknown as Window;
	return {
		window: previewWindow,
		parent,
		posts,
		message(init: Pick<MessageEvent, "origin" | "source" | "data">) {
			for (const listener of listeners.get("message") ?? []) {
				const event = { type: "message", ...init } as MessageEvent;
				if (typeof listener === "function") listener(event);
				else listener.handleEvent(event);
			}
		},
	};
}
