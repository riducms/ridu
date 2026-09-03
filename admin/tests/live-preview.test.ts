import { describe, expect, test } from "bun:test";

import {
	addLivePreviewChannel,
	addLivePreviewToken,
	livePreviewTargetOrigin,
	resolveLivePreviewURL,
} from "../src/features/documents/live-preview";

describe("live preview", () => {
	test("resolves document and draft-field placeholders against the admin origin", () => {
		const url = resolveLivePreviewURL(
			{ url: "/preview/{collection}/{id}?slug={field:seo.slug}" },
			{
				collection: "posts",
				documentID: "posts_10",
				values: { seo: { slug: "hello world" } },
			},
			"http://127.0.0.1:18081/admin/collections/posts/posts_10"
		);
		expect(url).toBe("http://127.0.0.1:18081/preview/posts/posts_10?slug=hello%20world");
		expect(livePreviewTargetOrigin(url)).toBe("http://127.0.0.1:18081");
	});

	test("adds a server-rendered preview capability without replacing the browser channel", () => {
		const url = addLivePreviewToken(
			addLivePreviewChannel("https://preview.example.test/posts/post-1", "channel-1"),
			"scoped-secret"
		);
		expect(url).toBe(
			"https://preview.example.test/posts/post-1?__ridu_preview=channel-1&__ridu_preview_token=scoped-secret"
		);
	});

	test("leaves unsupported values empty instead of serializing objects", () => {
		const url = resolveLivePreviewURL(
			{ url: "https://preview.example/{field:metadata}" },
			{ collection: "posts", documentID: "1", values: { metadata: { private: true } } },
			"http://localhost/admin"
		);
		expect(url).toBe("https://preview.example/");
	});

	test("adds an ephemeral channel without discarding preview query parameters", () => {
		const url = addLivePreviewChannel(
			"https://preview.example.test/posts/post-1?slug=hello",
			"channel-1"
		);
		expect(url).toBe(
			"https://preview.example.test/posts/post-1?slug=hello&__ridu_preview=channel-1"
		);
		expect(livePreviewTargetOrigin(url)).toBe("https://preview.example.test");
	});
});
