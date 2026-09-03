package core

import (
	"context"
	"fmt"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func (application *App) manifestForRequest(ctx context.Context, actor *store.Document) (schema.Snapshot, error) {
	return application.manifestForIdentity(ctx, actor, "")
}

func (application *App) manifestForIdentity(ctx context.Context, actor *store.Document, actorCollection schema.CollectionSlug) (schema.Snapshot, error) {
	snapshot := application.manifest.Snapshot()
	settings := snapshot.Application.Localization
	if settings == nil || application.availableLocales == nil {
		return snapshot, nil
	}
	codes, err := application.availableLocales(LocaleAvailabilityContext{
		Context: ctx, Actor: cloneDocument(actor), ActorCollection: actorCollection, Local: application.local,
	})
	if err != nil {
		return schema.Snapshot{}, &operationengine.Error{Code: "locale_availability_failed", Status: 500, Message: "available locale rule failed", Cause: err}
	}
	configured := make(map[schema.LocaleCode]schema.Locale, len(settings.Locales))
	for _, locale := range settings.Locales {
		configured[locale.Code] = locale
	}
	seen := make(map[schema.LocaleCode]bool, len(codes))
	locales := make([]schema.Locale, 0, len(codes))
	for index, code := range codes {
		locale, exists := configured[code]
		if !exists || seen[code] {
			return schema.Snapshot{}, &operationengine.Error{Code: "locale_availability_failed", Status: 500, Message: fmt.Sprintf("available locale rule returned invalid locale %q at index %d", code, index)}
		}
		seen[code] = true
		locales = append(locales, locale)
	}
	if len(locales) == 0 {
		return schema.Snapshot{}, &operationengine.Error{Code: "locale_availability_failed", Status: 500, Message: "available locale rule returned no locales"}
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
	settings.Locales = locales
	if !seen[settings.DefaultLocale] {
		settings.DefaultLocale = locales[0].Code
	}
	snapshot.Application.Localization = settings
	return snapshot, nil
}
