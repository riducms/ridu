package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/schema"
)

// Config is copied over a generated blank application by the documentation
// capture workflow. The generated application and its normal runtime remain
// the thing being exercised; this file only supplies focused authoring data.
func Config() ridu.Config {
	return ridu.Config{
		Name:             "Ridu field guide",
		Admin:            ridu.AdminConfig{User: "users"},
		StorageNamespace: "documentation",
		Localization: ridu.LocalizationConfig{
			Locales: []ridu.Locale{
				{Code: "en", Label: "English"},
				{Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
			},
			DefaultLocale: "en",
		},
		Plugins: installedPlugins(),
		Collections: []ridu.Collection{
			Users,
			Media,
			Categories,
			Articles,
			FieldGuide,
		},
	}
}
