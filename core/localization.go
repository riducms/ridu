package core

import (
	"context"
	"fmt"

	"github.com/riducms/ridu/internal/httpapi"
	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// localizationForRequest adapts LocalizationConfig.AvailableLocales to the
// admin presentation hook. It is nil when no rule can narrow the locales.
func (application *App) localizationForRequest() func(context.Context, *httpapi.AuthIdentity) (*schema.LocalizationSettings, error) {
	if application.availableLocales == nil || application.runtime.Application.Localization == nil {
		return nil
	}
	return func(ctx context.Context, identity *httpapi.AuthIdentity) (*schema.LocalizationSettings, error) {
		return application.localizationForIdentity(ctx, httpIdentityActor(identity), httpIdentityCollection(identity))
	}
}

// localizationForIdentity returns the content localization presented to one
// actor, or nil when the actor sees every configured locale in authored order.
func (application *App) localizationForIdentity(ctx context.Context, actor *store.Document, actorCollection schema.CollectionSlug) (*schema.LocalizationSettings, error) {
	configuredSettings := application.runtime.Application.Localization
	if configuredSettings == nil || application.availableLocales == nil {
		return nil, nil
	}
	codes, err := application.availableLocales(LocaleAvailabilityContext{
		Context: ctx, Actor: cloneDocument(actor), ActorCollection: actorCollection, Local: application.local,
	})
	if err != nil {
		return nil, &operationengine.Error{Code: "locale_availability_failed", Status: 500, Message: "available locale rule failed", Cause: err}
	}
	configured := make(map[schema.LocaleCode]schema.Locale, len(configuredSettings.Locales))
	for _, locale := range configuredSettings.Locales {
		configured[locale.Code] = locale
	}
	seen := make(map[schema.LocaleCode]bool, len(codes))
	locales := make([]schema.Locale, 0, len(codes))
	unchanged := len(codes) == len(configuredSettings.Locales)
	for index, code := range codes {
		locale, exists := configured[code]
		if !exists || seen[code] {
			return nil, &operationengine.Error{Code: "locale_availability_failed", Status: 500, Message: fmt.Sprintf("available locale rule returned invalid locale %q at index %d", code, index)}
		}
		seen[code] = true
		locales = append(locales, locale)
		unchanged = unchanged && configuredSettings.Locales[index].Code == code
	}
	if len(locales) == 0 {
		return nil, &operationengine.Error{Code: "locale_availability_failed", Status: 500, Message: "available locale rule returned no locales"}
	}
	if unchanged {
		return nil, nil
	}
	for index := range locales {
		fallbacks := make([]schema.LocaleCode, 0, len(locales[index].FallbackLocales))
		for _, fallback := range locales[index].FallbackLocales {
			if seen[fallback] {
				fallbacks = append(fallbacks, fallback)
			}
		}
		locales[index].FallbackLocales = fallbacks
	}
	settings := *configuredSettings
	settings.Locales = locales
	if !seen[settings.DefaultLocale] {
		settings.DefaultLocale = locales[0].Code
	}
	return &settings, nil
}
