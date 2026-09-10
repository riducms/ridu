package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/blocktypes"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"golang.org/x/text/language"
)

// Resolve performs a complete deterministic validation pass before freezing a
// manifest. It has no runtime plugin setup, database, filesystem, or network
// dependency.
func Resolve(input Input) (schema.Manifest, error) {
	var err error
	input, err = bindInputBlocks(input)
	if err != nil {
		return schema.Manifest{}, err
	}
	return resolveBound(input)
}

func resolveBound(input Input) (schema.Manifest, error) {
	resolver := &resolver{
		input:           input,
		blockTemplates:  make(map[string]schema.BlockType),
		collectionSlugs: make(map[schema.CollectionSlug]collectionReference),
		globalSlugs:     make(map[schema.CollectionSlug]string),
		pluginKeys:      make(map[string]string),
		pluginFieldKeys: make(map[string]string),
		adminPluginKeys: make(map[string]struct{}),
	}
	return resolver.resolve()
}

func isJSONObject(value json.RawMessage) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(value, &object) == nil && object != nil
}

type collectionReference struct {
	id     schema.StableID
	path   string
	auth   bool
	upload bool
}

type resolver struct {
	blockTemplates  map[string]schema.BlockType
	blockTypeNames  map[string]struct{ shape, path string }
	input           Input
	issues          []schema.Issue
	collectionSlugs map[schema.CollectionSlug]collectionReference
	globalSlugs     map[schema.CollectionSlug]string
	pluginKeys      map[string]string
	pluginFieldKeys map[string]string
	adminPluginKeys map[string]struct{}
	adminLanguages  map[string]struct{}
}

func (resolver *resolver) resolve() (schema.Manifest, error) {
	applicationName := strings.TrimSpace(resolver.input.Name)
	if applicationName == "" {
		resolver.issue("missing_application_name", "name", "application name must not be empty")
	}
	if len(resolver.input.Collections) == 0 {
		resolver.issue("missing_collections", "collections", "at least one collection is required")
	}

	resolver.indexCollections()
	resolver.indexGlobals()
	admin := resolver.resolveAdmin()
	adminLocalization := resolver.resolveAdminLocalization()
	applicationNameTranslations := resolver.resolveTranslations(resolver.input.NameTranslations, "nameTranslations")
	localization := resolver.resolveLocalization()
	plugins := resolver.resolvePlugins()
	endpoints := resolver.resolveEndpoints(resolver.input.Endpoints, "endpoints")
	resolver.validateRootEndpointNamespaces(endpoints)
	collections := make([]schema.Collection, len(resolver.input.Collections))
	for index, collection := range resolver.input.Collections {
		collections[index] = resolver.resolveCollection(index, collection)
	}
	globals := make([]schema.Global, len(resolver.input.Globals))
	for index, global := range resolver.input.Globals {
		globals[index] = resolver.resolveGlobal(index, global)
	}
	registeredBlocks := resolver.registeredBlocks()
	resolver.validateCrossCollectionFields(collections, globals)
	if err := schema.ValidateFieldEditors(schema.Snapshot{Collections: collections, Globals: globals}); err != nil {
		resolver.issue("invalid_field_editor", "fields", err.Error())
	}
	if err := schema.ValidateEmbeddedMetadata(schema.Snapshot{Collections: collections, Globals: globals, Plugins: plugins}); err != nil {
		resolver.issue("invalid_embedded_schema", "fields", err.Error())
	}
	if _, err := blocktypes.Build(schema.Snapshot{Collections: collections, Globals: globals}); err != nil {
		if validation, ok := err.(*schema.ValidationError); ok {
			resolver.issues = append(resolver.issues, validation.Issues...)
		}
	}

	if len(resolver.issues) != 0 {
		return schema.Manifest{}, schema.NewValidationError(resolver.issues)
	}

	return schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion,
		Application: schema.Application{
			Name: applicationName, NameTranslations: applicationNameTranslations,
			AllowIDOnCreate: resolver.input.AllowIDOnCreate,
			Admin:           admin, AdminLocalization: adminLocalization, Localization: localization,
			Endpoints: endpoints,
		},
		Blocks:      registeredBlocks,
		Collections: collections,
		Globals:     globals,
		Plugins:     plugins,
	}), nil
}

func (resolver *resolver) resolveAdminLocalization() *schema.AdminLocalizationSettings {
	input := resolver.input.Admin.Localization
	if len(input.Languages) == 0 && input.DefaultLanguage == "" && len(input.TimeZones) == 0 && input.DefaultTimeZone == "" {
		return nil
	}
	settings := &schema.AdminLocalizationSettings{
		Languages: make([]schema.AdminLanguage, len(input.Languages)),
		TimeZones: make([]schema.AdminTimeZone, len(input.TimeZones)), DefaultTimeZone: strings.TrimSpace(input.DefaultTimeZone),
	}
	languages := make(map[string]string, len(input.Languages))
	resolver.adminLanguages = make(map[string]struct{}, len(input.Languages))
	for index, language := range input.Languages {
		path := fmt.Sprintf("admin.localization.languages[%d]", index)
		code, validCode := canonicalAdminLanguageCode(language.Code)
		label := strings.TrimSpace(language.Label)
		if !validCode {
			resolver.issue("invalid_admin_language_code", path+".code", "admin language code must be a valid BCP-47 language tag such as en or pt-BR")
		}
		if previous, exists := languages[code]; code != "" && exists {
			resolver.issue("duplicate_admin_language_code", path+".code", fmt.Sprintf("admin language %q is already used at %s", code, previous))
		} else if code != "" {
			languages[code] = path + ".code"
			resolver.adminLanguages[code] = struct{}{}
		}
		if label == "" {
			resolver.issue("missing_admin_language_label", path+".label", "admin language label must not be empty")
		}
		settings.Languages[index] = schema.AdminLanguage{Code: code, Label: label, RTL: language.RTL}
	}
	if len(input.Languages) == 0 {
		resolver.issue("missing_admin_languages", "admin.localization.languages", "admin localization requires at least one language")
	}
	defaultLanguage, validDefaultLanguage := canonicalAdminLanguageCode(input.DefaultLanguage)
	settings.DefaultLanguage = defaultLanguage
	if settings.DefaultLanguage == "" {
		resolver.issue("missing_default_admin_language", "admin.localization.defaultLanguage", "admin localization requires a default language")
	} else if !validDefaultLanguage {
		resolver.issue("invalid_default_admin_language", "admin.localization.defaultLanguage", "default admin language must be a valid BCP-47 language tag")
	} else if _, exists := languages[settings.DefaultLanguage]; !exists {
		resolver.issue("unknown_default_admin_language", "admin.localization.defaultLanguage", fmt.Sprintf("default admin language %q is not configured", settings.DefaultLanguage))
	}
	for index, language := range input.Languages {
		settings.Languages[index].LabelTranslations = resolver.resolveTranslations(
			language.LabelTranslations,
			fmt.Sprintf("admin.localization.languages[%d].labelTranslations", index),
		)
	}
	timeZones := make(map[string]string, len(input.TimeZones))
	for index, timeZone := range input.TimeZones {
		path := fmt.Sprintf("admin.localization.timeZones[%d]", index)
		id := strings.TrimSpace(timeZone.ID)
		label := strings.TrimSpace(timeZone.Label)
		if !validAdminTimeZoneID(id) {
			resolver.issue("invalid_admin_timezone", path+".id", "admin timezone must be UTC, an offset such as +05:30, or a safe IANA timezone such as Europe/London")
		}
		if previous, exists := timeZones[id]; id != "" && exists {
			resolver.issue("duplicate_admin_timezone", path+".id", fmt.Sprintf("admin timezone %q is already used at %s", id, previous))
		} else if id != "" {
			timeZones[id] = path + ".id"
		}
		if label == "" {
			resolver.issue("missing_admin_timezone_label", path+".label", "admin timezone label must not be empty")
		}
		settings.TimeZones[index] = schema.AdminTimeZone{
			ID: id, Label: label,
			LabelTranslations: resolver.resolveTranslations(timeZone.LabelTranslations, path+".labelTranslations"),
		}
	}
	if settings.DefaultTimeZone != "" {
		if _, exists := timeZones[settings.DefaultTimeZone]; !exists {
			resolver.issue("unknown_default_admin_timezone", "admin.localization.defaultTimeZone", fmt.Sprintf("default admin timezone %q is not configured", settings.DefaultTimeZone))
		}
	}
	return settings
}

func (resolver *resolver) resolveTranslations(input map[string]string, path string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	keys := make([]string, 0, len(input))
	for language := range input {
		keys = append(keys, language)
	}
	sort.Strings(keys)
	resolved := make(map[string]string, len(input))
	for _, language := range keys {
		value := strings.TrimSpace(input[language])
		entryPath := fmt.Sprintf("%s[%q]", path, language)
		if _, configured := resolver.adminLanguages[language]; !configured {
			resolver.issue("unknown_admin_translation_language", entryPath, fmt.Sprintf("admin translation language %q is not configured", language))
		}
		if value == "" {
			resolver.issue("missing_admin_translation_value", entryPath, "admin translation value must not be blank")
		}
		resolved[language] = value
	}
	return resolved
}

func cloneTranslations(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for language, value := range input {
		cloned[language] = value
	}
	return cloned
}

func validAdminTimeZoneID(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 128 || strings.Contains(value, "..") || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") {
		return false
	}
	if validAdminTimeZoneOffset(value) {
		return true
	}
	if value != "UTC" && !strings.Contains(value, "/") {
		return false
	}
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || strings.ContainsRune("_+-/", character) {
			continue
		}
		return false
	}
	_, err := time.LoadLocation(value)
	return err == nil
}

func validAdminTimeZoneOffset(value string) bool {
	if len(value) != 6 || (value[0] != '+' && value[0] != '-') || value[3] != ':' {
		return false
	}
	for _, index := range []int{1, 2, 4, 5} {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	hours := int(value[1]-'0')*10 + int(value[2]-'0')
	minutes := int(value[4]-'0')*10 + int(value[5]-'0')
	return hours <= 23 && minutes <= 59
}

func canonicalAdminLanguageCode(value string) (string, bool) {
	candidate := strings.TrimSpace(value)
	if candidate == "" || candidate != value {
		return candidate, false
	}
	tag, err := language.Parse(candidate)
	if err != nil || tag == language.Und {
		return candidate, false
	}
	return tag.String(), true
}

func (resolver *resolver) resolveLocalization() *schema.LocalizationSettings {
	input := resolver.input.Localization
	if input == nil {
		return nil
	}
	settings := &schema.LocalizationSettings{
		Locales: make([]schema.Locale, len(input.Locales)), DefaultLocale: input.DefaultLocale,
		Fallback: !input.DisableFallback,
	}
	codes := make(map[schema.LocaleCode]string, len(input.Locales))
	for index, locale := range input.Locales {
		path := fmt.Sprintf("localization.locales[%d]", index)
		code := locale.Code
		label := strings.TrimSpace(locale.Label)
		if !schema.IsValidLocaleCode(string(code)) {
			resolver.issue("invalid_locale_code", path+".code", "locale code must contain alphanumeric segments separated by hyphens or underscores and must not be a reserved request token")
		}
		if previous, exists := codes[code]; code != "" && exists {
			resolver.issue("duplicate_locale_code", path+".code", fmt.Sprintf("locale code %q is already used at %s", code, previous))
		} else if code != "" {
			codes[code] = path + ".code"
		}
		if label == "" {
			resolver.issue("missing_locale_label", path+".label", "locale label must not be empty")
		}
		settings.Locales[index] = schema.Locale{
			Code: code, Label: label, RTL: locale.RTL,
			FallbackLocales: append([]schema.LocaleCode(nil), locale.FallbackLocales...),
		}
	}
	if len(input.Locales) == 0 {
		resolver.issue("missing_locales", "localization.locales", "localization requires at least one locale")
	}
	if input.DefaultLocale == "" {
		resolver.issue("missing_default_locale", "localization.defaultLocale", "localization requires a default locale")
	} else if _, exists := codes[input.DefaultLocale]; !exists {
		resolver.issue("unknown_default_locale", "localization.defaultLocale", fmt.Sprintf("default locale %q is not configured", input.DefaultLocale))
	}
	for index, locale := range settings.Locales {
		seen := make(map[schema.LocaleCode]struct{}, len(locale.FallbackLocales))
		for fallbackIndex, fallback := range locale.FallbackLocales {
			path := fmt.Sprintf("localization.locales[%d].fallbackLocales[%d]", index, fallbackIndex)
			if _, exists := codes[fallback]; !exists {
				resolver.issue("unknown_fallback_locale", path, fmt.Sprintf("fallback locale %q is not configured", fallback))
			}
			if fallback == locale.Code {
				resolver.issue("self_fallback_locale", path, "locale cannot fall back to itself")
			}
			if _, exists := seen[fallback]; exists {
				resolver.issue("duplicate_fallback_locale", path, fmt.Sprintf("fallback locale %q was configured more than once", fallback))
			}
			seen[fallback] = struct{}{}
		}
	}
	if cycle := fallbackCycle(settings.Locales); len(cycle) != 0 {
		resolver.issue("fallback_locale_cycle", "localization.locales", fmt.Sprintf("locale fallback cycle: %s", strings.Join(cycle, " -> ")))
	}
	return settings
}

func fallbackCycle(locales []schema.Locale) []string {
	edges := make(map[schema.LocaleCode][]schema.LocaleCode, len(locales))
	for _, locale := range locales {
		edges[locale.Code] = locale.FallbackLocales
	}
	state := make(map[schema.LocaleCode]uint8, len(locales))
	var stack []schema.LocaleCode
	var visit func(schema.LocaleCode) []string
	visit = func(code schema.LocaleCode) []string {
		state[code] = 1
		stack = append(stack, code)
		for _, next := range edges[code] {
			if state[next] == 1 {
				start := 0
				for stack[start] != next {
					start++
				}
				cycle := make([]string, 0, len(stack)-start+1)
				for _, item := range stack[start:] {
					cycle = append(cycle, string(item))
				}
				return append(cycle, string(next))
			}
			if state[next] == 0 {
				if cycle := visit(next); len(cycle) != 0 {
					return cycle
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[code] = 2
		return nil
	}
	for _, locale := range locales {
		if state[locale.Code] == 0 {
			if cycle := visit(locale.Code); len(cycle) != 0 {
				return cycle
			}
		}
	}
	return nil
}

func (resolver *resolver) indexCollections() {
	for index, collection := range resolver.input.Collections {
		path := fmt.Sprintf("collections[%d]", index)
		slugPath := path + ".slug"
		collectionID := effectiveCollectionID(collection)

		if collection.Slug == "" {
			resolver.issue("missing_collection_slug", slugPath, "collection slug must not be empty")
		} else if !schema.IsValidCollectionSlug(string(collection.Slug)) {
			resolver.issue("invalid_collection_slug", slugPath, "collection slug must be lowercase kebab-case")
		}
		if previous, exists := resolver.collectionSlugs[collection.Slug]; collection.Slug != "" && exists {
			resolver.issue("duplicate_collection_slug", slugPath, fmt.Sprintf("collection slug %q is already used at %s", collection.Slug, previous.path))
		} else if collection.Slug != "" {
			resolver.collectionSlugs[collection.Slug] = collectionReference{id: collectionID, path: slugPath, auth: collection.Auth, upload: collection.Upload}
		}
	}
}

func (resolver *resolver) indexGlobals() {
	for index, global := range resolver.input.Globals {
		path := fmt.Sprintf("globals[%d].slug", index)
		if global.Slug == "" {
			resolver.issue("missing_global_slug", path, "global slug must not be empty")
			continue
		}
		if !schema.IsValidCollectionSlug(string(global.Slug)) {
			resolver.issue("invalid_global_slug", path, "global slug must be lowercase kebab-case")
			continue
		}
		if previous, exists := resolver.globalSlugs[global.Slug]; exists {
			resolver.issue("duplicate_global_slug", path, fmt.Sprintf("global slug %q is already used at %s", global.Slug, previous))
			continue
		}
		resolver.globalSlugs[global.Slug] = path
		globalID := effectiveGlobalID(global)
		if previous, exists := resolver.collectionSlugs[schema.CollectionSlug(globalID)]; exists {
			resolver.issue(
				"duplicate_resource_id",
				path,
				fmt.Sprintf("global slug %q derives stable ID %q, which is already used at %s", global.Slug, globalID, previous.path),
			)
		}
	}
}

func effectiveGlobalID(global Global) schema.StableID {
	if schema.IsValidCollectionSlug(string(global.Slug)) {
		return schema.StableID("global-" + string(global.Slug))
	}
	return ""
}

func (resolver *resolver) resolveAdmin() *schema.AdminSettings {
	path := "admin.user"
	user := resolver.input.Admin.User
	hasAuthCollection := false
	for _, collection := range resolver.input.Collections {
		if collection.Auth {
			hasAuthCollection = true
			break
		}
	}

	if user == "" {
		if hasAuthCollection {
			resolver.issue("missing_admin_user", path, "admin user collection must be selected when auth is enabled")
		}
		return nil
	}

	reference, exists := resolver.collectionSlugs[user]
	if !exists {
		resolver.issue("unknown_admin_user", path, fmt.Sprintf("admin user collection %q does not exist", user))
		return nil
	}
	if !reference.auth {
		resolver.issue("invalid_admin_user", path, fmt.Sprintf("admin user collection %q must have auth enabled", user))
		return nil
	}
	return &schema.AdminSettings{UserCollectionID: reference.id, UserCollectionSlug: user}
}

func effectiveCollectionID(collection Collection) schema.StableID {
	if schema.IsValidCollectionSlug(string(collection.Slug)) {
		return schema.StableID(collection.Slug)
	}
	return ""
}

func (resolver *resolver) resolvePlugins() []schema.Plugin {
	plugins := make([]schema.Plugin, len(resolver.input.Plugins))
	adminTargets := make(map[string]string)
	adminRoutes := make(map[string]string)
	for index, plugin := range resolver.input.Plugins {
		pluginPath := fmt.Sprintf("plugins[%d]", index)
		path := pluginPath + ".key"
		if plugin.Key == "" {
			resolver.issue("missing_plugin_key", path, "plugin key must not be empty")
		} else if !schema.IsValidPluginKey(plugin.Key) {
			resolver.issue("invalid_plugin_key", path, "plugin key must be lowercase kebab-case")
		}
		if previousPath, exists := resolver.pluginKeys[plugin.Key]; plugin.Key != "" && exists {
			resolver.issue("duplicate_plugin_key", path, fmt.Sprintf("plugin key %q is already used at %s", plugin.Key, previousPath))
		} else if plugin.Key != "" {
			resolver.pluginKeys[plugin.Key] = path
		}
		resolved := schema.Plugin{Key: plugin.Key}
		hasDescriptor := plugin.Version != "" || plugin.GoPackage != "" || plugin.APIVersion != 0 || plugin.Ridu != nil || len(plugin.FieldTypes) != 0 || len(plugin.DatabaseContributions) != 0 || plugin.Admin != nil || len(plugin.Endpoints) != 0
		if hasDescriptor {
			resolver.resolvePluginDescriptor(pluginPath, plugin, &resolved)
		} else if resolver.pluginKeys[plugin.Key] == path {
			resolver.claimPluginField(plugin.Key, pluginPath+".key")
		}
		if plugin.Admin != nil {
			resolver.adminPluginKeys[plugin.Key] = struct{}{}
			adminPath := pluginPath + ".admin"
			if !schema.IsValidAdminPluginPackage(plugin.Admin.Package) {
				resolver.issue("invalid_admin_plugin_package", adminPath+".package", "admin plugin package must be a valid installed JavaScript package specifier")
			}
			if !schema.IsValidAdminPluginExport(plugin.Admin.Export) {
				resolver.issue("invalid_admin_plugin_export", adminPath+".export", "admin plugin export must be a valid named JavaScript export")
			}
			if plugin.Admin.APIVersion != schema.CurrentAdminPluginAPIVersion {
				resolver.issue("incompatible_admin_plugin_api", adminPath+".apiVersion", fmt.Sprintf("admin plugin API version must be %d", schema.CurrentAdminPluginAPIVersion))
			}
			if plugin.Admin.PairingVersion == 0 {
				resolver.issue("invalid_admin_plugin_pairing_version", adminPath+".pairingVersion", "admin plugin pairing version must be positive")
			}
			target := plugin.Admin.Package + "#" + plugin.Admin.Export
			if previousPath, exists := adminTargets[target]; plugin.Admin.Package != "" && plugin.Admin.Export != "" && exists {
				resolver.issue("duplicate_admin_plugin_export", adminPath, fmt.Sprintf("admin plugin export %q is already paired at %s", target, previousPath))
			} else if plugin.Admin.Package != "" && plugin.Admin.Export != "" {
				adminTargets[target] = adminPath
			}
			resolved.Admin = &schema.PluginAdmin{
				Package:        plugin.Admin.Package,
				Export:         plugin.Admin.Export,
				APIVersion:     plugin.Admin.APIVersion,
				PairingVersion: plugin.Admin.PairingVersion,
				Routes:         append([]string(nil), plugin.Admin.Routes...),
				Assets:         append([]string(nil), plugin.Admin.Assets...),
			}
			routes := make(map[string]struct{}, len(plugin.Admin.Routes))
			for routeIndex, route := range plugin.Admin.Routes {
				if !schema.IsValidAdminPluginRoute(route) {
					resolver.issue("invalid_admin_plugin_route", fmt.Sprintf("%s.routes[%d]", adminPath, routeIndex), "admin plugin route must be a relative path without traversal, query, fragment, or wildcard segments")
				}
				if _, duplicate := routes[route]; duplicate {
					resolver.issue("duplicate_admin_plugin_route", fmt.Sprintf("%s.routes[%d]", adminPath, routeIndex), fmt.Sprintf("admin plugin route %q is already declared", route))
				}
				if previousPath, duplicate := adminRoutes[route]; duplicate {
					resolver.issue("duplicate_admin_plugin_route", fmt.Sprintf("%s.routes[%d]", adminPath, routeIndex), fmt.Sprintf("admin plugin route %q is already declared at %s", route, previousPath))
				} else {
					adminRoutes[route] = fmt.Sprintf("%s.routes[%d]", adminPath, routeIndex)
				}
				routes[route] = struct{}{}
			}
			assets := make(map[string]struct{}, len(plugin.Admin.Assets))
			for assetIndex, asset := range plugin.Admin.Assets {
				if !schema.IsValidAdminPluginAsset(asset) {
					resolver.issue("invalid_admin_plugin_asset", fmt.Sprintf("%s.assets[%d]", adminPath, assetIndex), "admin plugin asset must be a unique package-relative path without traversal")
				}
				if _, duplicate := assets[asset]; duplicate {
					resolver.issue("duplicate_admin_plugin_asset", fmt.Sprintf("%s.assets[%d]", adminPath, assetIndex), fmt.Sprintf("admin plugin asset %q is already declared", asset))
				}
				assets[asset] = struct{}{}
			}
		}
		endpointKeys := make(map[string]struct{}, len(plugin.Endpoints))
		for endpointIndex, endpoint := range plugin.Endpoints {
			endpointPath := fmt.Sprintf("%s.endpoints[%d]", pluginPath, endpointIndex)
			method := strings.ToUpper(strings.TrimSpace(endpoint.Method))
			if !validPluginEndpoint(method, endpoint.Path) || strings.TrimSpace(endpoint.Summary) == "" {
				resolver.issue("invalid_plugin_endpoint", endpointPath, "plugin endpoints require an HTTP method, relative path, summary, and no traversal or wildcard segments")
			}
			identity := method + " " + endpoint.Path
			if _, exists := endpointKeys[identity]; exists {
				resolver.issue("duplicate_plugin_endpoint", endpointPath, fmt.Sprintf("plugin endpoint %q is already declared", identity))
			}
			endpointKeys[identity] = struct{}{}
			resolved.Endpoints = append(resolved.Endpoints, schema.PluginEndpoint{Method: method, Path: endpoint.Path, Summary: strings.TrimSpace(endpoint.Summary)})
		}
		plugins[index] = resolved
	}
	return plugins
}

func (resolver *resolver) claimPluginField(key, path string) {
	if previous, exists := resolver.pluginFieldKeys[key]; exists {
		resolver.issue("duplicate_plugin_field_type", path, fmt.Sprintf("plugin field type %q is already declared at %s", key, previous))
	} else {
		resolver.pluginFieldKeys[key] = path
	}
}

func (resolver *resolver) resolvePluginDescriptor(path string, plugin Plugin, resolved *schema.Plugin) {
	if !schema.IsValidSemanticVersion(plugin.Version) {
		resolver.issue("invalid_plugin_version", path+".version", "plugin version must be a complete semantic version")
	}
	if !schema.IsValidGoPackage(plugin.GoPackage) {
		resolver.issue("invalid_plugin_go_package", path+".goPackage", "plugin Go package must be a portable import path")
	}
	if plugin.APIVersion != schema.CurrentPluginAPIVersion {
		resolver.issue("incompatible_plugin_api", path+".apiVersion", fmt.Sprintf("plugin API version must be %d", schema.CurrentPluginAPIVersion))
	}
	if plugin.Ridu == nil || !schema.IsValidSemanticVersionRange(plugin.Ridu.Minimum, plugin.Ridu.MaximumExclusive) {
		resolver.issue("invalid_plugin_ridu_compatibility", path+".ridu", "plugin Ridu compatibility requires semantic minimum and optional exclusive maximum versions")
	}
	resolved.Version, resolved.GoPackage, resolved.APIVersion = plugin.Version, plugin.GoPackage, plugin.APIVersion
	if plugin.Ridu != nil {
		resolved.Ridu = &schema.PluginCompatibility{Minimum: plugin.Ridu.Minimum, MaximumExclusive: plugin.Ridu.MaximumExclusive}
	}
	fieldKeys := make(map[string]struct{}, len(plugin.FieldTypes))
	for index, fieldType := range plugin.FieldTypes {
		fieldPath := fmt.Sprintf("%s.fieldTypes[%d]", path, index)
		if !schema.IsValidPluginKey(fieldType.Key) {
			resolver.issue("invalid_plugin_field_type_key", fieldPath+".key", "plugin field type key must be lowercase kebab-case")
		}
		if _, exists := fieldKeys[fieldType.Key]; exists {
			resolver.issue("duplicate_plugin_field_type", fieldPath+".key", fmt.Sprintf("plugin field type %q is already declared", fieldType.Key))
		}
		fieldKeys[fieldType.Key] = struct{}{}
		resolver.claimPluginField(fieldType.Key, fieldPath+".key")
		if !schema.IsValidAdminPluginPackage(fieldType.TypeScriptPackage) || !schema.IsValidAdminPluginExport(fieldType.TypeScriptOutput) || !schema.IsValidAdminPluginExport(fieldType.TypeScriptInput) || fieldType.TypeScriptWhere != "" && !schema.IsValidAdminPluginExport(fieldType.TypeScriptWhere) {
			resolver.issue("invalid_plugin_typescript_type", fieldPath, "plugin TypeScript mapping requires a package and named output/input/optional where exports")
		}
		if (fieldType.GoPackage == "") != (fieldType.GoType == "") || fieldType.GoPackage != "" && (!schema.IsValidGoPackage(fieldType.GoPackage) || !schema.IsValidAdminPluginExport(fieldType.GoType)) {
			resolver.issue("invalid_plugin_go_type", fieldPath, "plugin Go mapping must provide both a portable package and exported type")
		}
		if len(fieldType.JSONSchema) != 0 {
			var value any
			if json.Unmarshal(fieldType.JSONSchema, &value) != nil {
				resolver.issue("invalid_plugin_json_schema", fieldPath+".jsonSchema", "plugin JSON Schema must be valid JSON")
			}
		}
		resolved.FieldTypes = append(resolved.FieldTypes, schema.PluginFieldType{EmbeddedTypes: append([]string(nil), fieldType.EmbeddedTypes...), Key: fieldType.Key, TypeScriptPackage: fieldType.TypeScriptPackage, TypeScriptOutput: fieldType.TypeScriptOutput, TypeScriptInput: fieldType.TypeScriptInput, TypeScriptWhere: fieldType.TypeScriptWhere, GoPackage: fieldType.GoPackage, GoType: fieldType.GoType, JSONSchema: append(json.RawMessage(nil), fieldType.JSONSchema...)})
	}
	prefix := "ridu_plugin_" + strings.ReplaceAll(plugin.Key, "-", "_") + "_"
	adapters := make(map[schema.PluginDatabaseAdapter]struct{}, len(plugin.DatabaseContributions))
	for contributionIndex, contribution := range plugin.DatabaseContributions {
		contributionPath := fmt.Sprintf("%s.databaseContributions[%d]", path, contributionIndex)
		if contribution.Adapter != schema.PluginDatabaseAdapterPostgres && contribution.Adapter != schema.PluginDatabaseAdapterSQLite {
			resolver.issue("invalid_plugin_database_adapter", contributionPath+".adapter", "plugin database adapter must be postgres or sqlite")
		}
		if _, exists := adapters[contribution.Adapter]; exists {
			resolver.issue("duplicate_plugin_database_contribution", contributionPath+".adapter", fmt.Sprintf("plugin database adapter %q is already declared", contribution.Adapter))
		}
		adapters[contribution.Adapter] = struct{}{}
		if len(contribution.Migrations) == 0 && len(contribution.Tables) == 0 {
			resolver.issue("empty_plugin_database_contribution", contributionPath, "plugin database contribution must declare migrations or owned tables")
		}
		if len(contribution.Tables) != 0 && len(contribution.Migrations) == 0 {
			resolver.issue("missing_plugin_database_migration", contributionPath+".migrations", "plugin-owned tables require a migration history that creates them")
		}
		resolvedContribution := schema.PluginDatabaseContribution{Adapter: contribution.Adapter}
		for migrationIndex, pluginMigration := range contribution.Migrations {
			migrationPath := fmt.Sprintf("%s.migrations[%d]", contributionPath, migrationIndex)
			if pluginMigration.Version != uint32(migrationIndex+1) {
				resolver.issue("non_contiguous_plugin_migration", migrationPath+".version", fmt.Sprintf("plugin migration version must be %d", migrationIndex+1))
			}
			if !schema.IsValidPluginKey(pluginMigration.Name) || len(pluginMigration.UpSQL) == 0 || len(pluginMigration.DownSQL) == 0 {
				resolver.issue("invalid_plugin_migration", migrationPath, "plugin migrations require a lowercase kebab-case name and non-empty reversible SQL statements")
			}
			for statementIndex, statement := range append(append([]string(nil), pluginMigration.UpSQL...), pluginMigration.DownSQL...) {
				if !schema.IsValidPluginMigrationSQL(contribution.Adapter, statement) {
					resolver.issue("invalid_plugin_migration_sql", fmt.Sprintf("%s.sql[%d]", migrationPath, statementIndex), "plugin migration SQL statements must be non-empty, contain no NUL bytes, and leave transaction control to the adapter")
				}
			}
			resolvedContribution.Migrations = append(resolvedContribution.Migrations, schema.PluginMigration{Version: pluginMigration.Version, Name: pluginMigration.Name, UpSQL: append([]string(nil), pluginMigration.UpSQL...), DownSQL: append([]string(nil), pluginMigration.DownSQL...)})
		}
		tables := make(map[string]struct{}, len(contribution.Tables))
		for tableIndex, table := range contribution.Tables {
			tablePath := fmt.Sprintf("%s.tables[%d]", contributionPath, tableIndex)
			if !schema.IsValidPluginTable(table) || !strings.HasPrefix(table, prefix) {
				resolver.issue("invalid_plugin_database_table", tablePath, fmt.Sprintf("plugin database tables must use the %q prefix", prefix))
			}
			if _, exists := tables[table]; exists {
				resolver.issue("duplicate_plugin_database_table", tablePath, fmt.Sprintf("plugin database table %q is already declared", table))
			}
			tables[table] = struct{}{}
			resolvedContribution.Tables = append(resolvedContribution.Tables, table)
		}
		resolved.DatabaseContributions = append(resolved.DatabaseContributions, resolvedContribution)
	}
}

func validPluginEndpoint(method, path string) bool {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		return false
	}
	return path != "" && path == strings.TrimSpace(path) && !strings.ContainsAny(path, " \t\r\n") && !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "./") && !strings.Contains(path, "..") && !strings.ContainsAny(path, "?#*")
}

func (resolver *resolver) resolveCollection(index int, collection Collection) schema.Collection {
	path := fmt.Sprintf("collections[%d]", index)
	if collection.Upload {
		fields, err := UploadFields(collection.Fields, path+".fields")
		collection.Fields = fields
		if validation, ok := err.(*schema.ValidationError); ok {
			resolver.issues = append(resolver.issues, validation.Issues...)
		}
	}
	resolver.validateDefinitionIndexes(collection.Fields, path+".fields", false)
	labels := collection.Labels
	if strings.TrimSpace(labels.Singular) == "" {
		labels.Singular = humanize(singularize(string(collection.Slug)))
	} else {
		labels.Singular = strings.TrimSpace(labels.Singular)
	}
	labels.SingularTranslations = resolver.resolveTranslations(labels.SingularTranslations, path+".labels.singularTranslations")
	labels.PluralTranslations = resolver.resolveTranslations(labels.PluralTranslations, path+".labels.pluralTranslations")
	if strings.TrimSpace(labels.Plural) == "" {
		labels.Plural = humanize(string(collection.Slug))
	} else {
		labels.Plural = strings.TrimSpace(labels.Plural)
	}

	fieldResolver := &fieldResolver{
		resolver:     resolver,
		collectionID: effectiveCollectionID(collection),
		seenIDs:      make(map[schema.StableID]string),
		seenPaths:    make(map[string]string),
	}
	fields := fieldResolver.resolveFields(collection.Fields, path+".fields", nil)
	fieldResolver.validateSlugSources(fields)
	fieldResolver.validateFieldConditions(fields)
	admin := schema.CollectionAdmin{
		UseAsTitle: strings.TrimSpace(collection.Admin.UseAsTitle), Group: strings.TrimSpace(collection.Admin.Group),
		GroupTranslations:       resolver.resolveTranslations(collection.Admin.GroupTranslations, path+".admin.groupTranslations"),
		Description:             strings.TrimSpace(collection.Admin.Description),
		DescriptionTranslations: resolver.resolveTranslations(collection.Admin.DescriptionTranslations, path+".admin.descriptionTranslations"),
		FolderField:             strings.TrimSpace(collection.Admin.FolderField),
		ParentField:             strings.TrimSpace(collection.Admin.ParentField), DefaultColumns: append([]string(nil), collection.Admin.DefaultColumns...),
	}
	admin.LivePreview = resolver.resolveLivePreview(collection.Admin.LivePreview, fields, path+".admin.livePreview")
	fieldByName := make(map[string]schema.Field, len(fields))
	for _, candidate := range fields {
		fieldByName[candidate.Name] = candidate
	}
	validateNamedField := func(name, configPath string) *schema.Field {
		if name == "" {
			return nil
		}
		candidate, exists := fieldByName[name]
		if !exists {
			resolver.issue("unknown_admin_field", path+configPath, fmt.Sprintf("admin field %q does not exist", name))
			return nil
		}
		return &candidate
	}
	validateNamedField(admin.UseAsTitle, ".admin.useAsTitle")
	seenColumns := make(map[string]bool, len(admin.DefaultColumns))
	for columnIndex, column := range admin.DefaultColumns {
		column = strings.TrimSpace(column)
		admin.DefaultColumns[columnIndex] = column
		validColumn := column != "" && collectionDefaultColumnExists(collection, fieldByName, column)
		if column == "" || seenColumns[column] || !validColumn {
			if column == "" || seenColumns[column] {
				resolver.issue("invalid_default_column", fmt.Sprintf("%s.admin.defaultColumns[%d]", path, columnIndex), "default columns must name unique direct fields")
			} else {
				resolver.issue("unknown_admin_field", fmt.Sprintf("%s.admin.defaultColumns[%d]", path, columnIndex), fmt.Sprintf("admin field %q does not exist", column))
			}
		}
		seenColumns[column] = true
	}
	if folder := validateNamedField(admin.FolderField, ".admin.folderField"); folder != nil && (folder.Relationship == nil || folder.Relationship.HasMany || folder.Relationship.Polymorphic) {
		resolver.issue("invalid_folder_field", path+".admin.folderField", "folder field must be a singular non-polymorphic relationship")
	}
	if parent := validateNamedField(admin.ParentField, ".admin.parentField"); parent != nil && (parent.Relationship == nil || parent.Relationship.HasMany || parent.Relationship.Polymorphic || parent.Relationship.CollectionID != effectiveCollectionID(collection)) {
		resolver.issue("invalid_parent_field", path+".admin.parentField", "parent field must be a singular relationship to this collection")
	}
	if collection.Upload {
		markUploadMetadata(fields)
	}
	indexes := resolver.resolveCollectionIndexes(fields, collection.Indexes, path+".indexes")
	var authSettings *schema.AuthSettings
	if collection.Auth {
		duration := collection.SessionDuration
		if duration == 0 {
			duration = 24 * time.Hour
		}
		if duration < time.Minute {
			resolver.issue("invalid_session_duration", path+".auth.sessionDuration", "auth session duration must be at least one minute")
		}
		minimumLength := collection.PasswordMinLength
		if minimumLength == 0 {
			minimumLength = 8
		}
		maximumBytes := collection.PasswordMaxBytes
		if maximumBytes == 0 {
			maximumBytes = 72
		}
		bcryptCost := collection.PasswordBcryptCost
		if bcryptCost == 0 {
			bcryptCost = 10
		}
		maximumAttempts := collection.MaxLoginAttempts
		if maximumAttempts == 0 {
			maximumAttempts = 5
		}
		lockDuration := collection.LockDuration
		if lockDuration == 0 {
			lockDuration = 10 * time.Minute
		}
		if minimumLength < 1 {
			resolver.issue("invalid_password_min_length", path+".auth.password.minLength", "password minimum length must be positive")
		}
		if maximumBytes < minimumLength || maximumBytes > 72 {
			resolver.issue("invalid_password_max_bytes", path+".auth.password.maxBytes", "password maximum bytes must be between the minimum length and 72")
		}
		if bcryptCost < 4 || bcryptCost > 16 {
			resolver.issue("invalid_bcrypt_cost", path+".auth.password.bcryptCost", "bcrypt cost must be between 4 and 16")
		}
		if maximumAttempts < -1 {
			resolver.issue("invalid_max_login_attempts", path+".auth.maxLoginAttempts", "maximum login attempts must be positive or -1 to disable")
		}
		if maximumAttempts > 0 && lockDuration < time.Second {
			resolver.issue("invalid_lock_duration", path+".auth.lockDuration", "account lock duration must be at least one second")
		}
		passwordResetTTL := collection.PasswordResetTTL
		if passwordResetTTL == 0 {
			passwordResetTTL = time.Hour
		}
		if collection.PasswordReset && passwordResetTTL < time.Minute {
			resolver.issue("invalid_password_reset_duration", path+".auth.passwordReset.tokenDuration", "password reset token duration must be at least one minute")
		}
		verificationTTL := collection.VerificationTTL
		if verificationTTL == 0 {
			verificationTTL = 24 * time.Hour
		}
		if collection.VerifyEmail && verificationTTL < time.Minute {
			resolver.issue("invalid_verification_duration", path+".auth.verify.tokenDuration", "verification token duration must be at least one minute")
		}
		authSettings = &schema.AuthSettings{
			IdentityField: "email", SessionDurationSeconds: int64(duration / time.Second),
			PasswordMinLength: minimumLength, PasswordMaxBytes: maximumBytes,
			PasswordBcryptCost: bcryptCost, MaxLoginAttempts: maximumAttempts,
			LockDurationSeconds: int64(lockDuration / time.Second),
			PasswordReset:       collection.PasswordReset, PasswordResetTokenDurationSeconds: int64(passwordResetTTL / time.Second),
			VerifyEmail: collection.VerifyEmail, VerificationTokenDurationSeconds: int64(verificationTTL / time.Second),
			APIKeys: collection.APIKeys,
		}
		validIdentity := false
		for _, field := range fields {
			if field.Name == "email" && (field.Type == schema.FieldTypeText || field.Type == schema.FieldTypeEmail) && field.Required && field.Unique && !field.Localized {
				validIdentity = true
			}
		}
		if !validIdentity {
			resolver.issue("invalid_auth_identity", path+".fields", "auth collections require a required, unique, non-localized text or email field named email")
		}
	}
	var uploadSettings *schema.UploadSettings
	if collection.Upload {
		maximum := collection.UploadConfig.MaxFileSize
		if maximum == 0 {
			maximum = 10 << 20
		}
		if maximum < 1 {
			resolver.issue("invalid_upload_size", path+".upload.maxFileSize", "upload maximum file size must be positive")
		} else if maximum > 256<<20 {
			resolver.issue("invalid_upload_size", path+".upload.maxFileSize", "upload maximum file size must not exceed 256 MiB")
		}
		mimeTypes := append([]string(nil), collection.UploadConfig.MimeTypes...)
		if len(mimeTypes) == 0 {
			mimeTypes = []string{"application/octet-stream", "image/*"}
		}
		seenMime := make(map[string]bool, len(mimeTypes))
		for index, mimeType := range mimeTypes {
			mimeType = strings.ToLower(strings.TrimSpace(mimeType))
			mimeTypes[index] = mimeType
			if mimeType == "" || !strings.Contains(mimeType, "/") || seenMime[mimeType] {
				resolver.issue("invalid_upload_mime_type", fmt.Sprintf("%s.upload.mimeTypes[%d]", path, index), "upload MIME types must be unique type/subtype values or type/* patterns")
			}
			seenMime[mimeType] = true
		}
		sizes := make([]schema.ImageSize, len(collection.UploadConfig.ImageSizes))
		seenSizes := make(map[string]bool, len(sizes))
		if len(sizes) > 64 {
			resolver.issue("invalid_image_size_budget", path+".upload.imageSizes", "upload collections support at most 64 generated image sizes")
		}
		var aggregateImagePixels int64
		for index, size := range collection.UploadConfig.ImageSizes {
			fit := strings.TrimSpace(size.Fit)
			if fit == "" {
				fit = "cover"
			}
			pixels := int64(size.Width) * int64(size.Height)
			aggregateImagePixels += pixels
			if !schema.IsValidPluginKey(size.Name) || seenSizes[size.Name] || size.Width < 1 || size.Height < 1 || size.Width > 20_000 || size.Height > 20_000 || pixels > 40_000_000 || fit != "cover" && fit != "contain" {
				resolver.issue("invalid_image_size", fmt.Sprintf("%s.upload.imageSizes[%d]", path, index), "image sizes require a unique lowercase kebab-case name, cover or contain fit, dimensions up to 20,000, and at most 40 million pixels")
			}
			seenSizes[size.Name] = true
			sizes[index] = schema.ImageSize{Name: size.Name, Width: size.Width, Height: size.Height, Fit: fit}
		}
		if aggregateImagePixels > 100_000_000 {
			resolver.issue("invalid_image_size_budget", path+".upload.imageSizes", "generated image sizes must not exceed 100 million aggregate pixels")
		}
		uploadSettings = &schema.UploadSettings{MaxFileSize: maximum, MimeTypes: mimeTypes, Private: collection.UploadConfig.Private, ImageSizes: sizes}
	}
	var versionSettings *schema.VersionSettings
	if collection.Versions {
		maximum := collection.VersionConfig.MaxPerDocument
		if maximum == 0 {
			maximum = 100
		}
		if maximum < 1 {
			resolver.issue("invalid_version_retention", path+".versions.maxPerDocument", "version retention must be positive")
		}
		autosave := collection.VersionConfig.AutosaveInterval
		if autosave == 0 {
			autosave = 30 * time.Second
		}
		if autosave < time.Second {
			resolver.issue("invalid_autosave_interval", path+".versions.autosaveInterval", "autosave interval must be at least one second")
		}
		versionSettings = &schema.VersionSettings{Drafts: collection.VersionConfig.Drafts, MaxPerDocument: maximum, AutosaveIntervalSeconds: int64(autosave / time.Second)}
	}
	var documentLockSettings *schema.DocumentLockSettings
	if collection.LockDocuments {
		duration := collection.DocumentLockDuration
		if duration == 0 {
			duration = 5 * time.Minute
		}
		if duration < 10*time.Second {
			resolver.issue("invalid_document_lock_duration", path+".documentLocks.duration", "document lock duration must be at least ten seconds")
		}
		documentLockSettings = &schema.DocumentLockSettings{DurationSeconds: int64(duration / time.Second)}
	}

	return schema.Collection{
		ID:           effectiveCollectionID(collection),
		Slug:         collection.Slug,
		Labels:       labels,
		Admin:        admin,
		Capabilities: schema.Capabilities{Auth: collection.Auth, Upload: collection.Upload, Versions: collection.Versions, Trash: collection.Trash, Locking: collection.LockDocuments},
		Auth:         authSettings,
		Upload:       uploadSettings,
		Versions:     versionSettings,
		DocumentLock: documentLockSettings,
		Fields:       fields,
		Indexes:      indexes,
		Endpoints:    resolver.resolveEndpoints(collection.Endpoints, path+".endpoints"),
	}
}

func collectionDefaultColumnExists(collection Collection, fields map[string]schema.Field, name string) bool {
	if _, exists := fields[name]; exists {
		return true
	}
	switch name {
	case "id", "createdAt", "updatedAt":
		return true
	case "deletedAt":
		return collection.Trash
	case "_status", "_revision":
		return collection.Versions
	}
	return false
}

func (resolver *resolver) resolveGlobal(index int, global Global) schema.Global {
	path := fmt.Sprintf("globals[%d]", index)
	resolver.validateDefinitionIndexes(global.Fields, path+".fields", false)
	label := strings.TrimSpace(global.Label)
	if label == "" {
		label = humanize(string(global.Slug))
	}
	labelTranslations := resolver.resolveTranslations(global.LabelTranslations, path+".labelTranslations")
	fieldResolver := &fieldResolver{
		resolver:     resolver,
		collectionID: effectiveGlobalID(global),
		seenIDs:      make(map[schema.StableID]string),
		seenPaths:    make(map[string]string),
	}
	fields := fieldResolver.resolveFields(global.Fields, path+".fields", nil)
	fieldResolver.validateSlugSources(fields)
	fieldResolver.validateFieldConditions(fields)
	var versions *schema.VersionSettings
	if global.Versions {
		maximum := global.VersionConfig.MaxPerDocument
		if maximum == 0 {
			maximum = 100
		}
		if maximum < 1 {
			resolver.issue("invalid_version_retention", path+".versions.maxPerDocument", "version retention must be positive")
		}
		autosave := global.VersionConfig.AutosaveInterval
		if autosave == 0 {
			autosave = 30 * time.Second
		}
		if autosave < time.Second {
			resolver.issue("invalid_autosave_interval", path+".versions.autosaveInterval", "autosave interval must be at least one second")
		}
		versions = &schema.VersionSettings{
			Drafts: global.VersionConfig.Drafts, MaxPerDocument: maximum,
			AutosaveIntervalSeconds: int64(autosave / time.Second),
		}
	}
	return schema.Global{
		ID: effectiveGlobalID(global), Slug: global.Slug,
		Labels: schema.CollectionLabels{Singular: label, SingularTranslations: labelTranslations, Plural: label, PluralTranslations: cloneTranslations(labelTranslations)},
		Admin: schema.CollectionAdmin{
			Group: strings.TrimSpace(global.Admin.Group), GroupTranslations: resolver.resolveTranslations(global.Admin.GroupTranslations, path+".admin.groupTranslations"),
			Description: strings.TrimSpace(global.Admin.Description), DescriptionTranslations: resolver.resolveTranslations(global.Admin.DescriptionTranslations, path+".admin.descriptionTranslations"),
			LivePreview: resolver.resolveLivePreview(global.Admin.LivePreview, fields, path+".admin.livePreview"),
		},
		Capabilities: schema.Capabilities{Versions: global.Versions, Global: true},
		Versions:     versions,
		Fields:       fields,
		Endpoints:    resolver.resolveEndpoints(global.Endpoints, path+".endpoints"),
	}
}

func (resolver *resolver) resolveEndpoints(endpoints []Endpoint, path string) []schema.Endpoint {
	resolved := make([]schema.Endpoint, len(endpoints))
	seen := make(map[string]string, len(endpoints))
	shapes := make(map[string]string, len(endpoints))
	for index, endpoint := range endpoints {
		entryPath := fmt.Sprintf("%s[%d]", path, index)
		method := strings.ToUpper(strings.TrimSpace(endpoint.Method))
		endpointPath := strings.TrimSpace(endpoint.Path)
		if !schema.IsSupportedEndpointMethod(method) {
			resolver.issue("invalid_endpoint_method", entryPath+".method", "custom endpoint method must be CONNECT, DELETE, GET, HEAD, OPTIONS, PATCH, POST, or PUT")
		}
		if !schema.IsValidEndpointPath(endpointPath) {
			resolver.issue("invalid_endpoint_path", entryPath+".path", "custom endpoint path must begin with / and contain static or named :parameter segments without traversal, query, fragment, or empty segments")
		}
		shape := endpointPathShape(endpointPath)
		identity := method + " " + shape
		if previous, duplicate := seen[identity]; duplicate {
			resolver.issue("duplicate_endpoint", entryPath, fmt.Sprintf("custom endpoint %q is already declared at %s", identity, previous))
		} else if method != "" && endpointPath != "" {
			seen[identity] = entryPath
			if previousPath, exists := shapes[shape]; exists && previousPath != endpointPath {
				resolver.issue("conflicting_endpoint_parameters", entryPath+".path", fmt.Sprintf("custom endpoint path uses different parameter names from %q", previousPath))
			} else {
				shapes[shape] = endpointPath
			}
		}
		resolved[index] = schema.Endpoint{Method: method, Path: endpointPath, Summary: strings.TrimSpace(endpoint.Summary)}
	}
	return resolved
}

func endpointPathShape(path string) string {
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for index, segment := range segments {
		if strings.HasPrefix(segment, ":") {
			segments[index] = ":"
		}
	}
	return "/" + strings.Join(segments, "/")
}

func (resolver *resolver) validateRootEndpointNamespaces(endpoints []schema.Endpoint) {
	for index, endpoint := range endpoints {
		firstSegment, _, _ := strings.Cut(strings.TrimPrefix(endpoint.Path, "/"), "/")
		if strings.HasPrefix(firstSegment, ":") {
			resolver.issue("reserved_endpoint_namespace", fmt.Sprintf("endpoints[%d].path", index), "root custom endpoints require a static first segment so resource namespaces remain unambiguous")
		} else if strings.HasPrefix(endpoint.Path, "/collections/") || strings.HasPrefix(endpoint.Path, "/globals/") {
			resolver.issue("reserved_endpoint_namespace", fmt.Sprintf("endpoints[%d].path", index), "root custom endpoints cannot enter collection or global namespaces; declare the endpoint on that resource")
		}
	}
}

func (resolver *resolver) validateDefinitionIndexes(definitions field.Fields, fieldsPath string, repeated bool) {
	for index, node := range definitions {
		definition := field.Snapshot(node)
		fieldPath := fmt.Sprintf("%s[%d]", fieldsPath, index)
		if definition.Index() {
			switch {
			case repeated:
				resolver.issue("unsupported_index", fieldPath+".index", "fields beneath arrays or blocks cannot be indexed")
			case !definitionSupportsIndex(definition):
				resolver.issue("unsupported_index", fieldPath+".index", fmt.Sprintf("field type %q cannot be indexed", definition.Kind()))
			}
		}
		switch definition.Kind() {
		case field.KindGroup:
			resolver.validateDefinitionIndexes(definition.Fields(), fieldPath+".fields", repeated)
		case field.KindArray:
			resolver.validateDefinitionIndexes(definition.Fields(), fieldPath+".fields", true)
		case field.KindBlocks:
			for blockIndex, block := range definition.Blocks() {
				resolver.validateDefinitionIndexes(block.Fields, fmt.Sprintf("%s.blocks[%d].fields", fieldPath, blockIndex), true)
			}
		case field.KindRow, field.KindCollapsible:
			resolver.validateDefinitionIndexes(definition.Fields(), fieldPath+".fields", repeated)
		case field.KindTabs:
			for tabIndex, tab := range definition.Fields() {
				resolver.validateDefinitionIndexes(field.Fields{tab}, fmt.Sprintf("%s.tabs[%d]", fieldPath, tabIndex), repeated)
			}
		}
	}
}

func definitionSupportsIndex(definition field.View) bool {
	switch definition.Kind() {
	case field.KindText, field.KindCode, field.KindTextarea, field.KindEmail, field.KindDate,
		field.KindNumber, field.KindCheckbox, field.KindRadio:
		return true
	case field.KindSelect:
		return !definition.SelectHasMany()
	case field.KindRelationship:
		return !definition.RelationshipHasMany() && len(definition.RelationshipTargets()) == 1
	case field.KindUpload:
		return !definition.RelationshipHasMany()
	default:
		return false
	}
}

func (resolver *resolver) resolveCollectionIndexes(fields []schema.Field, indexes []CollectionIndex, path string) []schema.CollectionIndex {
	resolved := make([]schema.CollectionIndex, 0, len(indexes))
	seenIndexes := make(map[string]string, len(indexes))
	for index, candidate := range indexes {
		indexPath := fmt.Sprintf("%s[%d]", path, index)
		if len(candidate.Fields) < 2 || len(candidate.Fields) > 32 {
			resolver.issue("invalid_index_size", indexPath+".fields", "compound indexes require between 2 and 32 fields")
		}
		resolvedIndex := schema.CollectionIndex{Unique: candidate.Unique, Fields: make([]query.Path, 0, len(candidate.Fields))}
		seenFields := make(map[string]struct{}, len(candidate.Fields))
		for fieldIndex, authoredPath := range candidate.Fields {
			fieldConfigPath := fmt.Sprintf("%s.fields[%d]", indexPath, fieldIndex)
			parsed, err := query.ParsePath(strings.TrimSpace(authoredPath))
			if err != nil {
				resolver.issue("invalid_index_field", fieldConfigPath, err.Error())
				continue
			}
			canonical := parsed.String()
			if _, duplicate := seenFields[canonical]; duplicate {
				resolver.issue("duplicate_index_field", fieldConfigPath, fmt.Sprintf("index field %q was configured more than once", canonical))
				continue
			}
			seenFields[canonical] = struct{}{}
			terminal := fieldByPath(fields, parsed.Segments())
			if terminal == nil || !supportsIndexField(*terminal) {
				resolver.issue("unsupported_index_field", fieldConfigPath, fmt.Sprintf("index field %q must resolve through groups to a supported scalar or singular reference", canonical))
				continue
			}
			resolvedIndex.Fields = append(resolvedIndex.Fields, parsed)
		}
		signatureParts := make([]string, len(resolvedIndex.Fields))
		for fieldIndex, fieldPath := range resolvedIndex.Fields {
			signatureParts[fieldIndex] = fieldPath.String()
		}
		signature := strings.Join(signatureParts, "\x00")
		if previous, duplicate := seenIndexes[signature]; signature != "" && duplicate {
			resolver.issue("duplicate_index", indexPath+".fields", fmt.Sprintf("an index over the same ordered fields is already configured at %s", previous))
		} else if signature != "" {
			seenIndexes[signature] = indexPath + ".fields"
		}
		resolved = append(resolved, resolvedIndex)
	}
	return resolved
}

func supportsIndexField(candidate schema.Field) bool {
	if !schemaFieldSupportsIndexType(candidate) {
		return false
	}
	if candidate.Relationship != nil {
		return !candidate.Relationship.HasMany && !candidate.Relationship.Polymorphic
	}
	if candidate.Upload != nil {
		return !candidate.Upload.HasMany
	}
	return true
}

func definitionMinLength(definition field.View) *int {
	value, exists := definition.MinLength()
	if !exists {
		return nil
	}
	return &value
}

func definitionMaxLength(definition field.View) *int {
	value, exists := definition.MaxLength()
	if !exists {
		return nil
	}
	return &value
}

func definitionMin(definition field.View) *float64 {
	value, exists := definition.Min()
	if !exists {
		return nil
	}
	return &value
}

func definitionMax(definition field.View) *float64 {
	value, exists := definition.Max()
	if !exists {
		return nil
	}
	return &value
}

func definitionStep(definition field.View) *float64 {
	value, exists := definition.Step()
	if !exists {
		return nil
	}
	return &value
}

func schemaFieldSupportsIndexType(candidate schema.Field) bool {
	switch candidate.Type {
	case schema.FieldTypeText, schema.FieldTypeCode, schema.FieldTypeTextarea, schema.FieldTypeEmail,
		schema.FieldTypeDate, schema.FieldTypeNumber, schema.FieldTypeCheckbox, schema.FieldTypeSelect,
		schema.FieldTypeRadio, schema.FieldTypeRelationship, schema.FieldTypeUpload:
		return candidate.Category == schema.FieldCategoryScalar || candidate.Category == schema.FieldCategoryRelationship || candidate.Category == schema.FieldCategoryUpload
	default:
		return false
	}
}

var livePreviewPlaceholder = regexp.MustCompile(`\{([^{}]+)\}`)

func (resolver *resolver) resolveLivePreview(input LivePreviewConfig, fields []schema.Field, path string) *schema.LivePreview {
	previewURL := strings.TrimSpace(input.URL)
	if previewURL == "" {
		if len(input.Breakpoints) > 0 {
			resolver.issue("missing_live_preview_url", path+".url", "live preview breakpoints require a URL template")
		}
		return nil
	}
	parsed, err := url.Parse(previewURL)
	if err != nil || (parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https") || (parsed.Scheme == "" && !strings.HasPrefix(parsed.Path, "/")) {
		resolver.issue("invalid_live_preview_url", path+".url", "live preview URL must be an absolute HTTP(S) URL or an absolute application path")
	}
	for _, match := range livePreviewPlaceholder.FindAllStringSubmatch(previewURL, -1) {
		placeholder := match[1]
		if placeholder == "id" || placeholder == "collection" {
			continue
		}
		if !strings.HasPrefix(placeholder, "field:") {
			resolver.issue("invalid_live_preview_placeholder", path+".url", fmt.Sprintf("unsupported live preview placeholder %q", match[0]))
			continue
		}
		fieldPath, pathError := query.ParsePath(strings.TrimPrefix(placeholder, "field:"))
		if pathError != nil || fieldByPath(fields, fieldPath.Segments()) == nil {
			resolver.issue("unknown_live_preview_field", path+".url", fmt.Sprintf("live preview placeholder %q does not name a stored field", match[0]))
		}
	}
	if remainder := livePreviewPlaceholder.ReplaceAllString(previewURL, ""); strings.ContainsAny(remainder, "{}") {
		resolver.issue("invalid_live_preview_placeholder", path+".url", "live preview placeholders must use {id}, {collection}, or {field:path.to.value}")
	}

	breakpoints := make([]schema.PreviewBreakpoint, len(input.Breakpoints))
	seen := make(map[string]bool, len(input.Breakpoints))
	for index, candidate := range input.Breakpoints {
		breakpointPath := fmt.Sprintf("%s.breakpoints[%d]", path, index)
		name := strings.TrimSpace(candidate.Name)
		if !schema.IsValidPluginKey(name) || seen[name] {
			resolver.issue("invalid_live_preview_breakpoint", breakpointPath+".name", "breakpoint names must be unique lowercase kebab-case")
		}
		seen[name] = true
		label := strings.TrimSpace(candidate.Label)
		if label == "" {
			label = humanize(name)
		}
		if candidate.Width < 1 || candidate.Height < 1 {
			resolver.issue("invalid_live_preview_breakpoint", breakpointPath, "breakpoint width and height must be positive")
		}
		breakpoints[index] = schema.PreviewBreakpoint{
			Name: name, Label: label,
			LabelTranslations: resolver.resolveTranslations(candidate.LabelTranslations, breakpointPath+".labelTranslations"),
			Width:             candidate.Width, Height: candidate.Height,
		}
	}
	return &schema.LivePreview{URL: previewURL, Breakpoints: breakpoints}
}

func fieldByPath(fields []schema.Field, segments []string) *schema.Field {
	if len(segments) == 0 {
		return nil
	}
	for index := range fields {
		candidate := &fields[index]
		if candidate.Name != segments[0] {
			continue
		}
		if len(segments) == 1 {
			if candidate.Category == schema.FieldCategoryPresentation || candidate.Category == schema.FieldCategoryNested || candidate.Category == schema.FieldCategoryPlugin || candidate.Type == schema.FieldTypeJSON || candidate.Type == schema.FieldTypePoint {
				return nil
			}
			return candidate
		}
		if candidate.Type == schema.FieldTypeGroup && candidate.Nested != nil {
			return fieldByPath(candidate.Nested.ResolvedFields(), segments[1:])
		}
	}
	return nil
}

func (fieldResolver *fieldResolver) validateSlugSources(fields []schema.Field) {
	var inspect func([]schema.Field, bool)
	inspect = func(candidates []schema.Field, nested bool) {
		for _, candidate := range candidates {
			if candidate.Text != nil && candidate.Text.Slug != nil {
				configPath := strings.TrimSuffix(fieldResolver.seenPaths[candidate.Path.String()], ".name")
				if configPath == "" {
					configPath = candidate.Path.String()
				}
				sourcePath := candidate.Text.Slug.SourcePath
				switch {
				case nested:
					fieldResolver.resolver.issue("unsupported_nested_slug", configPath+".sourcePath", "slug fields must be resource-root fields so uniqueness remains store-enforced")
				case sourcePath.String() == "":
					// The constructor or path parser already reports the exact authoring issue.
				case sourcePath.String() == candidate.Path.String():
					fieldResolver.resolver.issue("invalid_slug_source", configPath+".sourcePath", "slug source must name a different field")
				default:
					chain := slugSourceFieldChain(fields, sourcePath.Segments())
					if len(chain) == 0 || !slugSourceFieldType(chain[len(chain)-1]) {
						fieldResolver.resolver.issue("invalid_slug_source", configPath+".sourcePath", fmt.Sprintf("slug source %q must name a string field through non-repeated groups", sourcePath.String()))
						break
					}
					for _, sourceField := range chain {
						if sourceField.Localized {
							fieldResolver.resolver.issue("unsupported_localized_slug_source", configPath+".sourcePath", fmt.Sprintf("slug source %q cannot be localized", sourcePath.String()))
							break
						}
					}
					source := chain[len(chain)-1]
					if source.Text != nil && source.Text.Slug != nil {
						fieldResolver.resolver.issue("invalid_slug_source", configPath+".sourcePath", "slug fields cannot derive from other slug fields")
					}
				}
			}
			inspect(schema.EmbeddedBlocks(candidate), true)
			if candidate.Nested != nil {
				inspect(candidate.Nested.ResolvedFields(), true)
			}
			if candidate.Blocks != nil {
				for _, block := range candidate.Blocks.ResolvedTypes() {
					inspect(block.ResolvedFields(), true)
				}
			}
		}
	}
	inspect(fields, false)
}

func (fieldResolver *fieldResolver) validateFieldConditions(fields []schema.Field) {
	var inspect func([]schema.Field)
	inspect = func(siblings []schema.Field) {
		for _, candidate := range siblings {
			if candidate.Admin.Condition != nil {
				configPath := strings.TrimSuffix(fieldResolver.seenPaths[candidate.Path.String()], ".name")
				if configPath == "" {
					configPath = candidate.Path.String()
				}
				fieldResolver.validateFieldConditionReferences(
					*candidate.Admin.Condition,
					fields,
					siblings,
					configPath+".admin.visibleWhen",
				)
			}
			inspect(schema.EmbeddedBlocks(candidate))
			if candidate.Nested != nil {
				inspect(candidate.Nested.ResolvedFields())
			}
			if candidate.Blocks != nil {
				for _, block := range candidate.Blocks.ResolvedTypes() {
					inspect(block.ResolvedFields())
				}
			}
		}
	}
	inspect(fields)
}

func (fieldResolver *fieldResolver) validateFieldConditionReferences(
	condition schema.FieldCondition,
	root, siblings []schema.Field,
	path string,
) {
	if condition.Predicate != nil {
		predicate := condition.Predicate
		if predicate.Path.String() == "" {
			return
		}
		candidates := root
		if predicate.Scope == schema.FieldConditionSibling {
			candidates = siblings
		}
		target := fieldByPath(candidates, predicate.Path.Segments())
		targetType := fieldConditionValueType(target)
		if targetType == "" {
			fieldResolver.resolver.issue(
				"invalid_field_condition_path",
				path+".reference.path",
				fmt.Sprintf("%s condition path %q must name a scalar field through non-repeated groups", predicate.Scope, predicate.Path.String()),
			)
			return
		}
		for index, value := range predicate.Values {
			if value.Type == "" || value.Type == targetType {
				continue
			}
			fieldResolver.resolver.issue(
				"invalid_field_condition_type",
				fmt.Sprintf("%s.values[%d]", path, index),
				fmt.Sprintf("condition path %q stores %s values but the predicate uses %s", predicate.Path.String(), targetType, value.Type),
			)
		}
	}
	for index, child := range condition.Conditions {
		fieldResolver.validateFieldConditionReferences(
			child,
			root,
			siblings,
			fmt.Sprintf("%s.conditions[%d]", path, index),
		)
	}
}

func fieldConditionValueType(candidate *schema.Field) schema.ValueType {
	switch referenceFilterFieldKind(candidate) {
	case referenceFilterString:
		return schema.ValueTypeString
	case referenceFilterNumber:
		return schema.ValueTypeNumber
	case referenceFilterBoolean:
		return schema.ValueTypeBoolean
	default:
		return ""
	}
}

func slugSourceFieldChain(fields []schema.Field, segments []string) []schema.Field {
	if len(segments) == 0 {
		return nil
	}
	for _, candidate := range fields {
		if candidate.Name != segments[0] {
			continue
		}
		chain := []schema.Field{candidate}
		if len(segments) == 1 {
			return chain
		}
		if candidate.Type != schema.FieldTypeGroup || candidate.Nested == nil {
			return nil
		}
		children := slugSourceFieldChain(candidate.Nested.ResolvedFields(), segments[1:])
		if len(children) == 0 {
			return nil
		}
		return append(chain, children...)
	}
	return nil
}

func slugSourceFieldType(candidate schema.Field) bool {
	switch candidate.Type {
	case schema.FieldTypeText, schema.FieldTypeTextarea, schema.FieldTypeCode, schema.FieldTypeEmail:
		return candidate.Category == schema.FieldCategoryScalar
	default:
		return false
	}
}

func singularize(slug string) string {
	if strings.HasSuffix(slug, "ies") && len(slug) > 3 {
		return strings.TrimSuffix(slug, "ies") + "y"
	}
	if strings.HasSuffix(slug, "s") && !strings.HasSuffix(slug, "ss") && len(slug) > 1 {
		return strings.TrimSuffix(slug, "s")
	}
	return slug
}

type fieldResolver struct {
	resolver     *resolver
	collectionID schema.StableID
	seenIDs      map[schema.StableID]string
	seenPaths    map[string]string
}

func (fieldResolver *fieldResolver) resolveFields(definitions field.Fields, configPath string, parentPath []string) []schema.Field {
	var resolved []schema.Field
	for index, node := range definitions {
		definition := field.Snapshot(node)
		path := fmt.Sprintf("%s[%d]", configPath, index)
		if definition.Kind() == field.KindRow {
			resolved = append(resolved, fieldResolver.resolveRow(definition, path, parentPath)...)
			continue
		}
		if definition.Kind() == field.KindCollapsible {
			resolved = append(resolved, fieldResolver.resolveCollapsible(definition, path, parentPath)...)
			continue
		}
		if definition.Kind() == field.KindTabs {
			resolved = append(resolved, fieldResolver.resolveTabs(definition, path, parentPath)...)
			continue
		}
		resolved = append(resolved, fieldResolver.resolveField(definition, path, parentPath))
	}
	var directTabGroup *schema.FieldTabGroup
	for index := range resolved {
		if resolved[index].Admin.Tab == "" || resolved[index].Admin.TabGroup != nil {
			continue
		}
		if directTabGroup == nil {
			directTabGroup = &schema.FieldTabGroup{ID: schema.StableID(string(resolved[index].ID) + "-tabs")}
		}
		resolved[index].Admin.TabGroup = directTabGroup
	}
	return resolved
}

func (fieldResolver *fieldResolver) resolveTabs(definition field.View, configPath string, parentPath []string) []schema.Field {
	if definition.IsUnnamedTab() {
		fieldResolver.validateLayoutDefinition(definition, configPath)
		if len(definition.Fields()) == 0 {
			fieldResolver.resolver.issue("missing_tab_fields", configPath+".fields", "tab requires at least one child field")
		}
		children := fieldResolver.resolveFields(definition.Fields(), configPath+".fields", parentPath)
		label := strings.TrimSpace(definition.Label())
		if label == "" {
			fieldResolver.resolver.issue("missing_tab_label", configPath+".label", "tab label must not be empty")
		}
		var group *schema.FieldTabGroup
		if len(children) > 0 {
			group = &schema.FieldTabGroup{ID: schema.StableID(string(children[0].ID) + "-tabs"), Extensions: fieldResolver.resolveAdminExtensions(definition)}
		}
		for i := range children {
			children[i].Admin.Tab = label
			children[i].Admin.TabTranslations = fieldResolver.resolver.resolveTranslations(definition.LabelTranslations(), configPath+".labelTranslations")
			children[i].Admin.TabGroup = group
		}
		return children
	}
	fieldResolver.validateLayoutDefinition(definition, configPath)
	tabs := definition.Fields()
	if len(tabs) == 0 {
		fieldResolver.resolver.issue("missing_tabs", configPath+".tabs", "tabs requires at least one named or unnamed tab")
		return nil
	}
	var resolved []schema.Field
	seenNames := make(map[string]bool, len(tabs))
	for index, node := range tabs {
		tab := field.Snapshot(node)
		tabPath := fmt.Sprintf("%s.tabs[%d]", configPath, index)
		label := strings.TrimSpace(tab.Label())
		if label == "" {
			fieldResolver.resolver.issue("missing_tab_label", tabPath+".label", "tab label must not be empty")
			label = "Tab"
		}
		if len(tab.Fields()) == 0 {
			fieldResolver.resolver.issue("missing_tab_fields", tabPath+".fields", "tab requires at least one child field")
			continue
		}
		if !tab.IsNamedTab() && !tab.IsUnnamedTab() {
			fieldResolver.resolver.issue("invalid_tab", tabPath, "tabs children must be named or unnamed tabs")
			continue
		}
		if tab.Name() == "" {
			fieldResolver.validateLayoutDefinition(tab, tabPath)
			children := fieldResolver.resolveFields(tab.Fields(), tabPath+".fields", parentPath)
			for childIndex := range children {
				children[childIndex].Admin.Tab = label
				children[childIndex].Admin.TabTranslations = fieldResolver.resolver.resolveTranslations(tab.LabelTranslations(), tabPath+".labelTranslations")
			}
			resolved = append(resolved, children...)
			continue
		}
		if seenNames[tab.Name()] {
			fieldResolver.resolver.issue("duplicate_named_tab", tabPath+".name", fmt.Sprintf("named tab %q is configured more than once", tab.Name()))
		}
		seenNames[tab.Name()] = true
		resolvedGroup := fieldResolver.resolveFieldWithNestedConfig(tab, tabPath, parentPath, tabPath+".fields")
		resolvedGroup.Admin.NamedTab = true
		resolvedGroup.Admin.Tab = label
		resolvedGroup.Admin.TabTranslations = fieldResolver.resolver.resolveTranslations(tab.LabelTranslations(), tabPath+".labelTranslations")
		resolved = append(resolved, resolvedGroup)
	}
	if len(resolved) > 0 {
		group := &schema.FieldTabGroup{ID: schema.StableID(string(resolved[0].ID) + "-tabs"), Extensions: fieldResolver.resolveAdminExtensions(definition)}
		for index := range resolved {
			resolved[index].Admin.TabGroup = group
		}
	}
	return resolved
}

func (fieldResolver *fieldResolver) resolveCollapsible(definition field.View, configPath string, parentPath []string) []schema.Field {
	fieldResolver.validateLayoutDefinition(definition, configPath)
	children := definition.Fields()
	if len(children) == 0 {
		fieldResolver.resolver.issue("missing_collapsible_fields", configPath+".fields", "collapsible requires at least one child field")
		return nil
	}
	for index, child := range children {
		if child.Kind() == field.KindRow || child.Kind() == field.KindCollapsible {
			fieldResolver.resolver.issue("nested_presentation_group", fmt.Sprintf("%s.fields[%d]", configPath, index), "rows and collapsibles cannot be nested directly inside a collapsible")
		}
	}
	resolved := fieldResolver.resolveFields(children, configPath+".fields", parentPath)
	if len(resolved) == 0 {
		return nil
	}
	groupID := schema.StableID(string(resolved[0].ID) + "-collapsible")
	label := strings.TrimSpace(definition.Label())
	if label == "" {
		label = strings.TrimSpace(definition.Name())
		if label == "" {
			label = "Details"
		} else {
			label = humanize(label)
		}
	}
	for index := range resolved {
		resolved[index].Admin.Collapsible = &schema.FieldCollapsible{
			ID: groupID, Label: label,
			LabelTranslations:  fieldResolver.resolver.resolveTranslations(definition.LabelTranslations(), configPath+".labelTranslations"),
			InitiallyCollapsed: definition.InitiallyCollapsed(),
			Extensions:         fieldResolver.resolveAdminExtensions(definition),
		}
	}
	return resolved
}

func (fieldResolver *fieldResolver) resolveRow(definition field.View, configPath string, parentPath []string) []schema.Field {
	fieldResolver.validateLayoutDefinition(definition, configPath)
	children := definition.Fields()
	if len(children) == 0 {
		fieldResolver.resolver.issue("missing_row_fields", configPath+".fields", "row requires at least one child field")
		return nil
	}
	for index, child := range children {
		if child.Kind() == field.KindRow {
			fieldResolver.resolver.issue("nested_row", fmt.Sprintf("%s.fields[%d]", configPath, index), "row fields cannot be nested directly inside another row")
		}
	}
	resolved := fieldResolver.resolveFields(children, configPath+".fields", parentPath)
	if len(resolved) == 0 {
		return nil
	}
	rowID := resolved[0].ID
	for index := range resolved {
		resolved[index].Admin.Row = &schema.FieldRow{ID: rowID, Extensions: fieldResolver.resolveAdminExtensions(definition)}
	}
	return resolved
}

func (fieldResolver *fieldResolver) resolveField(definition field.View, configPath string, parentPath []string) schema.Field {
	return fieldResolver.resolveFieldWithNestedConfig(definition, configPath, parentPath, configPath+".fields")
}

func (fieldResolver *fieldResolver) resolveFieldWithNestedConfig(definition field.View, configPath string, parentPath []string, nestedConfigPath string) schema.Field {
	if len(parentPath) > 48 {
		fieldResolver.resolver.issue("schema_depth_exceeded", configPath, "schema depth exceeds 48; recursive schemas are unsupported")
		return schema.Field{}
	}
	for _, issue := range definition.Issues() {
		fieldResolver.resolver.issue(issue.Code, joinConfigPath(configPath, issue.Path), issue.Message)
	}
	if definition.Unique() {
		fieldPath := strings.Join(append(append([]string(nil), parentPath...), definition.Name()), ".")
		switch {
		case len(parentPath) != 0:
			fieldResolver.resolver.issue("unsupported_unique", configPath+".unique", fmt.Sprintf("nested field %q cannot be unique because Ridu stores only enforce uniqueness on resource-root fields", fieldPath))
		case definition.Kind() == field.KindRelationship && len(definition.RelationshipTargets()) > 1:
			fieldResolver.resolver.issue("unsupported_unique", configPath+".unique", fmt.Sprintf("polymorphic relationship field %q cannot be unique because Ridu stores do not enforce uniqueness for polymorphic references", fieldPath))
		case (definition.Kind() == field.KindRelationship || definition.Kind() == field.KindUpload) && definition.RelationshipHasMany():
			fieldResolver.resolver.issue("unsupported_unique", configPath+".unique", fmt.Sprintf("has-many reference field %q cannot be unique because Ridu stores do not enforce uniqueness for list-valued references", fieldPath))
		case definition.Kind() == field.KindSelect && definition.SelectHasMany():
			fieldResolver.resolver.issue("unsupported_unique", configPath+".unique", fmt.Sprintf("multi-select field %q cannot be unique because Ridu stores do not enforce uniqueness for list-valued selects", fieldPath))
		}
	}
	if definition.Localized() && fieldResolver.resolver.input.Localization == nil {
		fieldResolver.resolver.issue("missing_localization_config", configPath+".localized", "localized fields require application localization configuration")
	}
	if definition.Sidebar() && len(parentPath) != 0 {
		fieldResolver.resolver.issue("unsupported_sidebar", configPath+".admin.sidebar", "sidebar placement is only supported for root fields")
	}

	pathSegments := append(append([]string(nil), parentPath...), definition.Name())
	fieldID := fieldResolver.effectiveFieldID(pathSegments)
	if previousPath, exists := fieldResolver.seenIDs[fieldID]; fieldID != "" && exists {
		fieldResolver.resolver.issue("duplicate_field_id", configPath+".name", fmt.Sprintf("field name derives ID %q, which is already used at %s", fieldID, previousPath))
	} else if fieldID != "" {
		fieldResolver.seenIDs[fieldID] = configPath + ".name"
	}

	if definition.Name() == "" {
		fieldResolver.resolver.issue("missing_field_name", configPath+".name", "field name must not be empty")
	} else if !schema.IsValidFieldName(definition.Name()) {
		fieldResolver.resolver.issue("invalid_field_name", configPath+".name", "field name must start with a lowercase letter and contain only letters, numbers, or underscores")
	} else if isReservedFieldName(definition.Name()) || len(parentPath) == 0 && isReservedWhereFieldName(definition.Name()) {
		fieldResolver.resolver.issue("reserved_field_name", configPath+".name", fmt.Sprintf("field name %q is reserved for framework document or query metadata", definition.Name()))
	}

	pathString := strings.Join(pathSegments, ".")
	if previousPath, exists := fieldResolver.seenPaths[pathString]; pathString != "" && exists {
		fieldResolver.resolver.issue("duplicate_field_path", configPath+".name", fmt.Sprintf("field path %q is already used at %s", pathString, previousPath))
	} else if pathString != "" {
		fieldResolver.seenPaths[pathString] = configPath + ".name"
	}

	manifestPath, pathError := query.NewPath(pathSegments...)
	if pathError != nil && definition.Name() != "" && schema.IsValidFieldName(definition.Name()) {
		fieldResolver.resolver.issue("invalid_field_path", configPath+".name", pathError.Error())
	}

	label := strings.TrimSpace(definition.Label())
	if label == "" {
		label = humanize(definition.Name())
	}
	resolved := schema.Field{
		ID:              schema.StableID(fieldID),
		Name:            definition.Name(),
		Path:            manifestPath,
		Required:        definition.Required(),
		Unique:          definition.Unique(),
		Index:           definition.Index(),
		Localized:       definition.Localized(),
		QueryRestricted: definition.AccessPolicy().Read != nil,
		Admin: schema.FieldAdmin{
			Extensions:              fieldResolver.resolveAdminExtensions(definition),
			Label:                   label,
			LabelTranslations:       fieldResolver.resolver.resolveTranslations(definition.LabelTranslations(), configPath+".admin.labelTranslations"),
			Description:             strings.TrimSpace(definition.Description()),
			DescriptionTranslations: fieldResolver.resolver.resolveTranslations(definition.DescriptionTranslations(), configPath+".admin.descriptionTranslations"),
			Placeholder:             strings.TrimSpace(definition.Placeholder()),
			PlaceholderTranslations: fieldResolver.resolver.resolveTranslations(definition.PlaceholderTranslations(), configPath+".admin.placeholderTranslations"),
			ReadOnly:                definition.ReadOnly(), Hidden: definition.Hidden(), Sidebar: definition.Sidebar(),
			Columns: definition.Columns(), Tab: strings.TrimSpace(definition.Tab()),
			TabTranslations: fieldResolver.resolver.resolveTranslations(definition.TabTranslations(), configPath+".admin.tabTranslations"),
		},
	}
	if reference, config := definition.Editor(); reference != "" {
		resolved.Admin.Editor = &schema.FieldEditor{Reference: reference, Config: config}
	}
	if pluginKey, component, config, exists := definition.AdminComponent(); exists {
		if !schema.IsValidPluginKey(pluginKey) {
			fieldResolver.resolver.issue("invalid_admin_component_plugin", configPath+".admin.editor.pluginKey", "admin component plugin must be a lowercase plugin key")
		}
		if !schema.IsValidAdminPluginExport(component) {
			fieldResolver.resolver.issue("invalid_admin_component_name", configPath+".admin.editor.key", "admin component must be a JavaScript identifier")
		}
		if _, paired := fieldResolver.resolver.adminPluginKeys[pluginKey]; !paired {
			fieldResolver.resolver.issue("missing_admin_component_plugin", configPath+".admin.editor.pluginKey", fmt.Sprintf("admin component plugin %q is not registered with a paired admin package", pluginKey))
		}
		if len(config) != 0 && !isJSONObject(config) {
			fieldResolver.resolver.issue("invalid_admin_component_config", configPath+".admin.editor.config", "admin component config must be a JSON object")
		}
		resolved.Admin.Component = &schema.FieldAdminComponent{Plugin: pluginKey, Component: component, Config: append(json.RawMessage(nil), config...)}
	}
	if condition := definition.AdminPolicy().VisibleWhen; !condition.IsZero() {
		resolved.Admin.Condition = fieldResolver.resolveFieldCondition(condition, configPath+".admin.visibleWhen")
	}
	resolved.Admin.NamedTab = definition.IsNamedTab()
	resolved.DynamicDefault = definition.BehaviorSummary().DynamicDefault
	resolved.LiveValidation = definition.BehaviorSummary().LiveValidators > 0
	if defaultValue, exists := definition.Default(); exists {
		value := defaultValue.String()
		resolved.Default = &value
	}

	switch definition.Kind() {
	case field.KindTextList:
		resolved.Type, resolved.Category = schema.FieldTypeTextList, schema.FieldCategoryScalar
		resolved.List = &schema.PrimitiveListField{MinRows: definition.MinRows(), MaxRows: definition.MaxRows()}
		resolved.Text = &schema.TextField{MinLength: definitionMinLength(definition), MaxLength: definitionMaxLength(definition)}
	case field.KindNumberList:
		resolved.Type, resolved.Category = schema.FieldTypeNumberList, schema.FieldCategoryScalar
		resolved.List = &schema.PrimitiveListField{MinRows: definition.MinRows(), MaxRows: definition.MaxRows()}
		resolved.Number = &schema.NumberField{Min: definitionMin(definition), Max: definitionMax(definition)}
	case field.KindText:
		resolved.Type = schema.FieldTypeText
		resolved.Category = schema.FieldCategoryScalar
		resolved.Text = &schema.TextField{MinLength: definitionMinLength(definition), MaxLength: definitionMaxLength(definition)}
		if sourcePath, slug := definition.SlugSource(); slug {
			parsed, pathError := query.ParsePath(sourcePath)
			if pathError != nil && sourcePath != "" {
				fieldResolver.resolver.issue("invalid_slug_source", configPath+".sourcePath", pathError.Error())
			}
			resolved.Text.Slug = &schema.SlugField{SourcePath: parsed}
		}
	case field.KindCode:
		resolved.Type, resolved.Category = schema.FieldTypeCode, schema.FieldCategoryScalar
		resolved.Code = &schema.CodeField{Language: definition.CodeLanguage(), MinLength: definitionMinLength(definition), MaxLength: definitionMaxLength(definition)}
	case field.KindTextarea:
		resolved.Type, resolved.Category = schema.FieldTypeTextarea, schema.FieldCategoryScalar
		resolved.Textarea = &schema.TextField{MinLength: definitionMinLength(definition), MaxLength: definitionMaxLength(definition)}
	case field.KindEmail:
		resolved.Type, resolved.Category = schema.FieldTypeEmail, schema.FieldCategoryScalar
	case field.KindDate:
		resolved.Type, resolved.Category = schema.FieldTypeDate, schema.FieldCategoryScalar
		resolved.Date = &schema.DateField{Format: schema.DateFormat(definition.DateFormat())}
	case field.KindNumber:
		resolved.Type, resolved.Category = schema.FieldTypeNumber, schema.FieldCategoryScalar
		resolved.Number = &schema.NumberField{Min: definitionMin(definition), Max: definitionMax(definition), Step: definitionStep(definition)}
	case field.KindCheckbox:
		resolved.Type, resolved.Category = schema.FieldTypeCheckbox, schema.FieldCategoryScalar
	case field.KindJSON:
		resolved.Type, resolved.Category = schema.FieldTypeJSON, schema.FieldCategoryScalar
	case field.KindSelect:
		resolved.Type = schema.FieldTypeSelect
		resolved.Category = schema.FieldCategoryScalar
		resolved.Select = fieldResolver.resolveSelect(definition, configPath)
	case field.KindRadio:
		resolved.Type, resolved.Category = schema.FieldTypeRadio, schema.FieldCategoryScalar
		resolved.Select = fieldResolver.resolveSelect(definition, configPath)
	case field.KindPoint:
		resolved.Type, resolved.Category = schema.FieldTypePoint, schema.FieldCategoryScalar
		resolved.Point = &schema.PointField{}
	case field.KindUI:
		resolved.Type, resolved.Category = schema.FieldTypeUI, schema.FieldCategoryPresentation
		resolved.UI = &schema.UIField{}
	case field.KindJoin:
		if len(pathSegments) != 1 {
			fieldResolver.resolver.issue("nested_join", configPath, "join fields must be declared at the collection root")
		}
		resolved.Type, resolved.Category = schema.FieldTypeJoin, schema.FieldCategoryPresentation
		resolved.Join = fieldResolver.resolveJoin(definition, configPath)
	case field.KindVirtual:
		if len(pathSegments) != 1 {
			fieldResolver.resolver.issue("nested_virtual", configPath, "virtual fields must be declared at the collection or global root")
		}
		resolved.Type, resolved.Category = schema.FieldTypeVirtual, schema.FieldCategoryPresentation
		valueType := schema.ValueType(definition.ValueType())
		if valueType != schema.ValueTypeString && valueType != schema.ValueTypeNumber && valueType != schema.ValueTypeBoolean && valueType != schema.ValueTypeJSON {
			fieldResolver.resolver.issue("invalid_virtual_type", configPath+".valueType", "virtual fields require a string, number, boolean, or JSON value type")
		}
		resolved.Virtual = &schema.VirtualField{ValueType: valueType}
	case field.KindRelationship:
		resolved.Type = schema.FieldTypeRelationship
		resolved.Category = schema.FieldCategoryRelationship
		resolved.Relationship = fieldResolver.resolveRelationship(definition, configPath)
	case field.KindUpload:
		resolved.Type, resolved.Category = schema.FieldTypeUpload, schema.FieldCategoryUpload
		resolved.Upload = fieldResolver.resolveUpload(definition, configPath)
	case field.KindGroup:
		resolved.Type = schema.FieldTypeGroup
		resolved.Category = schema.FieldCategoryNested
		children := definition.Fields()
		if len(children) == 0 {
			fieldResolver.resolver.issue("missing_nested_fields", nestedConfigPath, "group field requires at least one child field")
		}
		resolved.Nested = &schema.NestedField{
			Fields: fieldResolver.resolveFields(children, nestedConfigPath, pathSegments),
		}
	case field.KindArray:
		resolved.Type, resolved.Category = schema.FieldTypeArray, schema.FieldCategoryNested
		children := definition.Fields()
		if len(children) == 0 {
			fieldResolver.resolver.issue("missing_nested_fields", configPath+".fields", "array field requires at least one child field")
		}
		resolved.Nested = &schema.NestedField{
			Fields:  fieldResolver.resolveFields(children, configPath+".fields", pathSegments),
			MinRows: definition.MinRows(), MaxRows: definition.MaxRows(), RowLabel: definition.RowLabel(),
			RowLabelComponent: fieldResolver.resolveRowLabelComponent(definition, configPath),
		}
		rowLabels := definition.RowLabels()
		if rowLabels.Singular != "" || rowLabels.Plural != "" || len(rowLabels.SingularTranslations) != 0 || len(rowLabels.PluralTranslations) != 0 {
			singular, plural := strings.TrimSpace(rowLabels.Singular), strings.TrimSpace(rowLabels.Plural)
			if singular == "" {
				singular = "Row"
			}
			if plural == "" {
				plural = "Rows"
			}
			resolved.Nested.RowLabels = &schema.ArrayRowLabels{
				Singular:             singular,
				SingularTranslations: fieldResolver.resolver.resolveTranslations(rowLabels.SingularTranslations, configPath+".admin.rowLabels.singularTranslations"),
				Plural:               plural,
				PluralTranslations:   fieldResolver.resolver.resolveTranslations(rowLabels.PluralTranslations, configPath+".admin.rowLabels.pluralTranslations"),
			}
		}
		if rowLabel := definition.RowLabel(); rowLabel != "" {
			found := false
			for _, child := range children {
				if child.Name() == rowLabel && field.Snapshot(child).Category() != field.CategoryPresentation {
					found = true
					break
				}
			}
			if !found {
				fieldResolver.resolver.issue("unknown_row_label", configPath+".admin.rowLabelPath", "row label must name a direct stored child field")
			}
		}
	case field.KindBlocks:
		resolved.Type, resolved.Category = schema.FieldTypeBlocks, schema.FieldCategoryNested
		resolved.Blocks = fieldResolver.resolveBlocks(definition.Blocks(), definition.BlockReferences(), configPath, pathSegments)
		resolved.Blocks.MinRows = definition.MinRows()
		resolved.Blocks.MaxRows = definition.MaxRows()
		if component := fieldResolver.resolveRowLabelComponent(definition, configPath); component != nil {
			resolved.Nested = &schema.NestedField{Fields: []schema.Field{}, RowLabelComponent: component}
		}
	case field.KindPlugin:
		resolved.Type, resolved.Category = schema.FieldTypePlugin, schema.FieldCategoryPlugin
		key := definition.PluginKey()
		if !schema.IsValidPluginKey(key) {
			fieldResolver.resolver.issue("invalid_field_plugin", configPath+".plugin.key", "field plugin key must be lowercase kebab-case")
		} else if _, exists := fieldResolver.resolver.pluginFieldKeys[key]; !exists {
			fieldResolver.resolver.issue("missing_field_plugin", configPath+".plugin.key", fmt.Sprintf("field plugin %q is not registered", key))
		}
		config := definition.PluginConfig()
		if len(config) == 0 || !json.Valid(config) {
			fieldResolver.resolver.issue("invalid_field_plugin_config", configPath+".plugin.config", "field plugin config must be valid JSON")
			config = json.RawMessage(`{}`)
		}
		resolved.Plugin = &schema.PluginField{Key: key, Config: config, ReferenceKeys: definition.PluginReferenceKeys(), EmbeddedTrees: fieldResolver.resolveEmbeddedTrees(definition, configPath, pathSegments)}
	default:
		fieldResolver.resolver.issue("unknown_field_kind", configPath+".type", fmt.Sprintf("unknown field kind %q", definition.Kind()))
	}
	if resolved.Localized {
		clearDescendantLocalization(&resolved)
	}
	return resolved
}

func (fieldResolver *fieldResolver) resolveAdminExtensions(definition field.View) map[string]json.RawMessage {
	declared := definition.AdminPolicy().Extensions
	if len(declared) == 0 {
		return nil
	}
	result := make(map[string]json.RawMessage, len(declared))
	for key, value := range declared {
		encoded, err := json.Marshal(value)
		if err != nil {
			// The graph validator provides the authored, deterministic diagnostic.
			// Keep the schema boundary total for invalid component configuration.
			continue
		}
		result[key] = encoded
	}
	return result
}

func (fieldResolver *fieldResolver) resolveRowLabelComponent(definition field.View, configPath string) *schema.FieldAdminComponent {
	if reference, config := definition.LocalRowLabel(); reference != "" {
		return &schema.FieldAdminComponent{Reference: reference, Config: config}
	}
	pluginKey, component, config, exists := definition.RowLabelComponent()
	if !exists {
		return nil
	}
	componentPath := configPath + ".admin.rowLabel"
	if !schema.IsValidPluginKey(pluginKey) {
		fieldResolver.resolver.issue("invalid_row_label_component_plugin", componentPath+".pluginKey", "row label component plugin must be a lowercase plugin key")
	}
	if !schema.IsValidAdminPluginExport(component) {
		fieldResolver.resolver.issue("invalid_row_label_component_name", componentPath+".key", "row label component must be a JavaScript identifier")
	}
	if _, paired := fieldResolver.resolver.adminPluginKeys[pluginKey]; !paired {
		fieldResolver.resolver.issue("missing_row_label_component_plugin", componentPath+".pluginKey", fmt.Sprintf("row label component plugin %q is not registered with a paired admin package", pluginKey))
	}
	if len(config) != 0 && !isJSONObject(config) {
		fieldResolver.resolver.issue("invalid_row_label_component_config", componentPath+".config", "row label component config must be a JSON object")
	}
	return &schema.FieldAdminComponent{Plugin: pluginKey, Component: component, Config: append(json.RawMessage(nil), config...)}
}

func (fieldResolver *fieldResolver) resolveFieldCondition(condition field.Condition, configPath string) *schema.FieldCondition {
	resolved := &schema.FieldCondition{Kind: schema.FieldConditionKind(condition.Kind())}
	children := condition.Conditions()
	if len(children) != 0 {
		resolved.Conditions = make([]schema.FieldCondition, len(children))
		for index, child := range children {
			resolved.Conditions[index] = *fieldResolver.resolveFieldCondition(child, fmt.Sprintf("%s.conditions[%d]", configPath, index))
		}
	}
	if condition.Kind() != field.ConditionKindPredicate {
		return resolved
	}
	// Structural reference diagnostics come from the field declaration; avoid
	// adding query-parser terminology for the same malformed authoring path.
	if condition.Reference().Err() != nil {
		return resolved
	}
	conditionPath, _ := query.ParsePath(condition.Reference().Path())
	values := condition.Values()
	resolvedValues := make([]schema.FieldConditionValue, len(values))
	for index, value := range values {
		resolvedValues[index] = resolvedFieldConditionValue(value)
	}
	resolved.Predicate = &schema.FieldConditionPredicate{
		Scope:    conditionReferenceScope(condition.Reference()),
		Path:     conditionPath,
		Operator: schema.FieldConditionOperator(condition.Operator()),
		Values:   resolvedValues,
	}
	return resolved
}

func conditionReferenceScope(reference field.Reference) schema.FieldConditionScope {
	if reference.Scope() == field.RootScope {
		return schema.FieldConditionDocument
	}
	return schema.FieldConditionSibling
}

func resolvedFieldConditionValue(value field.DefaultValue) schema.FieldConditionValue {
	resolved := schema.FieldConditionValue{Value: value.String()}
	switch value.Kind() {
	case field.DefaultString:
		resolved.Type = schema.ValueTypeString
	case field.DefaultNumber:
		resolved.Type = schema.ValueTypeNumber
	case field.DefaultBoolean:
		resolved.Type = schema.ValueTypeBoolean
	}
	return resolved
}

func clearDescendantLocalization(field *schema.Field) {
	if field.Nested != nil {
		for index := range field.Nested.ResolvedFields() {
			field.Nested.ResolvedFields()[index].Localized = false
			clearDescendantLocalization(&field.Nested.ResolvedFields()[index])
		}
	}
	if field.Blocks != nil {
		for blockIndex := range field.Blocks.ResolvedTypes() {
			for fieldIndex := range field.Blocks.ResolvedTypes()[blockIndex].ResolvedFields() {
				field.Blocks.ResolvedTypes()[blockIndex].ResolvedFields()[fieldIndex].Localized = false
				clearDescendantLocalization(&field.Blocks.ResolvedTypes()[blockIndex].ResolvedFields()[fieldIndex])
			}
		}
	}
	if field.Plugin != nil {
		for i := range field.Plugin.EmbeddedTrees {
			for j := range field.Plugin.EmbeddedTrees[i].Cases {
				for k := range field.Plugin.EmbeddedTrees[i].Cases[j].ResolvedTypes() {
					for n := range field.Plugin.EmbeddedTrees[i].Cases[j].ResolvedTypes()[k].ResolvedFields() {
						child := &field.Plugin.EmbeddedTrees[i].Cases[j].ResolvedTypes()[k].ResolvedFields()[n]
						child.Localized = false
						clearDescendantLocalization(child)
					}
				}
			}
		}
	}

}

func (fieldResolver *fieldResolver) effectiveFieldID(pathSegments []string) schema.StableID {
	if fieldResolver.collectionID == "" || len(pathSegments) == 0 {
		return ""
	}
	parts := []string{string(fieldResolver.collectionID)}
	for _, segment := range pathSegments {
		if !schema.IsValidFieldName(segment) && !schema.IsValidPluginKey(segment) {
			return ""
		}
		parts = append(parts, kebabCase(segment))
	}
	derived := strings.Join(parts, "-")
	if !schema.IsValidStableID(derived) {
		return ""
	}
	return schema.StableID(derived)
}

func kebabCase(value string) string {
	var result []rune
	for index, current := range []rune(value) {
		if current == '_' || current == '-' {
			if len(result) != 0 && result[len(result)-1] != '-' {
				result = append(result, '-')
			}
			continue
		}
		if unicode.IsUpper(current) {
			if index > 0 && len(result) != 0 && result[len(result)-1] != '-' {
				result = append(result, '-')
			}
			current = unicode.ToLower(current)
		}
		result = append(result, current)
	}
	return string(result)
}

func (fieldResolver *fieldResolver) resolveUpload(definition field.View, configPath string) *schema.UploadField {
	targets := definition.RelationshipTargets()
	resolved := &schema.UploadField{
		HasMany:  definition.RelationshipHasMany(),
		OnDelete: fieldResolver.resolveReferenceDeleteAction(definition, configPath),
	}
	for index, filter := range definition.RelationshipFilters() {
		targetPath, targetError := query.ParsePath(filter.TargetPath)
		var sourcePath *query.Path
		if filter.SourcePath != "" {
			parsed, sourceError := query.ParsePath(filter.SourcePath)
			if sourceError != nil {
				fieldResolver.resolver.issue("invalid_relationship_filter", fmt.Sprintf("%s.filterOptionRules[%d]", configPath, index), "upload option filter paths must be valid field paths")
				continue
			}
			sourcePath = &parsed
		}
		if targetError != nil {
			fieldResolver.resolver.issue("invalid_relationship_filter", fmt.Sprintf("%s.filterOptionRules[%d]", configPath, index), "upload option filter paths must be valid field paths")
			continue
		}
		resolved.OptionFilters = append(resolved.OptionFilters, schema.RelationshipFilter{
			CollectionSlug: schema.CollectionSlug(filter.Collection),
			TargetPath:     targetPath,
			Operator:       string(filter.Operator),
			SourcePath:     sourcePath,
			Value:          resolvedRelationshipFilterValue(filter.Literal),
		})
	}
	if len(targets) != 1 {
		fieldResolver.resolver.issue("invalid_upload_target", configPath+".target", "upload fields require exactly one target collection")
		return resolved
	}
	target := schema.CollectionSlug(targets[0])
	collection, exists := fieldResolver.resolver.collectionSlugs[target]
	if !exists {
		fieldResolver.resolver.issue("missing_upload_collection", configPath+".target", fmt.Sprintf("upload target collection %q does not exist", target))
		return resolved
	}
	if !collection.upload {
		fieldResolver.resolver.issue("invalid_upload_collection", configPath+".target", fmt.Sprintf("collection %q is not upload-enabled", target))
	}
	resolved.CollectionID, resolved.CollectionSlug = collection.id, target
	return resolved
}

func (fieldResolver *fieldResolver) resolveSelect(definition field.View, configPath string) *schema.SelectField {
	options := definition.Options()
	if len(options) == 0 {
		fieldResolver.resolver.issue("missing_select_options", configPath+".options", "select field requires at least one option")
	}

	seenValues := make(map[string]string)
	resolved := make([]schema.SelectOption, len(options))
	for index, option := range options {
		path := fmt.Sprintf("%s.options[%d]", configPath, index)
		if option.Value == "" {
			fieldResolver.resolver.issue("missing_select_value", path+".value", "select option value must not be empty")
		}
		if previousPath, exists := seenValues[option.Value]; option.Value != "" && exists {
			fieldResolver.resolver.issue("duplicate_select_value", path+".value", fmt.Sprintf("select value %q is already used at %s", option.Value, previousPath))
		} else if option.Value != "" {
			seenValues[option.Value] = path + ".value"
		}
		label := strings.TrimSpace(option.Label)
		if label == "" {
			label = humanize(option.Value)
		}
		resolved[index] = schema.SelectOption{
			Value: option.Value, Label: label,
			LabelTranslations: fieldResolver.resolver.resolveTranslations(option.LabelTranslations, path+".labelTranslations"),
		}
	}

	if defaultValue, exists := definition.Default(); exists {
		if _, valid := seenValues[defaultValue.String()]; !valid {
			fieldResolver.resolver.issue("invalid_select_default", configPath+".default", fmt.Sprintf("default value %q is not one of the configured select options", defaultValue.String()))
		}
	}
	hasMany := definition.SelectHasMany()
	defaults := definition.SelectDefaults()
	seenDefaults := make(map[string]bool, len(defaults))
	for index, value := range defaults {
		path := fmt.Sprintf("%s.default[%d]", configPath, index)
		if _, valid := seenValues[value]; !valid {
			fieldResolver.resolver.issue("invalid_select_default", path, fmt.Sprintf("default value %q is not one of the configured select options", value))
		}
		if seenDefaults[value] {
			fieldResolver.resolver.issue("duplicate_select_default", path, fmt.Sprintf("default value %q was configured more than once", value))
		}
		seenDefaults[value] = true
	}
	return &schema.SelectField{
		Options:       resolved,
		HasMany:       hasMany,
		DefaultValues: append([]string(nil), defaults...),
	}
}

func (fieldResolver *fieldResolver) resolveRelationship(definition field.View, configPath string) *schema.RelationshipField {
	targets := definition.RelationshipTargets()
	resolved := &schema.RelationshipField{
		HasMany:     definition.RelationshipHasMany(),
		Polymorphic: len(targets) > 1,
		OnDelete:    fieldResolver.resolveReferenceDeleteAction(definition, configPath),
	}
	for index, filter := range definition.RelationshipFilters() {
		targetPath, targetError := query.ParsePath(filter.TargetPath)
		var sourcePath *query.Path
		if filter.SourcePath != "" {
			parsed, sourceError := query.ParsePath(filter.SourcePath)
			if sourceError != nil {
				fieldResolver.resolver.issue("invalid_relationship_filter", fmt.Sprintf("%s.filterOptionRules[%d]", configPath, index), "relationship option filter paths must be valid field paths")
				continue
			}
			sourcePath = &parsed
		}
		if targetError != nil {
			fieldResolver.resolver.issue("invalid_relationship_filter", fmt.Sprintf("%s.filterOptionRules[%d]", configPath, index), "relationship option filter paths must be valid field paths")
			continue
		}
		resolved.OptionFilters = append(resolved.OptionFilters, schema.RelationshipFilter{
			CollectionSlug: schema.CollectionSlug(filter.Collection),
			TargetPath:     targetPath,
			Operator:       string(filter.Operator),
			SourcePath:     sourcePath,
			Value:          resolvedRelationshipFilterValue(filter.Literal),
		})
	}
	if len(targets) == 0 {
		fieldResolver.resolver.issue("missing_relationship_target", configPath+".target", "relationship field requires a target collection slug")
		return resolved
	}
	seen := make(map[schema.CollectionSlug]bool, len(targets))
	for index, rawTarget := range targets {
		target := schema.CollectionSlug(rawTarget)
		path := configPath + ".target"
		if len(targets) > 1 {
			path = fmt.Sprintf("%s.targets[%d]", configPath, index)
		}
		if !schema.IsValidCollectionSlug(string(target)) {
			fieldResolver.resolver.issue("invalid_relationship_target", path, "relationship target must be a lowercase kebab-case collection slug")
			continue
		}
		if seen[target] {
			fieldResolver.resolver.issue("duplicate_relationship_target", path, fmt.Sprintf("relationship target %q is configured more than once", target))
			continue
		}
		seen[target] = true
		collection, exists := fieldResolver.resolver.collectionSlugs[target]
		if !exists {
			fieldResolver.resolver.issue("missing_relationship", path, fmt.Sprintf("relationship target collection %q does not exist", target))
			continue
		}
		resolved.Targets = append(resolved.Targets, schema.RelationshipTarget{CollectionID: collection.id, CollectionSlug: target})
	}
	if len(resolved.Targets) == 1 {
		resolved.CollectionID = resolved.Targets[0].CollectionID
		resolved.CollectionSlug = resolved.Targets[0].CollectionSlug
		resolved.Targets = nil
	}
	return resolved
}

func (fieldResolver *fieldResolver) resolveReferenceDeleteAction(definition field.View, configPath string) schema.ReferenceDeleteAction {
	action := schema.ReferenceDeleteAction(definition.ReferenceDeleteAction())
	if action == "" {
		if definition.Required() {
			return schema.ReferenceDeleteRestrict
		}
		return schema.ReferenceDeleteNullify
	}
	if action == schema.ReferenceDeleteNullify && definition.Required() {
		fieldResolver.resolver.issue(
			"invalid_reference_delete_action",
			configPath+".onDelete",
			"required references cannot be nullified; use restrict or omit OnDelete to use the safe default",
		)
	}
	return action
}

func (fieldResolver *fieldResolver) resolveJoin(definition field.View, configPath string) *schema.JoinField {
	target := schema.CollectionSlug(definition.JoinCollection())
	allowCreate := definition.JoinAllowCreate()
	join := &schema.JoinField{
		CollectionSlug: target,
		Limit:          definition.JoinLimit(),
		DefaultColumns: definition.JoinDefaultColumns(),
		DefaultSort:    definition.JoinDefaultSort(),
		AllowCreate:    &allowCreate,
	}
	reference, exists := fieldResolver.resolver.collectionSlugs[target]
	if !exists {
		fieldResolver.resolver.issue("missing_join_collection", configPath+".collection", fmt.Sprintf("join collection %q does not exist", target))
	} else {
		join.CollectionID = reference.id
	}
	on, err := query.ParsePath(definition.JoinOn())
	if err != nil {
		fieldResolver.resolver.issue("invalid_join_path", configPath+".on", "join target path must be a valid field path")
	} else {
		join.On = on
	}
	return join
}

func (resolver *resolver) validateCrossCollectionFields(collections []schema.Collection, globals []schema.Global) {
	byID := make(map[schema.StableID]schema.Collection, len(collections))
	for _, collection := range collections {
		byID[collection.ID] = collection
	}
	validate := func(prefix string, collection schema.Collection, joinsAllowed bool) {
		var inspect func([]schema.Field, string)
		inspect = func(fields []schema.Field, fieldsPath string) {
			for fieldIndex, candidate := range fields {
				path := fmt.Sprintf("%s[%d]", fieldsPath, fieldIndex)
				if candidate.Join != nil {
					if !joinsAllowed {
						resolver.issue("unsupported_global_join", path, "join fields are not supported on globals because relationships target collections")
						continue
					}
					target, exists := byID[candidate.Join.CollectionID]
					var targetField *schema.Field
					if exists {
						targetField = directFieldByPath(target.Fields, candidate.Join.On)
					}
					if targetField == nil || targetField.Relationship == nil || targetField.Relationship.HasMany || targetField.Relationship.Polymorphic || targetField.Relationship.CollectionID != collection.ID {
						resolver.issue("invalid_join_target", path+".on", "join target must be a singular relationship back to this collection")
					}
					if exists {
						for columnIndex, column := range candidate.Join.DefaultColumns {
							columnPath, err := query.ParsePath(column)
							if err != nil || (!isJoinSystemColumn(column) && directFieldByPath(target.Fields, columnPath) == nil) {
								resolver.issue("invalid_join_column", fmt.Sprintf("%s.defaultColumns[%d]", path, columnIndex), fmt.Sprintf("join column %q does not exist in collection %q", column, target.Slug))
							}
						}
						if rawSort := strings.TrimPrefix(candidate.Join.DefaultSort, "-"); rawSort != "" {
							sortPath, err := query.ParsePath(rawSort)
							if err != nil || (!isJoinSystemColumn(rawSort) && directFieldByPath(target.Fields, sortPath) == nil) {
								resolver.issue("invalid_join_sort", path+".defaultSort", fmt.Sprintf("join sort %q does not exist in collection %q", candidate.Join.DefaultSort, target.Slug))
							}
						}
					}
				}
				var optionFilters []schema.RelationshipFilter
				var targets []schema.RelationshipTarget
				if candidate.Relationship != nil {
					optionFilters = candidate.Relationship.OptionFilters
					targets = candidate.Relationship.Targets
					if !candidate.Relationship.Polymorphic {
						targets = []schema.RelationshipTarget{{CollectionID: candidate.Relationship.CollectionID, CollectionSlug: candidate.Relationship.CollectionSlug}}
					}
				} else if candidate.Upload != nil {
					optionFilters = candidate.Upload.OptionFilters
					targets = []schema.RelationshipTarget{{CollectionID: candidate.Upload.CollectionID, CollectionSlug: candidate.Upload.CollectionSlug}}
				}
				if len(optionFilters) > 0 {
					for filterIndex, filter := range optionFilters {
						filterPath := fmt.Sprintf("%s.optionFilters[%d]", path, filterIndex)
						var sourceField *schema.Field
						if filter.SourcePath != nil {
							sourceField = fieldByPath(collection.Fields, filter.SourcePath.Segments())
							if sourceField == nil {
								resolver.issue("invalid_relationship_filter_source", filterPath+".sourcePath", "relationship filter source must name a field in this collection")
							}
						} else if filter.Value == nil {
							resolver.issue("invalid_relationship_filter_value", filterPath+".value", "relationship filter requires a source path or literal value")
						}
						matchedTarget := false
						for _, targetReference := range targets {
							if filter.CollectionSlug != "" && filter.CollectionSlug != targetReference.CollectionSlug {
								continue
							}
							matchedTarget = true
							target := byID[targetReference.CollectionID]
							targetField := fieldByPath(target.Fields, filter.TargetPath.Segments())
							targetKind := referenceFilterFieldKind(targetField)
							if filter.TargetPath.String() == "_status" && target.Versions != nil && target.Versions.Drafts {
								targetKind = referenceFilterString
							}
							if targetKind == referenceFilterInvalid {
								resolver.issue("invalid_relationship_filter_target", filterPath+".targetPath", fmt.Sprintf("relationship filter target does not exist in collection %q", target.Slug))
							} else {
								operandKind := referenceFilterFieldKind(sourceField)
								if filter.Value != nil {
									operandKind = referenceFilterLiteralKind(filter.Value)
								}
								if !referenceFilterKindsCompatible(operandKind, targetKind, filter.Operator) {
									resolver.issue("invalid_relationship_filter_type", filterPath, fmt.Sprintf("relationship filter operator %q requires compatible scalar operand and target types", filter.Operator))
								}
							}
						}
						if !matchedTarget {
							resolver.issue("invalid_relationship_filter_collection", filterPath+".collectionSlug", fmt.Sprintf("relationship does not target collection %q", filter.CollectionSlug))
						}
					}
				}
				inspect(schema.EmbeddedBlocks(candidate), path+".plugin.embeddedTrees")
				if candidate.Nested != nil {
					inspect(candidate.Nested.ResolvedFields(), path+".nested.fields")
				}
				if candidate.Blocks != nil {
					for blockIndex, block := range candidate.Blocks.ResolvedTypes() {
						inspect(block.ResolvedFields(), fmt.Sprintf("%s.blocks.types[%d].fields", path, blockIndex))
					}
				}
			}
		}
		inspect(collection.Fields, prefix+".fields")
	}
	for collectionIndex, collection := range collections {
		validate(fmt.Sprintf("collections[%d]", collectionIndex), collection, true)
	}
	for globalIndex, global := range globals {
		validate(fmt.Sprintf("globals[%d]", globalIndex), global, false)
	}
}

func isJoinSystemColumn(path string) bool {
	switch path {
	case "id", "createdAt", "updatedAt", "_status":
		return true
	default:
		return false
	}
}

func directFieldByPath(fields []schema.Field, path query.Path) *schema.Field {
	segments := path.Segments()
	if len(segments) != 1 {
		return nil
	}
	for index := range fields {
		if fields[index].Name == segments[0] && fields[index].Category != schema.FieldCategoryPresentation {
			return &fields[index]
		}
	}
	return nil
}

type referenceFilterValueKind uint8

const (
	referenceFilterInvalid referenceFilterValueKind = iota
	referenceFilterString
	referenceFilterNumber
	referenceFilterBoolean
)

func referenceFilterFieldsCompatible(source, target *schema.Field, operator string) bool {
	return referenceFilterKindsCompatible(referenceFilterFieldKind(source), referenceFilterFieldKind(target), operator)
}

func referenceFilterKindsCompatible(sourceKind, targetKind referenceFilterValueKind, operator string) bool {
	if sourceKind == referenceFilterInvalid || sourceKind != targetKind {
		return false
	}
	switch operator {
	case "equals", "notEquals":
		return true
	case "like", "contains":
		return sourceKind == referenceFilterString
	case "greaterThan", "greaterThanEqual", "lessThan", "lessThanEqual":
		return sourceKind == referenceFilterString || sourceKind == referenceFilterNumber
	default:
		return false
	}
}

func referenceFilterLiteralKind(value *schema.RelationshipFilterValue) referenceFilterValueKind {
	if value == nil {
		return referenceFilterInvalid
	}
	switch value.Type {
	case schema.ValueTypeString:
		return referenceFilterString
	case schema.ValueTypeNumber:
		return referenceFilterNumber
	case schema.ValueTypeBoolean:
		return referenceFilterBoolean
	default:
		return referenceFilterInvalid
	}
}

func resolvedRelationshipFilterValue(value *field.DefaultValue) *schema.RelationshipFilterValue {
	if value == nil {
		return nil
	}
	resolved := &schema.RelationshipFilterValue{Value: value.String()}
	switch value.Kind() {
	case field.DefaultString:
		resolved.Type = schema.ValueTypeString
	case field.DefaultNumber:
		resolved.Type = schema.ValueTypeNumber
	case field.DefaultBoolean:
		resolved.Type = schema.ValueTypeBoolean
	}
	return resolved
}

func referenceFilterFieldKind(candidate *schema.Field) referenceFilterValueKind {
	if candidate == nil || candidate.Category == schema.FieldCategoryPresentation || candidate.Category == schema.FieldCategoryNested || candidate.Category == schema.FieldCategoryPlugin {
		return referenceFilterInvalid
	}
	switch candidate.Type {
	case schema.FieldTypeText, schema.FieldTypeCode, schema.FieldTypeTextarea, schema.FieldTypeEmail,
		schema.FieldTypeDate, schema.FieldTypeRadio:
		return referenceFilterString
	case schema.FieldTypeSelect:
		if candidate.Select != nil && !candidate.Select.HasMany {
			return referenceFilterString
		}
	case schema.FieldTypeNumber:
		return referenceFilterNumber
	case schema.FieldTypeCheckbox:
		return referenceFilterBoolean
	case schema.FieldTypeRelationship:
		if candidate.Relationship != nil && !candidate.Relationship.HasMany && !candidate.Relationship.Polymorphic {
			return referenceFilterString
		}
	case schema.FieldTypeUpload:
		if candidate.Upload != nil && !candidate.Upload.HasMany {
			return referenceFilterString
		}
	}
	return referenceFilterInvalid
}

func (resolver *resolver) issue(code, path, message string) {
	resolver.issues = append(resolver.issues, schema.Issue{Code: code, Path: path, Message: message})
}

func joinConfigPath(parent, child string) string {
	if child == "" {
		return parent
	}
	if strings.HasPrefix(child, "[") {
		return parent + child
	}
	return parent + "." + child
}

func humanize(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	var result []rune
	for index, current := range []rune(value) {
		if current == '-' || current == '_' {
			if len(result) != 0 && result[len(result)-1] != ' ' {
				result = append(result, ' ')
			}
			continue
		}
		if index > 0 && unicode.IsUpper(current) && len(result) != 0 && result[len(result)-1] != ' ' {
			result = append(result, ' ')
		}
		result = append(result, current)
	}
	result[0] = unicode.ToUpper(result[0])
	return string(result)
}

func isReservedFieldName(name string) bool {
	switch name {
	case "id", "createdAt", "updatedAt", "deletedAt":
		return true
	default:
		return false
	}
}

func isReservedWhereFieldName(name string) bool {
	switch name {
	case "and", "or", "not":
		return true
	default:
		return false
	}
}

func (fieldResolver *fieldResolver) resolveBlockTypes(blocks []field.Block, configPath string, pathSegments []string) []schema.BlockType {
	if len(blocks) == 0 {
		fieldResolver.resolver.issue("missing_block_types", configPath+".blocks", "blocks field requires at least one block type")
	}
	resolvedBlocks := make([]schema.BlockType, len(blocks))
	seen := make(map[string]bool, len(blocks))
	for index, block := range blocks {
		blockPath := fmt.Sprintf("%s.blocks[%d]", configPath, index)
		if !schema.IsValidPluginKey(block.Slug) || seen[block.Slug] {
			fieldResolver.resolver.issue("invalid_block_slug", blockPath+".slug", "block slug must be unique lowercase kebab-case")
		}
		seen[block.Slug] = true
		labels := fieldResolver.resolver.resolveBlockLabels(block.Slug, block.Labels, blockPath+".labels")
		blockFields := fieldResolver.resolveFields(block.Fields, blockPath+".fields", append(pathSegments, block.Slug))
		for _, blockField := range blockFields {
			if blockField.Name == "blockType" {
				fieldResolver.resolver.issue("reserved_field_name", blockPath+".fields", "direct block field name \"blockType\" is reserved for the framework block discriminator")
				break
			}
		}
		var admin *schema.BlockAdmin
		if rowLabel := strings.TrimSpace(block.Admin.RowLabelPath); rowLabel != "" {
			found := false
			for _, child := range blockFields {
				if child.Name == rowLabel && child.Category == schema.FieldCategoryScalar && child.Type != schema.FieldTypeJSON && child.Type != schema.FieldTypePoint && child.Type != schema.FieldTypeTextList && child.Type != schema.FieldTypeNumberList && (child.Select == nil || !child.Select.HasMany) {
					found = true
					break
				}
			}
			if !found {
				fieldResolver.resolver.issue("invalid_block_row_label", blockPath+".admin.rowLabelPath", "block row label must name a direct stored scalar child field")
			}
			admin = &schema.BlockAdmin{RowLabel: rowLabel}
		}
		resolvedBlocks[index] = schema.BlockType{
			Admin: admin, TypeName: block.TypeName,
			Slug: block.Slug, Labels: labels,
			Fields: blockFields,
		}
		if block.TypeName != "" {
			shape, err := blocktypes.Shape(resolvedBlocks[index])
			if err == nil {
				if fieldResolver.resolver.blockTypeNames == nil {
					fieldResolver.resolver.blockTypeNames = make(map[string]struct{ shape, path string })
				}
				if prior, exists := fieldResolver.resolver.blockTypeNames[block.TypeName]; exists && prior.shape != shape {
					fieldResolver.resolver.issue("block_type_name_conflict", blockPath+".typeName", fmt.Sprintf("block type name %q has a different resolved definition at %s", block.TypeName, prior.path))
				} else {
					fieldResolver.resolver.blockTypeNames[block.TypeName] = struct{ shape, path string }{shape, blockPath + ".typeName"}
				}
			}
		}

	}

	return resolvedBlocks
}
