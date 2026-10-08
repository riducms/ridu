export type LengthStatus = "missing" | "tooShort" | "almostThere" | "good" | "tooLong";

export interface LengthState {
	status: LengthStatus;
	length: number;
	progress: number;
	remaining: number;
}

export function lengthState(text: string, minLength: number, maxLength: number): LengthState {
	const length = text.length;
	if (length === 0) return { status: "missing", length, progress: 0, remaining: minLength };
	if (length < minLength) {
		const progress = Math.min(1, length / minLength);
		return {
			status: progress > 0.9 ? "almostThere" : "tooShort",
			length,
			progress,
			remaining: minLength - length,
		};
	}
	if (length <= maxLength) {
		const range = Math.max(1, maxLength - minLength);
		return {
			status: "good",
			length,
			progress: Math.min(1, (length - minLength) / range),
			remaining: maxLength - length,
		};
	}
	return { status: "tooLong", length, progress: 1, remaining: length - maxLength };
}
