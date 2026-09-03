import { expect, test } from "./fixture";

import { loginAsEditor } from "./helpers";

test("metadata-only root and populated selections omit authored PostgreSQL values", async ({
	page,
}) => {
	await loginAsEditor(page);

	const metadataSelect = encodeURIComponent(JSON.stringify({ id: true }));
	const rootResponse = await page.request.get(
		`/api/collections/posts?limit=1&select=${metadataSelect}`
	);
	expect(rootResponse.ok()).toBe(true);
	const root = (await rootResponse.json()) as {
		docs: Array<Record<string, unknown>>;
	};
	expect(root.docs).toHaveLength(1);
	expect(root.docs[0]?.id).toBeTruthy();
	expect(root.docs[0]?.title).toBeUndefined();

	const authorSelect = encodeURIComponent(JSON.stringify({ author: true }));
	const populate = encodeURIComponent(JSON.stringify({ author: { select: { id: true } } }));
	const populationResponse = await page.request.get(
		`/api/collections/posts?limit=1&select=${authorSelect}&populate=${populate}`
	);
	expect(populationResponse.ok()).toBe(true);
	const population = (await populationResponse.json()) as {
		docs: Array<{ author?: Record<string, unknown> }>;
	};
	const author = population.docs[0]?.author;
	expect(author?.id).toBeTruthy();
	expect(author?.email).toBeUndefined();
	expect(author?.roles).toBeUndefined();
});
