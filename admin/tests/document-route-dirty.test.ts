import { describe, expect, it } from "bun:test";

import {
	documentRouteIsDirty,
	saveAuthCreateWithClearedCredentials,
	type AuthCreateCredentials,
} from "@admin/features/documents/document-route-dirty";

describe("document route dirty state", () => {
	it("treats either credential input as an unsaved auth-user creation change", () => {
		expect(
			documentRouteIsDirty({
				documentValuesDirty: false,
				creatingAuthUser: true,
				password: "secret",
				passwordConfirmation: "",
			})
		).toBe(true);
		expect(
			documentRouteIsDirty({
				documentValuesDirty: false,
				creatingAuthUser: true,
				password: "",
				passwordConfirmation: "secret",
			})
		).toBe(true);
	});

	it("ignores empty or inapplicable credentials without hiding document changes", () => {
		expect(
			documentRouteIsDirty({
				documentValuesDirty: false,
				creatingAuthUser: true,
				password: "",
				passwordConfirmation: "",
			})
		).toBe(false);
		expect(
			documentRouteIsDirty({
				documentValuesDirty: false,
				creatingAuthUser: false,
				password: "secret",
				passwordConfirmation: "secret",
			})
		).toBe(false);
		expect(
			documentRouteIsDirty({
				documentValuesDirty: true,
				creatingAuthUser: false,
				password: "",
				passwordConfirmation: "",
			})
		).toBe(true);
	});

	it("clears credentials before a successful create redirect can be blocked", async () => {
		let credentials: AuthCreateCredentials = {
			password: "secret",
			passwordConfirmation: "secret",
		};
		const events: string[] = [];
		const saved = await saveAuthCreateWithClearedCredentials(
			credentials,
			(next) => {
				credentials = next;
				events.push(next.password === "" ? "cleared" : "restored");
			},
			async (password) => {
				events.push("save");
				expect(password).toBe("secret");
				expect(credentials).toEqual({ password: "", passwordConfirmation: "" });
				return true;
			}
		);

		expect(saved).toBe(true);
		expect(events).toEqual(["cleared", "save"]);
		expect(credentials).toEqual({ password: "", passwordConfirmation: "" });
	});

	it("restores credential input when auth creation fails", async () => {
		const original: AuthCreateCredentials = {
			password: "secret",
			passwordConfirmation: "different",
		};
		let credentials = original;
		const saved = await saveAuthCreateWithClearedCredentials(
			original,
			(next) => (credentials = next),
			async () => false
		);

		expect(saved).toBe(false);
		expect(credentials).toEqual(original);
	});

	it("restores credential input and rethrows when auth creation rejects", async () => {
		const original: AuthCreateCredentials = {
			password: "secret",
			passwordConfirmation: "secret",
		};
		const failure = new Error("auth creation failed");
		let credentials = original;
		const saving = saveAuthCreateWithClearedCredentials(
			original,
			(next) => (credentials = next),
			async () => {
				throw failure;
			}
		);

		await expect(saving).rejects.toBe(failure);
		expect(credentials).toEqual(original);
	});
});
