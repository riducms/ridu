import { expect, it } from "vitest";
import { userEvent } from "vitest/browser";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { createAdminI18n, en } from "@riducms/translations";

const Harness = svelte`
 <script>
  import { setAdminI18n } from "@riducms/plugin";
  import DateValueControl from "../../src/components/ui/date-value-control/date-value-control.svelte";
  let { i18n, value: initial, readOnly = false, timeZone } = $props();
  // svelte-ignore state_referenced_locally
  let value = $state(initial);
  setAdminI18n(i18n);
 </script>
 <DateValueControl id="publish-time" label="Publish time" appearance="date-time" {timeZone} {value} readonly={readOnly} onValueChange={(next) => value = next} />
 <output>{value}</output>
`;
it("keeps the date when a time is selected, converts through the configured zone, and clears", async () => {
	const i18n = createAdminI18n({ languages: [en], language: "en", timeZone: "America/New_York" });
	const screen = await render(Harness, { i18n, value: "2026-09-21T16:00:00Z" });
	await screen.getByRole("button", { name: "Open calendar" }).click();
	await expect
		.element(screen.getByRole("button", { name: "Monday, September 21, 2026", exact: true }))
		.toHaveFocus();
	await expect
		.element(screen.getByRole("radio", { name: "12:00 PM", exact: true }))
		.toHaveAttribute("aria-checked", "true");
	await userEvent.keyboard("{ArrowRight}{Enter}");
	await screen.getByRole("radio", { name: "3:30 PM", exact: true }).click();
	await expect.element(screen.getByRole("status")).toHaveTextContent("2026-09-22T19:30:00.000Z");
	await screen.getByRole("button", { name: "Open calendar" }).click();
	await screen.getByRole("button", { name: "Clear selection" }).click();
	await expect.element(screen.getByRole("status")).toHaveTextContent("");
	await screen.unmount();
});
it("read-only dates expose neither a clear action nor an enabled calendar", async () => {
	const i18n = createAdminI18n({ languages: [en], language: "en" });
	const screen = await render(Harness, { i18n, value: "2026-09-21T16:00:00Z", readOnly: true });
	await expect.element(screen.getByRole("button", { name: "Open calendar" })).toBeDisabled();
	await expect
		.element(screen.getByRole("button", { name: "Clear selection" }))
		.not.toBeInTheDocument();
	await screen.unmount();
});

it("keeps the picker open after choosing a date with the pointer so a time can be selected", async () => {
	const i18n = createAdminI18n({ languages: [en], language: "en", timeZone: "America/New_York" });
	const screen = await render(Harness, { i18n, value: "2026-09-21T16:00:00Z" });
	await screen.getByRole("button", { name: "Open calendar" }).click();
	await screen.getByRole("button", { name: "Tuesday, September 22, 2026", exact: true }).click();
	await expect.element(screen.getByRole("radio", { name: "3:30 PM", exact: true })).toBeVisible();
	await screen.getByRole("radio", { name: "3:30 PM", exact: true }).click();
	await expect.element(screen.getByRole("status")).toHaveTextContent("2026-09-22T19:30:00.000Z");
	const minute = screen.getByRole("spinbutton", { name: /minute/ });
	await minute.click();
	await expect.element(minute).toHaveFocus();
	await userEvent.keyboard("45");
	await expect.element(screen.getByRole("status")).toHaveTextContent("2026-09-22T19:45:00.000Z");
	const trigger = screen.getByRole("button", { name: "Open calendar" });
	await expect.element(trigger).toHaveAttribute("aria-expanded", "false");
	await trigger.click();
	await screen.getByRole("radio", { name: "4:00 PM", exact: true }).click();
	await expect.element(screen.getByRole("status")).toHaveTextContent("2026-09-22T20:00:00.000Z");
	await userEvent.keyboard("{Escape}");
	await expect.element(trigger).toHaveAttribute("aria-expanded", "false");
	await expect.element(trigger).toHaveFocus();
	await screen.unmount();
});

it("changes the display timezone without changing the instant, then edits in the chosen zone", async () => {
	const i18n = createAdminI18n({ languages: [en], language: "en", timeZone: "UTC" });
	const value = "2027-03-28T23:30:00.000Z";
	const screen = await render(Harness, { i18n, value, timeZone: "Europe/London" });
	await expect.element(screen.getByRole("spinbutton", { name: /day,/ })).toHaveTextContent("29");
	await screen.rerender({ i18n, value, timeZone: "America/New_York" });
	await expect.element(screen.getByRole("spinbutton", { name: /day,/ })).toHaveTextContent("28");
	await expect.element(screen.getByRole("spinbutton", { name: /hour/ })).toHaveTextContent("07");
	await expect.element(screen.getByRole("status")).toHaveTextContent(value);
	await screen.getByRole("button", { name: "Open calendar" }).click();
	await screen.getByRole("radio", { name: "8:00 PM", exact: true }).click();
	await expect.element(screen.getByRole("status")).toHaveTextContent("2027-03-29T00:00:00.000Z");
	await screen.unmount();
});
