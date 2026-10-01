import { expect, test } from "./fixture";
import { loginAsEditor } from "./helpers";

// A hidden collection is presentation only: the admin leaves it out of the
// navigation, the dashboard and search, and its pages show not-found, while
// the API serves it and relationships to it still render.
test("hidden collections stay out of the admin but keep their API and relationships", async ({
	page,
}) => {
	const navigation = await loginAsEditor(page);
	await expect(navigation.getByRole("link", { name: "Stat reports", exact: true })).toBeVisible();
	await expect(navigation.getByRole("link", { name: "Usage stats", exact: true })).toHaveCount(0);

	const main = page.getByRole("main");
	await expect(main.getByRole("link", { name: "Open Stat reports", exact: true })).toBeVisible();
	await expect(main.getByRole("link", { name: "Open Usage stats", exact: true })).toHaveCount(0);
	await expect(main.getByRole("heading", { name: "Usage stats", exact: true })).toHaveCount(0);

	await page.getByRole("button", { name: "Search and navigate", exact: true }).click();
	const search = page.getByRole("dialog");
	await expect(search.getByText("Stat reports", { exact: true }).first()).toBeVisible();
	await expect(search.getByText("Usage stats", { exact: true })).toHaveCount(0);
	await page.keyboard.press("Escape");

	const stat = await page.request.post("/api/collections/usage-stats", {
		data: { label: "Weekly visits" },
	});
	expect(stat.ok(), await stat.text()).toBe(true);
	const statID = (await stat.json()).doc.id as string;
	const listed = await page.request.get("/api/collections/usage-stats");
	expect(listed.ok(), await listed.text()).toBe(true);
	const report = await page.request.post("/api/collections/stat-reports", {
		data: { title: "October", stat: statID },
	});
	expect(report.ok(), await report.text()).toBe(true);
	const reportID = (await report.json()).doc.id as string;

	for (const path of [
		"/admin/collections/usage-stats",
		"/admin/Collections/usage-stats",
		`/admin/collections/usage-stats/${statID}`,
	]) {
		await page.goto(path);
		await expect(
			page.getByRole("heading", { name: "This admin page does not exist." })
		).toBeVisible();
		await expect(page.getByText("Plugin notFound view", { exact: true })).toBeVisible();
		await expect(page).toHaveTitle(/^Error 404 - /);
		await expect(page.getByRole("main")).not.toHaveClass(/ridu-shell__main--contained/);
		await expect(page.locator('a[href*="/collections/usage-stats"]')).toHaveCount(0);
	}

	await page.goto(`/admin/collections/stat-reports/${reportID}`);
	await expect(page.getByText("Weekly visits").first()).toBeVisible();
	await expect(page.locator(`a[href*="/collections/usage-stats/"]`)).toHaveCount(0);
});
