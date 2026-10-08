import { describe, expect, it } from "bun:test";

import { createRichTextI18n } from "../src/lib/editor/rich-text-editor-i18n";

describe("createRichTextI18n", () => {
	it("falls back to English and fills placeholders", () => {
		const i18n = createRichTextI18n();
		expect(i18n.language).toBe("en");
		expect(i18n.direction).toBe("ltr");
		expect(i18n.t("plugin.richtext:editor.heading", { level: 2 })).toBe("Heading 2");
	});

	it("uses a language the editor translates, including its regional variants", () => {
		expect(
			createRichTextI18n({ lang: "fr" }).t("plugin.richtext:editor.heading", { level: 3 })
		).toBe("Titre 3");
		expect(createRichTextI18n({ lang: "fr-CA" }).t("plugin.richtext:menu.lists")).toBe("Listes");
		expect(createRichTextI18n({ lang: "de" }).t("plugin.richtext:menu.lists")).toBe("Lists");
		expect(createRichTextI18n({ lang: "ar", dir: "rtl" }).direction).toBe("rtl");
	});

	it("uses English for a language tag Intl can't read", () => {
		expect(createRichTextI18n({ lang: "" }).language).toBe("en");
		const i18n = createRichTextI18n({ lang: "fr_FR" });
		expect(i18n.language).toBe("en");
		expect(i18n.t("plugin.richtext:menu.lists")).toBe("Lists");
	});

	it("prefers the app's messages and falls back for the rest", () => {
		const i18n = createRichTextI18n({
			lang: "fr",
			messages: { "editor.heading": "Niveau {level}" },
		});
		expect(i18n.t("plugin.richtext:editor.heading", { level: 1 })).toBe("Niveau 1");
		expect(i18n.t("plugin.richtext:menu.lists")).toBe("Listes");
	});

	it("rejects keys outside its catalogue", () => {
		const i18n = createRichTextI18n();
		expect(() => i18n.t("general:save")).toThrow("no message general:save");
		expect(() => i18n.t("plugin.richtext:missing")).toThrow("no message plugin.richtext:missing");
	});
});
