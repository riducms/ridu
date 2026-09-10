export function fieldDescriptionID(controlID: string): string {
	return `${controlID}-description`;
}

export function fieldErrorID(controlID: string): string {
	return `${controlID}-error`;
}

export function fieldControlARIA(
	controlID: string,
	hasDescription: boolean,
	invalid: boolean
): {
	"aria-describedby": string | undefined;
	"aria-errormessage": string | undefined;
	"aria-invalid": boolean;
} {
	const describedBy = [
		hasDescription ? fieldDescriptionID(controlID) : undefined,
		invalid ? fieldErrorID(controlID) : undefined,
	]
		.filter((id): id is string => id !== undefined)
		.join(" ");

	return {
		"aria-describedby": describedBy === "" ? undefined : describedBy,
		"aria-errormessage": invalid ? fieldErrorID(controlID) : undefined,
		"aria-invalid": invalid,
	};
}
