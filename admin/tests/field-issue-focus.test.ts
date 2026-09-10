import { expect, it } from "bun:test";
import { focusFieldIssue } from "../src/core/forms/field-issue-focus";

it("progressively reveals nested lazy rows before focusing the exact invalid field", async () => {
	let mounted = 0;
	let focused = false;
	const element = (reveal: () => void, control: object | null = null) => ({
		parentElement: null,
		dispatchEvent: reveal,
		scrollIntoView() {},
		querySelector: () => control,
	});
	const outer = element(() => {
		mounted = 1;
	});
	const inner = element(() => {
		mounted = 2;
	});
	const target = element(() => {}, {
		focus() {
			focused = true;
		},
	});
	const globals = {
		CSS: { escape: (value: string) => value },
		HTMLDetailsElement: class {},
		document: {
			querySelector(selector: string) {
				if (selector.includes("layout.0.entries.0.title")) return mounted === 2 ? target : null;
				if (selector.includes("layout.0.entries.0")) return mounted >= 1 ? inner : null;
				if (selector.includes("layout.0")) return outer;
				return null;
			},
		},
	};
	const previous = Object.fromEntries(
		Object.keys(globals).map((key) => [key, Object.getOwnPropertyDescriptor(globalThis, key)])
	);
	try {
		for (const [key, value] of Object.entries(globals))
			Object.defineProperty(globalThis, key, { configurable: true, value });
		await focusFieldIssue("layout.0.entries.0.title");
		expect(mounted).toBe(2);
		expect(focused).toBe(true);
	} finally {
		for (const [key, descriptor] of Object.entries(previous)) {
			if (descriptor === undefined) Reflect.deleteProperty(globalThis, key);
			else Object.defineProperty(globalThis, key, descriptor);
		}
	}
});
