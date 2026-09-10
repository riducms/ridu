/** Keep unfinished input in the unsaved form so collapse/remount cannot discard it. */
export function primitiveNumberInput(input: string): number | string {
	if (!/^[+-]?(?:\d+(?:\.\d+)?|\.\d+)(?:[eE][+-]?\d+)?$/.test(input)) return input;
	const value = Number(input);
	return Number.isFinite(value) ? value : input;
}
