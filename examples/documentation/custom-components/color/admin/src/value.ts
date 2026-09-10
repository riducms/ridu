export type Color = `#${string}`;
// Reads and writes use the same value shape for this field.
export type ColorInput = Color;

// TypeScript's # prefix type does not check the six hex digits.
export function decodeColor(value: unknown): Color {
	if (typeof value !== 'string' || !/^#[0-9a-f]{6}$/i.test(value))
		throw new Error('Expected a six-digit hexadecimal color.');
	return value as Color;
}
