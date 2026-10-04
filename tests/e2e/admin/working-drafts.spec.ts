import { expect, test } from "./fixture";
import { loginAsEditor, observePageErrors } from "./helpers";

test("an incomplete autosaved draft becomes live only on publish and can later be discarded", async ({
	page,
	request,
}) => {
	test.setTimeout(180_000);
	await page.clock.install();
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const title = page.getByLabel("Title — English", { exact: true });
	const summary = page.getByLabel("Summary — English", { exact: true });
	const initialTitle = "Working draft browser journey";
	const pendingTitle = "Unpublished working revision";

	await page.goto("/admin/collections/posts/create?locale=en");
	const autosavedCreation = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === "/api/collections/posts"
	);
	await title.fill(initialTitle);
	await expect(summary).toHaveValue("");
	await page.clock.fastForward(15_000);
	expect((await autosavedCreation).status()).toBe(201);
	// Let the router's scheduled transition run after the asynchronous save returns.
	await page.clock.runFor(100);
	await expect(page).toHaveURL(/\/admin\/collections\/posts\/(?!create(?:\?|$))[^/?]+\?locale=en$/);
	const id = new URL(page.url()).pathname.split("/").at(-1);
	expect(id).toBeTruthy();
	const endpoint = `/api/collections/posts/${id}?locale=en`;
	const workingEndpoint = `${endpoint}&draft=true`;
	const publicEndpoint = `${endpoint}&draft=false`;

	const incompleteResponse = await page.request.get(workingEndpoint);
	expect(incompleteResponse.ok(), await incompleteResponse.text()).toBe(true);
	const incomplete = (await incompleteResponse.json()).doc;
	expect(incomplete).toMatchObject({ title: initialTitle, _status: "draft" });
	expect(incomplete.summary ?? "").toBe("");
	expect((await request.get(publicEndpoint)).status()).toBe(404);

	await page.reload();
	await expect(title).toHaveValue(initialTitle);
	await expect(summary).toHaveValue("");
	await page.getByRole("button", { name: "Publish changes", exact: true }).click();
	await expect(summary).toHaveAttribute("aria-invalid", "true");
	const rejection = await page.request.post(`/api/collections/posts/${id}/publish`, {
		headers: { "If-Match": String(incomplete._revision) },
	});
	expect(rejection.status()).toBe(422);
	expect((await rejection.json()).error).toMatchObject({
		code: "validation",
		issues: expect.arrayContaining([
			expect.objectContaining({ code: "required", path: "summary" }),
		]),
	});
	expect((await request.get(publicEndpoint)).status()).toBe(404);
	await expect(title).toHaveValue(initialTitle);

	await summary.fill("The complete introduction makes publication valid.");
	await page.getByRole("combobox", { name: "Status", exact: true }).click();
	await page.getByRole("option", { name: "Published", exact: true }).click();
	const successfulPublish = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === `/api/collections/posts/${id}/publish`
	);
	await page.getByRole("button", { name: "Publish changes", exact: true }).click();
	expect((await successfulPublish).ok()).toBe(true);
	const liveResponse = await request.get(publicEndpoint);
	expect(liveResponse.ok(), await liveResponse.text()).toBe(true);
	const live = (await liveResponse.json()).doc;
	expect(live).toMatchObject({ title: initialTitle, _status: "published" });
	expect(live._hasDraftChanges).toBeUndefined();

	await title.fill(pendingTitle);
	await page.clock.fastForward(15_000);
	await expect
		.poll(async () => {
			const response = await page.request.get(workingEndpoint);
			return response.ok() ? (await response.json()).doc : undefined;
		})
		.toMatchObject({
			title: pendingTitle,
			_status: "published",
			_hasDraftChanges: true,
		});
	const stillLiveResponse = await request.get(publicEndpoint);
	expect(stillLiveResponse.ok(), await stillLiveResponse.text()).toBe(true);
	expect((await stillLiveResponse.json()).doc).toMatchObject({
		title: initialTitle,
		_revision: live._revision,
		_status: "published",
	});

	await page.reload();
	await expect(title).toHaveValue(pendingTitle);
	await expect(
		page.getByText("Saved draft changes pending publication", { exact: true })
	).toBeVisible();
	await page.getByRole("button", { name: "More actions", exact: true }).click();
	await page.getByRole("button", { name: "Discard saved draft", exact: true }).click();
	const confirmation = page.getByRole("dialog", { name: "Discard the saved draft?" });
	await expect(confirmation).toContainText("The published version will stay live.");
	const discardResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === `/api/collections/posts/${id}/discard-draft`
	);
	await confirmation.getByRole("button", { name: "Discard saved draft", exact: true }).click();
	expect((await discardResponse).ok()).toBe(true);
	await expect(title).toHaveValue(initialTitle);
	await expect(
		page.getByText("Saved draft changes pending publication", { exact: true })
	).toHaveCount(0);
	const resetResponse = await page.request.get(workingEndpoint);
	expect(resetResponse.ok(), await resetResponse.text()).toBe(true);
	expect((await resetResponse.json()).doc).toMatchObject({
		title: initialTitle,
		_status: "published",
		_hasDraftChanges: false,
	});
	const publicAfterDiscard = await request.get(publicEndpoint);
	expect(publicAfterDiscard.ok(), await publicAfterDiscard.text()).toBe(true);
	expect((await publicAfterDiscard.json()).doc).toMatchObject({
		title: initialTitle,
		_revision: live._revision,
		_status: "published",
	});
	expect(errors.pageErrors).toEqual([]);
});
