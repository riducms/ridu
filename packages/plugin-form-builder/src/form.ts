import type {
	CustomFormField,
	Confirmation,
	FormDefinition,
	FormField,
	FormFieldDefinition,
	FormSubmissionInput,
	FormValidationIssue,
	FormValues,
	PriceCondition,
	UploadReference,
} from "./types";

type FormInputField = Exclude<FormField, { blockType: "message" }> | CustomFormField;

export function buildSubmissionInput(
	form: FormDefinition<FormFieldDefinition>,
	values: FormValues
): FormSubmissionInput {
	const submissionData: FormSubmissionInput["submissionData"] = [];
	const submissionUploads: NonNullable<FormSubmissionInput["submissionUploads"]> = [];
	for (const field of form.fields ?? []) {
		if (!isInputField(field)) continue;
		const value = values[field.name];
		if (value === undefined || value === null) continue;
		if (field.blockType === "upload") {
			if (isUploadReferences(value) && value.length > 0) {
				submissionUploads.push({ field: field.name, value: [...value] });
			}
			continue;
		}
		submissionData.push({ field: field.name, value });
	}
	return submissionUploads.length > 0
		? { form: form.id, submissionData, submissionUploads }
		: { form: form.id, submissionData };
}

export function validateFormValues(
	form: FormDefinition<FormFieldDefinition>,
	values: FormValues
): FormValidationIssue[] {
	const issues: FormValidationIssue[] = [];
	for (const field of form.fields ?? []) {
		if (!isInputField(field)) continue;
		const value = values[field.name];
		if (field.required && isMissing(field, value)) {
			issues.push({
				code: "required",
				field: field.name,
				message: `${field.label || field.name} is required.`,
			});
			continue;
		}
		if (value === undefined || value === null || value === "") continue;
		if (field.blockType === "email" && (typeof value !== "string" || !isEmail(value))) {
			issues.push({ code: "invalid", field: field.name, message: "Enter a valid email address." });
		}
		if (field.blockType === "number" || field.blockType === "payment") {
			if (typeof value !== "number" || !Number.isFinite(value)) {
				issues.push({ code: "invalid", field: field.name, message: "Enter a valid number." });
			}
		}
		if (field.blockType === "select" || field.blockType === "radio") {
			const options = "options" in field && Array.isArray(field.options) ? field.options : [];
			if (
				typeof value !== "string" ||
				!options.some(
					(option) =>
						typeof option === "object" &&
						option !== null &&
						"value" in option &&
						option.value === value
				)
			) {
				issues.push({ code: "invalid", field: field.name, message: "Choose an available option." });
			}
		}
		if (field.blockType === "upload" && !isUploadReferences(value)) {
			issues.push({ code: "invalid", field: field.name, message: "Choose a valid uploaded file." });
		}
	}
	return issues;
}

export function confirmationFor(form: FormDefinition<FormFieldDefinition>): Confirmation {
	return form.confirmationType === "redirect"
		? { type: "redirect", redirect: form.redirect ?? {} }
		: { type: "message", message: form.confirmationMessage ?? "" };
}

export function getPaymentTotal(
	basePrice: number,
	conditions: readonly PriceCondition[],
	values: FormValues
): number {
	if (!Number.isFinite(basePrice)) throw new TypeError("basePrice must be finite");
	let total = basePrice;
	for (const condition of conditions) {
		const source = values[condition.fieldToUse];
		if (!conditionMatches(source, condition)) continue;
		const operandSource =
			condition.valueType === "valueOfField"
				? values[condition.valueForOperator]
				: condition.valueForOperator;
		const operand = typeof operandSource === "number" ? operandSource : Number(operandSource);
		if (!Number.isFinite(operand)) throw new TypeError("price condition operand must be finite");
		switch (condition.operator) {
			case "add":
				total += operand;
				break;
			case "subtract":
				total -= operand;
				break;
			case "multiply":
				total *= operand;
				break;
			case "divide":
				if (operand === 0) throw new RangeError("price condition cannot divide by zero");
				total /= operand;
				break;
		}
		if (!Number.isFinite(total))
			throw new RangeError("price condition produced a non-finite total");
	}
	return total;
}

function isInputField(field: FormFieldDefinition): field is FormInputField {
	return "name" in field && typeof field.name === "string";
}

function isMissing(field: FormInputField, value: FormValues[string]) {
	if (field.blockType === "checkbox") return value !== true;
	if (field.blockType === "upload") return !isUploadReferences(value) || value.length === 0;
	return value === undefined || value === null || value === "";
}

function isUploadReferences(value: unknown): value is UploadReference[] {
	return (
		Array.isArray(value) &&
		value.every((reference) =>
			typeof reference === "string"
				? reference.length > 0
				: typeof reference === "object" &&
					reference !== null &&
					typeof (reference as { relationTo?: unknown }).relationTo === "string" &&
					typeof (reference as { id?: unknown }).id === "string"
		)
	);
}

function isEmail(value: string) {
	return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value);
}

function conditionMatches(value: FormValues[string], condition: PriceCondition) {
	if (condition.condition === "hasValue") {
		if (value === undefined || value === null) return false;
		if (typeof value === "string") return value.trim().length > 0;
		if (Array.isArray(value)) return value.length > 0;
		return true;
	}
	const normalized = String(value ?? "");
	return condition.condition === "equals"
		? normalized === condition.valueForCondition
		: normalized !== condition.valueForCondition;
}
