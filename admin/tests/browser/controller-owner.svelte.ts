import { afterEach } from "vitest";

const owners = new Set<() => void>();
afterEach(() => {
	for (const dispose of owners) dispose();
	owners.clear();
});

export function ownController<Value, Arguments extends unknown[]>(
	Controller: new (...args: Arguments) => Value,
	...args: Arguments
): Value {
	let controller!: Value;
	const dispose = $effect.root(() => {
		controller = new Controller(...args);
	});
	owners.add(dispose);
	return controller;
}
