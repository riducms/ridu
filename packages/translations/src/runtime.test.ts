import { describe, expect, test } from "bun:test";

import { ar } from "./languages/ar";
import { en } from "./languages/en";
import { fr } from "./languages/fr";
import {
	createAdminI18n,
	extendTranslationLanguage,
	parseAcceptLanguage,
	resolvePreferredLanguage,
	validateLanguageCatalogs,
	validatePluginMessageCatalog,
} from "./runtime";

describe("admin translations", () => {
	test("interpolates, uses Intl plurals, and formats in one language and timezone", () => {
		const i18n = createAdminI18n({ language: "en", timeZone: "UTC" });
		expect(i18n.t("dashboard:welcome", { application: "Ridu" })).toBe("Welcome to Ridu");
		expect(i18n.t("collections:selected", { count: 2 })).toBe("2 selected");
		expect(i18n.t("collections:selected", { count: 1234 })).toBe("1,234 selected");
		expect(i18n.formatNumber(1234.5)).toBe("1,234.5");
		expect(
			i18n.formatDate("2026-08-24T23:30:00Z", { dateStyle: "medium", timeStyle: "short" })
		).toContain("11:30");
	});

	test("continues through preferred languages and understands quality values", () => {
		const preferences = parseAcceptLanguage("xx;q=1, fr-CA;q=0.9, en;q=0.8");
		expect(resolvePreferredLanguage(preferences, ["en", "fr"], "en")).toBe("fr");
		expect(resolvePreferredLanguage(["en"], ["en-US", "fr"], "fr")).toBe("en-US");
		expect(resolvePreferredLanguage(["zh-Hant-HK"], ["zh-Hans", "zh-Hant"], "zh-Hans")).toBe(
			"zh-Hant"
		);
		expect(resolvePreferredLanguage(["sr-Latn-RS"], ["sr-Cyrl", "sr-Latn"], "sr-Cyrl")).toBe(
			"sr-Latn"
		);
		expect(parseAcceptLanguage("fr;q=1.5, en;q=0.8")).toEqual(["en"]);
	});

	test("translates, pluralizes, and formats the French admin", () => {
		const i18n = createAdminI18n({ languages: [en, fr], language: "fr", timeZone: "Europe/Paris" });

		expect(i18n.t("dashboard:heading")).toBe("Tableau de bord");
		expect(i18n.t("dashboard:welcome", { application: "Ridu" })).toBe("Bienvenue dans Ridu");
		expect(i18n.text("Posts", { fr: "Articles" })).toBe("Articles");
		expect(i18n.text("Posts", { en: "Posts" })).toBe("Posts");
		expect(i18n.t("collections:selected", { count: 1 })).toBe("1 élément sélectionné");
		expect(i18n.t("collections:selected", { count: 2 })).toBe("2 éléments sélectionnés");
		expect(i18n.t("collections:selected", { count: 1234 })).toBe(
			"1\u202f234 éléments sélectionnés"
		);
		expect(i18n.formatNumber(1234.5)).toBe("1\u202f234,5");
		expect(
			i18n.formatDate("2026-08-24T22:30:00Z", {
				dateStyle: "long",
				timeStyle: "short",
			})
		).toBe("25 août 2026 à 00:30");
	});

	test("provides a complete right-to-left Arabic catalog", () => {
		const i18n = createAdminI18n({ languages: [en, ar], language: "ar" });

		expect(i18n.language).toBe("ar");
		expect(i18n.direction).toBe("rtl");
		expect(i18n.t("dashboard:welcome", { application: "Ridu" })).toBe("مرحبًا بك في Ridu");
		expect(i18n.t("collections:pageSummary", { page: 2, pages: 5 })).toBe("الصفحة 2 من 5");
	});

	test("selects all six Arabic plural categories", () => {
		const i18n = createAdminI18n({ languages: [en, ar], language: "ar" });

		expect(i18n.t("collections:selected", { count: 0 })).toBe("لم يتم تحديد أي عنصر (0)");
		expect(i18n.t("collections:selected", { count: 1 })).toBe("تم تحديد عنصر واحد (1)");
		expect(i18n.t("collections:selected", { count: 2 })).toBe("تم تحديد عنصرين (2)");
		expect(i18n.t("collections:selected", { count: 3 })).toBe("تم تحديد 3 عناصر");
		expect(i18n.t("collections:selected", { count: 11 })).toBe("تم تحديد 11 عنصرًا");
		expect(i18n.t("collections:selected", { count: 100 })).toBe("تم تحديد 100 عنصر");
	});

	test("rejects missing variables and invalid plugin keys deterministically", () => {
		const i18n = createAdminI18n({
			languages: [en],
			pluginMessages: { richtext: { fallback: { bold: "Bold" } } },
		});
		expect(() => i18n.t("dashboard:welcome")).toThrow("requires application");
		expect(i18n.t("plugin.richtext:bold")).toBe("Bold");
		expect(() => i18n.t("plugin.richtext:missing")).toThrow("Missing admin translation");
		expect(() => validateLanguageCatalogs([{ ...en, code: "en-us" }])).toThrow(
			"canonical BCP-47 casing"
		);
		expect(() => createAdminI18n({ timeZone: "+25:00" })).toThrow("Invalid admin timezone");
		expect(() => createAdminI18n({ timeZone: "+05:30" })).not.toThrow();
		expect(() =>
			validatePluginMessageCatalog("broken", {
				fallback: { count: { one: "One" } as never },
			})
		).toThrow("requires other");
		expect(() =>
			validatePluginMessageCatalog("broken", {
				fallback: { count: { other: "{count} items", invalid: "bad" } as never },
			})
		).toThrow("invalid plural category");
	});

	test("supports typed project overrides without copying a whole catalog", () => {
		const custom = extendTranslationLanguage(en, {
			messages: { "dashboard:heading": "Control room" },
		});
		const i18n = createAdminI18n({ languages: [custom] });
		expect(i18n.t("dashboard:heading")).toBe("Control room");
		expect(i18n.t("general:save")).toBe("Save");
	});
});
