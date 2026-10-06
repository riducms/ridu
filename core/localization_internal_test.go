package core

import (
	"context"
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestLocalizationForIdentityOverridesOnlyNarrowedPresentations(t *testing.T) {
	configured := &schema.LocalizationSettings{DefaultLocale: "en", Fallback: true, Locales: []schema.Locale{
		{Code: "en", Label: "English"},
		{Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
		{Code: "ar", Label: "Arabic", FallbackLocales: []schema.LocaleCode{"fr", "en"}},
	}}
	var codes []schema.LocaleCode
	application := &App{
		runtime: schema.Snapshot{Application: schema.Application{Localization: configured}},
		availableLocales: func(LocaleAvailabilityContext) ([]schema.LocaleCode, error) {
			return codes, nil
		},
	}
	codes = []schema.LocaleCode{"en", "fr", "ar"}
	if settings, err := application.localizationForIdentity(context.Background(), nil, ""); err != nil || settings != nil {
		t.Fatalf("all locales in authored order = %#v, %v; want the shared manifest", settings, err)
	}
	codes = []schema.LocaleCode{"ar", "fr"}
	settings, err := application.localizationForIdentity(context.Background(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if settings == nil || len(settings.Locales) != 2 || settings.Locales[0].Code != "ar" || settings.DefaultLocale != "ar" || !settings.Fallback {
		t.Fatalf("narrowed settings = %#v", settings)
	}
	if fallbacks := settings.Locales[0].FallbackLocales; len(fallbacks) != 1 || fallbacks[0] != "fr" {
		t.Fatalf("narrowed fallbacks = %#v", fallbacks)
	}
	if len(configured.Locales) != 3 || len(configured.Locales[2].FallbackLocales) != 2 || configured.DefaultLocale != "en" {
		t.Fatalf("narrowing mutated the configured settings: %#v", configured)
	}
	codes = []schema.LocaleCode{"fr", "en", "ar"}
	if settings, err := application.localizationForIdentity(context.Background(), nil, ""); err != nil || settings == nil || settings.Locales[0].Code != "fr" {
		t.Fatalf("reordered locales = %#v, %v", settings, err)
	}
	codes = []schema.LocaleCode{"de"}
	if _, err := application.localizationForIdentity(context.Background(), nil, ""); err == nil {
		t.Fatal("an unknown locale must fail")
	}
}
