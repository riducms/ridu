import { describe, expect, it } from 'bun:test';
import { normalizeSource } from './normalize-source';

describe('normalizeSource', () => {
	it('normalizes newlines and removes blank template-literal edges', () => {
		expect(normalizeSource('\r\n  first\r  second\r\n\t\r\n')).toBe('first\nsecond');
	});

	it('removes only the common indentation and trailing whitespace', () => {
		const source = '\n\t\troot  \n\t\t  child\n\n\t\tend\t\n\t';

		expect(normalizeSource(source)).toBe('root\n  child\n\nend');
	});

	it('expands nested tabs after removing common indentation', () => {
		expect(normalizeSource('\n\troot\n\t\tchild\n')).toBe('root\n  child');
		expect(normalizeSource('\n\troot\n\t\tchild\n', 4)).toBe('root\n    child');
	});

	it('preserves indentation when a tab crosses the common indent', () => {
		expect(normalizeSource('\tchild\n root')).toBe(' child\nroot');
	});

	it('handles empty and whitespace-only values', () => {
		expect(normalizeSource('')).toBe('');
		expect(normalizeSource('\n \t\n')).toBe('');
	});

	it('rejects invalid tab widths', () => {
		expect(() => normalizeSource('code', 0)).toThrow(RangeError);
		expect(() => normalizeSource('code', 1.5)).toThrow('tabWidth must be a positive integer');
	});
});
