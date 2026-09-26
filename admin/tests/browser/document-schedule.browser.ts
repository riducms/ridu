import { expect, it, vi } from "vitest";
import { userEvent } from "vitest/browser";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import type { DeleteEnvelope } from "@riducms/protocol";
import type { AdminClient, AdminScheduledPublication } from "@admin/core/api/admin-client";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";

const Harness = svelte`
 <script>
  import { setAdminI18n } from "@riducms/plugin";
  import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
  import { setNotificationCenter } from "../../src/core/notifications/notification-center.svelte";
  import DocumentSchedule from "../../src/features/documents/document-schedule.svelte";
  let { runtime, notifications, slug = "posts", documentID = "one", dirty = false, editable = true, status = "draft", canPublish = true, canUnpublish = true, close } = $props();
  setAdminRuntime(runtime); setAdminI18n(runtime.i18n); setNotificationCenter(notifications);
 </script>
 <DocumentSchedule {slug} {documentID} title="Post" revision={7} {dirty} {editable} {status} {canPublish} {canUnpublish} onClose={close} />
`;
const futureRunAt = new Date();
futureRunAt.setUTCFullYear(futureRunAt.getUTCFullYear() + 1);
const job: AdminScheduledPublication = {
	id: "job",
	action: "publish",
	documentId: "one",
	expectedRevision: 7,
	runAt: futureRunAt.toISOString(),
	attempts: 0,
	createdAt: "2026-09-21T12:00:00Z",
};
function fixture() {
	const client = {
		scheduledPublications: vi.fn(async () => [job]),
		cancelScheduledPublication: vi.fn(async () => undefined),
		schedulePublish: vi.fn(async () => job),
		scheduleUnpublish: vi.fn(async () => ({
			...job,
			id: "unpublish-job",
			action: "unpublish" as const,
		})),
	} as unknown as AdminClient;
	const runtime = new AdminRuntime(client);
	const notifications = new NotificationCenter();
	const success = vi.spyOn(notifications, "success").mockReturnValue(0);
	const close = vi.fn();
	return { client, runtime, notifications, success, close };
}
it("shows upcoming publication, cancels it through the SDK, and prevents scheduling unsaved edits", async () => {
	const props = fixture();
	const screen = await render(Harness, { ...props, dirty: true });
	const cancel = screen.getByRole("button", { name: "Cancel scheduled publish" });
	await expect.element(cancel).toBeVisible();
	await expect.element(screen.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
	await cancel.click();
	expect(props.client.cancelScheduledPublication).toHaveBeenCalledWith(
		"posts",
		"one",
		"job",
		expect.objectContaining({ signal: expect.any(AbortSignal) })
	);
	await expect.element(cancel).not.toBeInTheDocument();
	expect(props.success).toHaveBeenCalledOnce();
	await screen.unmount();
});

it("defaults a published document to scheduling unpublish", async () => {
	const props = fixture();
	const screen = await render(Harness, { ...props, status: "published" });
	await expect.element(screen.getByRole("radio", { name: "Unpublish" })).toBeChecked();
	await expect
		.element(screen.getByRole("radio", { name: "Publish", exact: true }))
		.not.toBeChecked();
	const publish = document.querySelector<HTMLElement>('[role="radio"][data-value="publish"]');
	const unpublish = document.querySelector<HTMLElement>('[role="radio"][data-value="unpublish"]');
	expect(publish?.getBoundingClientRect().top).toBe(unpublish?.getBoundingClientRect().top);
	await screen.unmount();
});

it("submits the selected unpublish action with the current revision", async () => {
	const props = fixture();
	const screen = await render(Harness, { ...props, status: "published" });
	const futureYear = String(new Date().getFullYear() + 1);
	for (const [segment, digits] of [
		["month", "10"],
		["day", "23"],
		["year", futureYear],
		["hour", "12"],
		["minute", "00"],
	] as const) {
		await screen.getByRole("spinbutton", { name: new RegExp(segment) }).click();
		await userEvent.keyboard(digits);
	}
	await screen.getByRole("button", { name: "Save", exact: true }).click();
	await expect.poll(() => props.client.scheduleUnpublish).toHaveBeenCalledOnce();
	expect(props.client.scheduleUnpublish).toHaveBeenCalledWith(
		"posts",
		"one",
		expect.any(Date),
		expect.objectContaining({ revision: 7, signal: expect.any(AbortSignal) })
	);
	await screen.unmount();
});

it("disables cancellation when the queued action is not permitted", async () => {
	const props = fixture();
	const screen = await render(Harness, {
		...props,
		status: "published",
		canPublish: false,
		canUnpublish: true,
	});
	const cancel = screen.getByRole("button", { name: "Cancel scheduled publish" });
	await expect.element(cancel).toBeDisabled();
	expect(props.client.cancelScheduledPublication).not.toHaveBeenCalled();
	await screen.unmount();
});

it("lets an author choose a date and time in one visit to the calendar inside the schedule drawer", async () => {
	const props = fixture();
	const screen = await render(Harness, { ...props, status: "published" });
	await screen.getByRole("button", { name: "Open calendar" }).click();
	const today = new Intl.DateTimeFormat("en", { dateStyle: "full" }).format(new Date());
	await screen.getByRole("button", { name: today, exact: true }).click();
	await expect.element(screen.getByRole("radio", { name: "3:30 PM", exact: true })).toBeVisible();
	await screen.getByRole("radio", { name: "3:30 PM", exact: true }).click();
	await expect.element(screen.getByRole("spinbutton", { name: /hour/ })).toHaveTextContent("03");
	const minute = screen.getByRole("spinbutton", { name: /minute/ });
	await minute.click();
	await expect.element(minute).toHaveFocus();
	await userEvent.keyboard("45");
	await expect.element(minute).toHaveTextContent("45");
	const trigger = screen.getByRole("button", { name: "Open calendar" });
	await trigger.click();
	await screen.getByRole("radio", { name: "4:00 PM", exact: true }).click();
	await expect.element(screen.getByRole("spinbutton", { name: /hour/ })).toHaveTextContent("04");
	await expect.element(trigger).toHaveAttribute("aria-expanded", "false");
	await expect.element(trigger).toHaveFocus();
	expect(props.close).not.toHaveBeenCalled();
	await screen.unmount();
});
it("aborts pending schedule requests and ignores their completion after unmount", async () => {
	const props = fixture();
	const pending = Promise.withResolvers<DeleteEnvelope>();
	vi.mocked(props.client.cancelScheduledPublication).mockReturnValue(pending.promise);
	const screen = await render(Harness, props);
	await screen.getByRole("button", { name: "Cancel scheduled publish" }).click();
	const signal = vi.mocked(props.client.cancelScheduledPublication).mock.calls[0]?.[3]?.signal;
	await screen.unmount();
	expect(signal?.aborted).toBe(true);
	pending.resolve({ id: "job", deleted: true });
	await pending.promise;
	expect(props.success).not.toHaveBeenCalled();
});

it("waits for the current schedule before allowing a new publication", async () => {
	const props = fixture();
	const pending = Promise.withResolvers<AdminScheduledPublication[]>();
	vi.mocked(props.client.scheduledPublications).mockReturnValue(pending.promise);
	const screen = await render(Harness, props);
	const save = screen.getByRole("button", { name: "Save", exact: true });

	await expect.element(save).toBeDisabled();
	pending.resolve([job]);
	await expect.element(save).toBeEnabled();
	await expect
		.element(screen.getByRole("button", { name: "Cancel scheduled publish" }))
		.toBeVisible();
	await screen.unmount();
});

it("replaces the request lifetime when the document identity changes", async () => {
	const props = fixture();
	const first = Promise.withResolvers<AdminScheduledPublication[]>();
	vi.mocked(props.client.scheduledPublications)
		.mockImplementationOnce(() => first.promise)
		.mockResolvedValueOnce([{ ...job, id: "job-two", documentId: "two" }]);
	const screen = await render(Harness, props);
	await expect.poll(() => props.client.scheduledPublications).toHaveBeenCalledOnce();
	const firstSignal = vi.mocked(props.client.scheduledPublications).mock.calls[0]?.[2]?.signal;

	await screen.rerender({ ...props, documentID: "two" });
	await expect.poll(() => props.client.scheduledPublications).toHaveBeenCalledTimes(2);
	const secondSignal = vi.mocked(props.client.scheduledPublications).mock.calls[1]?.[2]?.signal;
	expect(firstSignal?.aborted).toBe(true);
	expect(secondSignal?.aborted).toBe(false);
	await expect
		.element(screen.getByRole("button", { name: "Cancel scheduled publish" }))
		.toBeVisible();

	first.resolve([job]);
	await first.promise;
	await screen.unmount();
});

it("searches timezones, preserves the scheduled instant across date rollover, and saves the chosen zone", async () => {
	const props = fixture();
	props.runtime.i18n.configure({
		languages: [{ code: "en", label: "English" }],
		defaultLanguage: "en",
		defaultTimeZone: "UTC",
		timeZones: [
			{ id: "UTC", label: "UTC" },
			{ id: "America/New_York", label: "Eastern Time" },
			{ id: "Asia/Kolkata", label: "India" },
		],
	});
	vi.mocked(props.client.scheduledPublications).mockResolvedValue([]);
	const screen = await render(Harness, props);
	const year = new Date().getUTCFullYear() + 2;
	for (const [segment, digits] of [
		["month", "01"],
		["day,", "15"],
		["year", String(year)],
		["hour", "11"],
		["minute", "30"],
		["AM/PM", "p"],
	]) {
		await screen.getByRole("spinbutton", { name: new RegExp(segment!) }).click();
		await userEvent.keyboard(digits!);
	}
	const zone = screen.getByRole("combobox", { name: "Timezone", exact: true });
	await expect
		.poll(() => (zone.element() as HTMLInputElement).value)
		.toMatch(/^\(UTC\+00:00\) UTC(?: \(.+\))?$/);
	await zone.fill("UTC+05:30");
	await expect
		.element(screen.getByRole("option", { name: /^\(UTC\+05:30\) India \(.+\)$/ }))
		.toBeVisible();
	await zone.fill("Kolkata");
	await screen.getByRole("option", { name: /^\(UTC\+05:30\) India \(.+\)$/ }).click();
	await expect
		.poll(() => (zone.element() as HTMLInputElement).value)
		.toMatch(/^\(UTC\+05:30\) India \(.+\)$/);
	await expect.element(screen.getByRole("spinbutton", { name: /day,/ })).toHaveTextContent("16");
	await expect.element(screen.getByRole("spinbutton", { name: /hour/ })).toHaveTextContent("05");
	const instant = new Date(`${year}-01-15T23:30:00.000Z`);
	vi.mocked(props.client.schedulePublish).mockResolvedValue({
		...job,
		runAt: instant.toISOString(),
		timeZone: "Asia/Kolkata",
	});
	await screen.getByRole("button", { name: "Save", exact: true }).click();
	await expect.poll(() => props.client.schedulePublish).toHaveBeenCalledOnce();
	expect(props.client.schedulePublish).toHaveBeenCalledWith(
		"posts",
		"one",
		instant,
		expect.objectContaining({ revision: 7, timeZone: "Asia/Kolkata" })
	);
	expect(props.runtime.i18n.timeZone).toBe("UTC");
	await expect.element(screen.getByText(/^\(UTC\+05:30\) India \(.+\)$/)).toBeVisible();
	await screen.unmount();
	// A newly loaded event must render in its own saved timezone.
	vi.mocked(props.client.scheduledPublications).mockResolvedValue([
		{ ...job, runAt: instant.toISOString(), timeZone: "Asia/Kolkata" },
	]);
	const reopened = await render(Harness, props);
	const displayed = props.runtime.i18n.formatDate(instant, {
		dateStyle: "long",
		timeStyle: "short",
		timeZone: "Asia/Kolkata",
	});
	await expect.element(reopened.getByText(displayed, { exact: false })).toBeVisible();
	await expect.element(reopened.getByText(/^\(UTC\+05:30\) India \(.+\)$/)).toBeVisible();
	await reopened.unmount();
});

it("rejects a wall time that repeats when daylight saving ends", async () => {
	const props = fixture();
	props.runtime.i18n.configure({
		languages: [{ code: "en", label: "English" }],
		defaultLanguage: "en",
		defaultTimeZone: "Europe/London",
		timeZones: [{ id: "Europe/London", label: "London" }],
	});
	vi.mocked(props.client.scheduledPublications).mockResolvedValue([]);
	const screen = await render(Harness, props);
	const year = new Date().getUTCFullYear() + 2;
	const endOfOctober = new Date(Date.UTC(year, 10, 0));
	const transitionDay = endOfOctober.getUTCDate() - endOfOctober.getUTCDay();
	for (const [segment, digits] of [
		["month", "10"],
		["day,", String(transitionDay).padStart(2, "0")],
		["year", String(year)],
		["hour", "01"],
		["minute", "30"],
		["AM/PM", "a"],
	] as const) {
		await screen.getByRole("spinbutton", { name: new RegExp(segment) }).click();
		await userEvent.keyboard(digits);
	}
	await screen.getByRole("button", { name: "Save", exact: true }).click();
	await expect
		.element(
			screen.getByText(
				"Choose a time that occurs only once in this timezone; this time repeats when the clocks change.",
				{ exact: true }
			)
		)
		.toBeVisible();
	expect(props.client.schedulePublish).not.toHaveBeenCalled();
	await screen.unmount();
});

it("supports timezone keyboard selection, clearing, and disabled schedule fields", async () => {
	const props = fixture();
	vi.mocked(props.client.scheduledPublications).mockResolvedValue([]);
	props.runtime.i18n.configure({
		languages: [{ code: "en", label: "English" }],
		defaultLanguage: "en",
		defaultTimeZone: "UTC",
		timeZones: [
			{ id: "UTC", label: "UTC" },
			{ id: "Europe/London", label: "London" },
		],
	});
	const screen = await render(Harness, props);
	await expect.element(screen.getByRole("button", { name: "Close", exact: true })).toHaveFocus();
	const zone = screen.getByRole("combobox", { name: "Timezone", exact: true });
	await zone.fill("London");
	const londonOption = screen.getByRole("option", {
		name: /^\(UTC\+0[01]:00\) London \(.+\)$/,
	});
	await expect.element(londonOption).toBeVisible();
	await userEvent.keyboard("{ArrowDown}");
	await expect.element(londonOption).toHaveAttribute("aria-selected", "true");
	await expect.element(zone).toHaveAttribute("aria-activedescendant", londonOption.element().id);
	await userEvent.keyboard("{Enter}");
	await expect.element(zone).toHaveAttribute("aria-expanded", "false");
	for (const [segment, digits] of [
		["month,", "01"],
		["day,", "15"],
		["year,", String(new Date().getUTCFullYear() + 2)],
		["hour,", "12"],
		["minute,", "00"],
	]) {
		await screen.getByRole("spinbutton", { name: segment, exact: true }).click();
		await userEvent.keyboard(digits!);
	}
	await expect
		.poll(() => (zone.element() as HTMLInputElement).value)
		.toMatch(/^\(UTC\+00:00\) London \(.+\)$/);
	await screen.getByRole("spinbutton", { name: "month,", exact: true }).click();
	await userEvent.keyboard("07");
	await expect
		.poll(() => (zone.element() as HTMLInputElement).value)
		.toMatch(/^\(UTC\+01:00\) London \(.+\)$/);
	await zone.fill("no-such-time-zone");
	await expect.element(screen.getByRole("option")).not.toBeInTheDocument();
	await userEvent.keyboard("{Escape}");
	await expect
		.poll(() => (zone.element() as HTMLInputElement).value)
		.toMatch(/^\(UTC\+01:00\) London \(.+\)$/);
	await screen.getByRole("button", { name: "Clear selection", exact: true }).nth(1).click();
	await expect.element(zone).toHaveValue("");
	await expect.element(zone).toHaveFocus();
	await screen.getByRole("button", { name: "Save", exact: true }).click();
	await expect.poll(() => props.client.schedulePublish).toHaveBeenCalledOnce();
	expect(props.client.schedulePublish).toHaveBeenCalledWith(
		"posts",
		"one",
		expect.any(Date),
		expect.objectContaining({
			timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone,
		})
	);
	await screen.rerender({ ...props, dirty: true });
	await expect.element(zone).toBeDisabled();
	await screen.unmount();
});
