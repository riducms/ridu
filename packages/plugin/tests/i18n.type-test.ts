import { defineAdminMessages } from "../src/i18n";

defineAdminMessages({
	fallback: {
		greeting: "Hello {name}",
		selected: { one: "{count} selected", other: "{count} selected" },
	},
	translations: {
		fr: {
			greeting: "Bonjour {name}",
			selected: { one: "{count} sélectionné", other: "{count} sélectionnés" },
		},
	},
});

defineAdminMessages({
	fallback: { greeting: "Hello {name}" },
	translations: {
		fr: {
			// @ts-expect-error Translated placeholders must exactly match the fallback.
			greeting: "Bonjour",
		},
	},
});

defineAdminMessages({
	fallback: { greeting: "Hello", farewell: "Goodbye" },
	translations: {
		// @ts-expect-error Every translated locale must define every fallback key.
		fr: { greeting: "Bonjour" },
	},
});
