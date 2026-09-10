import { describe, expect, it } from "bun:test";

import {
	buildSubmissionInput,
	confirmationFor,
	getPaymentTotal,
	validateFormValues,
	type FormDefinition,
} from "../src";
import { formBuilderAdminPlugin } from "../src/admin";

const form: FormDefinition = {
	id: "form-1",
	title: "Contact",
	confirmationType: "message",
	confirmationMessage: "Thanks",
	fields: [
		{ blockType: "text", name: "name", label: "Name", required: true },
		{ blockType: "email", name: "email", label: "Email", required: true },
		{ blockType: "checkbox", name: "terms", label: "Terms", required: true },
		{
			blockType: "upload",
			name: "resume",
			label: "Resume",
			uploadCollection: "documents",
		},
	],
};

describe("Form Builder contract", () => {
	it("pairs with the backend at version one", () => {
		expect(formBuilderAdminPlugin.key).toBe("form-builder");
		expect(formBuilderAdminPlugin.pairingVersion).toBe(1);
		expect("fields" in formBuilderAdminPlugin).toBe(false);
	});

	it("builds ordinary and upload submission rows", () => {
		expect(
			buildSubmissionInput(form, {
				name: "Ada",
				email: "ada@example.test",
				terms: true,
				resume: ["file-1"],
			})
		).toEqual({
			form: "form-1",
			submissionData: [
				{ field: "name", value: "Ada" },
				{ field: "email", value: "ada@example.test" },
				{ field: "terms", value: true },
			],
			submissionUploads: [{ field: "resume", value: ["file-1"] }],
		});
		expect(buildSubmissionInput(form, { name: null, email: undefined })).toEqual({
			form: "form-1",
			submissionData: [],
		});
	});

	it("provides immediate validation while leaving the server authoritative", () => {
		expect(validateFormValues(form, { name: "", email: "invalid", terms: false })).toEqual([
			{ code: "required", field: "name", message: "Name is required." },
			{ code: "invalid", field: "email", message: "Enter a valid email address." },
			{ code: "required", field: "terms", message: "Terms is required." },
		]);
	});

	it("calculates conditional payment totals and confirmation behavior", () => {
		expect(
			getPaymentTotal(
				10,
				[
					{
						fieldToUse: "quantity",
						condition: "hasValue",
						operator: "multiply",
						valueForOperator: "quantity",
						valueType: "valueOfField",
					},
				],
				{ quantity: 3 }
			)
		).toBe(30);
		expect(confirmationFor(form)).toEqual({ type: "message", message: "Thanks" });
		expect(
			getPaymentTotal(
				10,
				[
					{
						fieldToUse: "name",
						condition: "hasValue",
						operator: "add",
						valueForOperator: "5",
						valueType: "static",
					},
				],
				{ name: "   " }
			)
		).toBe(10);
	});

	it("normalizes nullable fields and supports Ridu polymorphic upload references", () => {
		expect(buildSubmissionInput({ ...form, fields: null }, {})).toEqual({
			form: "form-1",
			submissionData: [],
		});
		expect(
			buildSubmissionInput(form, {
				resume: [{ relationTo: "documents", id: "file-1" }],
			})
		).toMatchObject({
			submissionUploads: [{ field: "resume", value: [{ relationTo: "documents", id: "file-1" }] }],
		});
	});
});
