import { expect, test } from "./fixture";

import { loginAsEditor, observePageErrors } from "./helpers";

test("Form Builder authors native forms and validates public submissions end to end", async ({
	page,
}) => {
	test.setTimeout(45_000);
	const { consoleErrors, pageErrors } = observePageErrors(page);

	const publicFormsResponse = await page.request.get("/api/collections/forms?limit=10");
	expect(publicFormsResponse.ok()).toBe(true);
	const publicForms = (await publicFormsResponse.json()).docs;
	const seededForm = publicForms.find(
		(candidate: { title?: string }) => candidate.title === "Contact Form"
	);
	expect(seededForm).toBeDefined();
	const formID = seededForm.id as string;
	const publicFormResponse = await page.request.get(`/api/collections/forms/${formID}`);
	expect(publicFormResponse.ok()).toBe(true);
	const publicForm = (await publicFormResponse.json()).doc;
	expect(publicForm.title).toBe("Contact Form");
	expect(publicForm.fields).toHaveLength(7);
	expect(publicForm.emails).toBeUndefined();

	const anonymousFormMutation = await page.request.patch(`/api/collections/forms/${formID}`, {
		data: { title: "Mutated anonymously" },
	});
	expect(anonymousFormMutation.status()).toBe(403);

	const invalid = await page.request.post("/api/collections/form-submissions", {
		data: {
			form: formID,
			submissionData: [
				{ field: "email", value: "not-an-email" },
				{ field: "unknown", value: "ignored" },
			],
		},
	});
	expect(invalid.status()).toBe(422);
	const invalidBody = await invalid.json();
	expect(invalidBody.error.code).toBe("validation");
	expect(invalidBody.error.issues.map((issue: { code: string }) => issue.code)).toEqual(
		expect.arrayContaining(["invalid_email", "required", "unknown_form_field"])
	);

	const scalarUpload = await page.request.post("/api/collections/form-submissions", {
		data: {
			form: formID,
			submissionData: [
				{ field: "name", value: "Ada Lovelace" },
				{ field: "email", value: "ada@example.test" },
				{ field: "message", value: "Tell me more." },
				{ field: "attachment", value: "media_1" },
			],
		},
	});
	expect(scalarUpload.status()).toBe(422);
	expect(
		(await scalarUpload.json()).error.issues.map((issue: { code: string }) => issue.code)
	).toContain("upload_in_submission_data");

	const mediaResponse = await page.request.get("/api/collections/media?limit=1");
	expect(mediaResponse.ok()).toBe(true);
	const mediaID = (await mediaResponse.json()).docs[0].id as string;

	const valid = await page.request.post("/api/collections/form-submissions", {
		data: {
			form: formID,
			submissionData: [
				{ field: "name", value: "Ada Lovelace" },
				{ field: "email", value: "ada@example.test" },
				{ field: "topic", value: "sales" },
				{ field: "message", value: "Tell me more." },
				{ field: "donation", value: 30 },
			],
			submissionUploads: [{ field: "attachment", value: [mediaID] }],
		},
	});
	expect(valid.status()).toBe(201);
	const validSubmission = (await valid.json()).doc;

	await loginAsEditor(page);
	consoleErrors.length = 0;
	const privateFormResponse = await page.request.get(`/api/collections/forms/${formID}`);
	expect(privateFormResponse.ok()).toBe(true);
	expect((await privateFormResponse.json()).doc.emails[0].emailTo).toBe("forms@riducms.test");
	const privateSubmissionResponse = await page.request.get(
		`/api/collections/form-submissions/${validSubmission.id}`
	);
	expect(privateSubmissionResponse.ok()).toBe(true);
	const privateSubmission = (await privateSubmissionResponse.json()).doc;
	expect(privateSubmission.payment).toMatchObject({ processor: "fixture", total: 30 });
	expect(privateSubmission.submissionUploads[0]).toMatchObject({
		field: "attachment",
		value: [mediaID],
	});

	await page.goto("/admin/collections/forms");
	await expect(page.getByRole("heading", { name: "Forms" })).toBeVisible();
	await page.getByRole("link", { name: "Contact Form", exact: true }).click();
	await expect(page.getByLabel("Title", { exact: true })).toHaveValue("Contact Form");
	await expect(page.locator('[data-field-path="fields"]')).toContainText("Name");
	await expect(page.locator('[data-field-path="fields"]')).toContainText("Email");
	await expect(page.locator('[data-field-path="fields"]')).toContainText("Message");
	await expect(page.locator('[data-field-path="fields"]')).toContainText("Upload");
	await expect(page.locator('[data-field-path="fields"]')).toContainText("Payment");
	await expect(page.getByLabel("Email to", { exact: true })).toHaveValue("forms@riducms.test");

	await page.goto("/admin/collections/form-submissions");
	await expect(page.getByRole("heading", { name: "Form Submissions" })).toBeVisible();
	await expect(page.getByText("Contact Form", { exact: true }).first()).toBeVisible();

	expect(consoleErrors).toEqual([]);
	expect(pageErrors).toEqual([]);
});
