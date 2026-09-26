import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { userEvent } from "vitest/browser";
import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";

const Viewer = svelte`
	<script>
		import { setAdminI18n } from "@riducms/plugin";
		import { createAdminI18n } from "@riducms/translations";
		import JsonViewer from "../../src/components/json-tree/json-viewer.svelte";
		let { value } = $props();
		setAdminI18n(createAdminI18n());
	</script>
	<JsonViewer {value} />
`;

it("renders objects, arrays, empty containers and escaped values as valid JSON", async () => {
	const value = {
		'quoted"key': 'A "quote"\nand a newline',
		items: [{ enabled: true, score: 2.5 }, { nested: [] }, "entry", null, false],
		emptyArray: [],
		emptyObject: {},
	};
	const screen = await render(Viewer, { value });
	try {
		const data = screen.getByRole("region", { name: "JSON data" }).element();
		expect(JSON.parse(data.textContent ?? "")).toEqual(value);
		await expect
			.element(screen.getByRole("button", { name: "emptyArray" }))
			.not.toBeInTheDocument();

		const items = screen.getByRole("button", { name: "items", exact: true });
		await expect.element(screen.getByRole("button", { name: "[0]", exact: true })).toBeVisible();
		await expect.element(screen.getByRole("button", { name: "[1]", exact: true })).toBeVisible();
		await items.click();
		await expect.element(items).toHaveAttribute("aria-expanded", "false");
		await expect
			.element(screen.getByRole("button", { name: "[0]", exact: true }))
			.not.toBeInTheDocument();
		await expect.element(screen.getByText('"entry"', { exact: true })).not.toBeInTheDocument();
		await userEvent.keyboard("{Enter}");
		await expect.element(screen.getByText('"entry"', { exact: true })).toBeVisible();
		await userEvent.keyboard(" ");
		await expect.element(items).toHaveAttribute("aria-expanded", "false");
		expect(document.activeElement).toBe(items.element());

		await screen.rerender({ value: { ...value, other: "updated" } });
		await expect.element(items).toHaveAttribute("aria-expanded", "false");
	} finally {
		await screen.unmount();
	}
});

it("announces only the latest clipboard result and does not attribute it to new data", async () => {
	const first = Promise.withResolvers<void>();
	const second = Promise.withResolvers<void>();
	const third = Promise.withResolvers<void>();
	const writeText = vi
		.spyOn(navigator.clipboard, "writeText")
		.mockReturnValueOnce(first.promise)
		.mockReturnValueOnce(second.promise)
		.mockReturnValueOnce(third.promise);
	const screen = await render(Viewer, { value: { title: "First" } });
	try {
		const copy = screen.getByRole("button", { name: "Copy JSON" });
		await copy.click();
		await copy.click();
		second.resolve();
		await second.promise;
		await expect.element(screen.getByRole("status").filter({ hasText: "Copied" })).toBeVisible();

		first.reject(new Error("Old clipboard failure"));
		await first.promise.catch(() => undefined);
		await expect.element(screen.getByRole("status").filter({ hasText: "Copied" })).toBeVisible();

		await copy.click();
		await screen.rerender({ value: { title: "Second" } });
		third.resolve();
		await third.promise;
		await expect
			.element(screen.getByRole("status").filter({ hasText: "Copied" }))
			.not.toBeInTheDocument();
	} finally {
		writeText.mockRestore();
		await screen.unmount();
	}
});

it.each([null, true, 42, "text", [], {}])("handles a root JSON value: %j", async (value) => {
	const screen = await render(Viewer, { value });
	try {
		expect(
			JSON.parse(screen.getByRole("region", { name: "JSON data" }).element().textContent ?? "")
		).toEqual(value);
	} finally {
		await screen.unmount();
	}
});
