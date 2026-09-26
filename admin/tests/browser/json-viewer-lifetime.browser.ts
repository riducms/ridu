import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { tick } from "svelte";
import { afterEach, expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";

const Harness = svelte`
	<script>
		import { setAdminI18n } from "@riducms/plugin";
		import { createAdminI18n } from "@riducms/translations";
		import { TooltipProvider } from "@riducms/ui";
		import JsonViewer from "../../src/components/json-tree/json-viewer.svelte";
		setAdminI18n(createAdminI18n());
	</script>
	<TooltipProvider><JsonViewer value={{ title: "Example" }} /></TooltipProvider>
`;

afterEach(() => vi.restoreAllMocks());

it("does not schedule copy feedback after the viewer unmounts during clipboard write", async () => {
	const clipboard = Promise.withResolvers<void>();
	const write = vi.spyOn(navigator.clipboard, "writeText").mockReturnValue(clipboard.promise);
	const schedule = vi.spyOn(window, "setTimeout");
	const screen = await render(Harness);
	await screen.getByRole("button", { name: "Copy JSON" }).click();
	expect(write).toHaveBeenCalledWith('{\n  "title": "Example"\n}');
	await screen.unmount();
	const feedbackTimers = () => schedule.mock.calls.filter(([, delay]) => delay === 1_500).length;
	const before = feedbackTimers();

	clipboard.resolve();
	await clipboard.promise;
	await tick();
	expect(feedbackTimers()).toBe(before);
});

it("restarts the feedback timer when the same value is copied again", async () => {
	vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
	const schedule = vi.spyOn(window, "setTimeout");
	const cancel = vi.spyOn(window, "clearTimeout");
	const screen = await render(Harness);
	try {
		const copy = screen.getByRole("button", { name: "Copy JSON" });
		const feedbackTimers = () => schedule.mock.calls.filter(([, delay]) => delay === 1_500);
		await copy.click();
		await expect.poll(() => feedbackTimers().length).toBe(1);
		const firstIndex = schedule.mock.calls.findIndex(([, delay]) => delay === 1_500);
		const firstTimer = schedule.mock.results[firstIndex]?.value;

		await copy.click();
		await expect.poll(() => feedbackTimers().length).toBe(2);
		expect(cancel).toHaveBeenCalledWith(firstTimer);
	} finally {
		await screen.unmount();
	}
});
