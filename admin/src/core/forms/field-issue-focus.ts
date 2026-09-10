import { tick } from "svelte";

export const fieldIssueRevealEvent = "ridu:reveal-field-issue";

/** Open lazy field disclosures and repeated rows before focusing the invalid control. */
export async function focusFieldIssue(path: string) {
	const selector = `[data-field-path="${CSS.escape(path)}"]`;
	let previous: HTMLElement | null = null;
	// Each reveal can mount the next schema boundary. Bound the search by the
	// path depth, and stop when a reveal no longer makes progress.
	for (let depth = 0; depth <= path.split(".").length; depth++) {
		const segments = path.split(".");
		let container: HTMLElement | null = null;
		while (segments.length > 0 && container === null) {
			container = document.querySelector<HTMLElement>(
				`[data-field-path="${CSS.escape(segments.join("."))}"]`
			);
			segments.pop();
		}
		if (container === null || container === previous) return;
		previous = container;
		for (
			let ancestor = container.parentElement;
			ancestor !== null;
			ancestor = ancestor.parentElement
		) {
			if (ancestor instanceof HTMLDetailsElement && ancestor.hasAttribute("data-field-collapsible"))
				ancestor.open = true;
		}
		container.dispatchEvent(new Event(fieldIssueRevealEvent, { bubbles: true }));
		await tick();
		const revealed = document.querySelector<HTMLElement>(selector);
		const control = revealed?.querySelector<HTMLElement>(
			"input:not([disabled]), textarea:not([disabled]), [contenteditable='true'], button:not([disabled]), [tabindex]:not([tabindex='-1'])"
		);
		if (revealed !== null && control != null) {
			revealed.scrollIntoView({ behavior: "instant", block: "center" });
			control.focus({ preventScroll: true });
			return;
		}
	}
}
