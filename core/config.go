package core

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/riducms/ridu/field"
	configresolver "github.com/riducms/ridu/internal/config"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/storage"
	"github.com/riducms/ridu/store"
)

// Config is executable application-owned Ridu configuration. Resolve applies
// compiled plugin transforms to a defensive copy before final validation.
type Config struct {
	// Name is the author-facing application name shown by framework tooling.
	Name string
	// NameTranslations overrides Name for configured admin interface languages.
	NameTranslations map[string]string
	// Admin configures the framework-owned administration interface.
	Admin AdminConfig
	// Localization configures content locales. The zero value disables content
	// localization without affecting admin interface language.
	Localization LocalizationConfig
	// AllowIDOnCreate lets ordinary create operations accept a caller-supplied
	// canonical string ID. It is disabled by default; migration imports retain
	// their separate identity-preserving path regardless of this setting.
	AllowIDOnCreate bool
	// Blocks declares shared immutable definitions selected by field and embedded references.
	Blocks []field.Block
	// Collections declares every document collection owned by the application.
	Collections []Collection
	// Globals declares singleton documents with their own API and admin routes.
	Globals []Global
	// Endpoints declares application-level custom HTTP endpoints below /api.
	// Handlers are anonymous by default and must enforce their own policy.
	Endpoints []Endpoint
	// Tasks registers compiled durable task handlers. Executable handlers are
	// runtime-only and are intentionally excluded from the schema manifest.
	Tasks []TaskDefinition
	// Hooks observes application-wide lifecycle failures.
	Hooks RootHooks
	// Plugins lists compiled extensions in deterministic execution order.
	Plugins []Plugin
	// AfterCommit dispatches effects only after a successful transaction commits.
	AfterCommit AfterCommitDispatcher
	// Storage provides the application-wide object-storage adapter when App is
	// constructed directly with New. Applications run through Execute should
	// prefer WithUploadStorage so external clients are opened only at runtime.
	// Storage is never serialized into the schema manifest.
	Storage storage.Backend
	// StorageNamespace is the stable, deployment-owned prefix for upload objects.
	// It must remain unchanged when the display Name changes and must be unique
	// among applications sharing one backend.
	StorageNamespace string
	pluginEndpoints  []runtimePluginEndpoint
	fieldGraph       configresolver.Graph
}

// LocalizationConfig declares the application's content locales. Fallback is
// enabled by default; set DisableFallback to require exact locale values.
type LocalizationConfig struct {
	Locales         []Locale
	DefaultLocale   schema.LocaleCode
	DisableFallback bool
	// AvailableLocales may reduce the locale list exposed to an admin request.
	// It does not weaken API validation or authorization and is never serialized.
	AvailableLocales LocaleAvailability
}

// LocaleAvailability dynamically limits content locales shown to one author.
type LocaleAvailability func(LocaleAvailabilityContext) ([]schema.LocaleCode, error)

// LocaleAvailabilityContext is the request-scoped input to AvailableLocales.
type LocaleAvailabilityContext struct {
	Context         context.Context
	Actor           *store.Document
	ActorCollection schema.CollectionSlug
	Local           *LocalAPI
}

// Locale declares one content locale and its ordered fallback chain.
type Locale struct {
	Code            schema.LocaleCode
	Label           string
	RTL             bool
	FallbackLocales []schema.LocaleCode
}

// AdminConfig controls application-level behavior of the framework-owned admin.
type AdminConfig struct {
	// User is the slug of the auth-enabled collection whose sessions may access
	// the admin. It is required when the application has any auth collection.
	User schema.CollectionSlug
	// Localization configures the language and timezone choices for the
	// framework-owned interface. It is independent from content localization.
	Localization AdminLocalizationConfig
}

// AdminLocalizationConfig declares interface languages and editor timezones.
// Translation catalogs remain statically imported TypeScript modules and are
// never serialized into the schema manifest.
type AdminLocalizationConfig struct {
	Languages       []AdminLanguage
	DefaultLanguage string
	TimeZones       []AdminTimeZone
	DefaultTimeZone string
}

// AdminLanguage is one translated admin interface available to editors.
type AdminLanguage struct {
	Code              string
	Label             string
	LabelTranslations map[string]string
	RTL               bool
}

// AdminTimeZone is one IANA timezone offered by the admin interface.
type AdminTimeZone struct {
	ID                string
	Label             string
	LabelTranslations map[string]string
}

// Collection defines one document collection in authoring configuration.
type Collection struct {
	// Slug is the URL-safe collection name used by APIs, relationships, and the admin.
	Slug schema.CollectionSlug
	// Labels override the singular and plural names shown to authors.
	Labels CollectionLabels
	// Admin configures collection presentation and editorial organization.
	Admin CollectionAdmin
	// Fields owns the collection's immutable field shape, behavior, and presentation.
	Fields field.Fields
	// Indexes defines ordered multi-field indexes. Unique indexes enforce tuple
	// uniqueness while allowing multiple rows containing null.
	Indexes []CollectionIndex
	// Auth enables identities, passwords, and sessions for this collection.
	Auth bool
	// AuthConfig customizes behavior that applies when Auth is enabled.
	AuthConfig AuthConfig
	// Upload enables file metadata and storage behavior for this collection.
	Upload bool
	// UploadConfig customizes validation, privacy, and image variants for uploads.
	UploadConfig UploadConfig
	// Versions enables document revisions for this collection.
	Versions bool
	// Trash keeps deleted documents recoverable until they are permanently deleted.
	Trash bool
	// LockDocuments coordinates exclusive document editing with timeout and takeover.
	LockDocuments bool
	// DocumentLockConfig customizes lock expiry when LockDocuments is enabled.
	DocumentLockConfig DocumentLockConfig
	// VersionConfig customizes drafts, retention, and autosave behavior.
	VersionConfig VersionConfig
	// Access defines collection-level authorization for each operation.
	Access CollectionAccess
	// Hooks defines collection-wide lifecycle behavior.
	Hooks CollectionHooks
	// Endpoints declares custom HTTP endpoints below
	// /api/collections/<slug>. They run before matching built-in collection
	// routes for the same method.
	Endpoints []Endpoint
}

// CollectionIndex defines one ordered compound database index. Fields are
// dot-separated paths through non-repeated groups.
type CollectionIndex struct {
	Fields []string
	Unique bool
}

// DocumentLockConfig controls persisted authoring locks.
type DocumentLockConfig struct {
	Duration time.Duration
}

// CollectionAdmin is serializable presentation metadata consumed by the framework admin.
type CollectionAdmin struct {
	UseAsTitle              string
	DefaultColumns          []string
	Group                   string
	GroupTranslations       map[string]string
	Description             string
	DescriptionTranslations map[string]string
	FolderField             string
	ParentField             string
	LivePreview             LivePreviewConfig
}

// LivePreviewConfig exposes a same- or cross-origin frontend inside the document editor.
// URL is a serializable template and may contain {id}, {collection}, and
// {field:path.to.value} placeholders that the admin resolves from the current draft.
type LivePreviewConfig struct {
	URL         string
	Breakpoints []PreviewBreakpoint
}

// PreviewBreakpoint is one named authoring viewport offered by live preview.
type PreviewBreakpoint struct {
	Name              string
	Label             string
	LabelTranslations map[string]string
	Width             int
	Height            int
}

// Global defines one singleton document in authoring configuration.
type Global struct {
	// Slug is the URL-safe global name used by APIs and the admin.
	Slug schema.CollectionSlug
	// Label overrides the author-facing name shown in navigation and headings.
	Label string
	// LabelTranslations overrides Label for configured admin interface languages.
	LabelTranslations map[string]string
	// Admin is serializable presentation metadata consumed by the framework admin.
	Admin GlobalAdmin
	// Fields owns the global's immutable field shape, behavior, and presentation.
	Fields field.Fields
	// Versions enables revisions for this global.
	Versions bool
	// VersionConfig customizes drafts, retention, and autosave behavior.
	VersionConfig VersionConfig
	// Access defines read and update authorization.
	Access GlobalAccess
	// Hooks defines global-wide lifecycle behavior.
	Hooks CollectionHooks
	// Endpoints declares custom HTTP endpoints below /api/globals/<slug>.
	// They run before matching built-in global routes for the same method.
	Endpoints []Endpoint
}

// GlobalAdmin customizes global presentation without affecting authorization.
type GlobalAdmin struct {
	Group                   string
	GroupTranslations       map[string]string
	Description             string
	DescriptionTranslations map[string]string
	LivePreview             LivePreviewConfig
}

// AuthConfig controls session behavior for an auth-enabled collection.
type AuthConfig struct {
	// SessionDuration is how long a login remains valid. Zero defaults to 24 hours.
	SessionDuration time.Duration
	// Password controls local password validation and hashing. Zero values use
	// the secure framework defaults documented on PasswordPolicy.
	Password PasswordPolicy
	// MaxLoginAttempts is the number of consecutive credential failures allowed
	// before the account is temporarily locked. Zero defaults to 5; a negative
	// value disables account lockout.
	MaxLoginAttempts int
	// LockDuration is how long an account remains locked after MaxLoginAttempts.
	// Zero defaults to 10 minutes.
	LockDuration time.Duration
	// PasswordReset enables the forgot/reset-password flow when Send is set.
	// The raw single-use token is delivered only to this trusted callback.
	PasswordReset PasswordResetConfig
	// Verify enables email verification. When non-nil, newly provisioned
	// credentials cannot log in until a verification token is consumed.
	Verify *VerifyEmailConfig
	// APIKeys allows users to mint revocable, session-independent bearer
	// credentials. API keys are disabled by default.
	APIKeys bool
	// Access contains authorization rules for auth operations that are not CRUD.
	Access AuthAccess
	// Hooks contains authentication lifecycle callbacks.
	Hooks AuthHooks
	// Strategies adds application-owned request authentication in declaration order.
	Strategies []AuthStrategy
}

// PasswordResetConfig configures password-recovery delivery.
type PasswordResetConfig struct {
	// TokenDuration is the lifetime of a reset token. Zero defaults to one hour.
	TokenDuration time.Duration
	// Send delivers a newly issued token. It should enqueue or send the message
	// before returning; returning an error prevents a success response.
	Send func(context.Context, PasswordResetNotification) error
}

// PasswordResetNotification contains the secret needed to construct an
// application-owned reset link. Token is shown once and must never be logged.
type PasswordResetNotification struct {
	Collection schema.CollectionSlug
	User       store.Document
	Token      string
	ExpiresAt  time.Time
}

// VerifyEmailConfig configures mandatory identity verification.
type VerifyEmailConfig struct {
	// TokenDuration is the lifetime of a verification token. Zero defaults to 24 hours.
	TokenDuration time.Duration
	// Send delivers a newly issued verification token.
	Send func(context.Context, VerifyEmailNotification) error
}

// VerifyEmailNotification contains the secret needed to construct an
// application-owned verification link. Token is shown once and must not be logged.
type VerifyEmailNotification struct {
	Collection schema.CollectionSlug
	User       store.Document
	Token      string
	ExpiresAt  time.Time
}

// PasswordPolicy controls the built-in local password strategy.
//
// Ridu deliberately defaults to length-based validation instead of mandatory
// character classes. Applications needing breached-password checks or other
// product-specific rules can provide Validate; it executes only in the trusted
// Go runtime and is never serialized into the schema manifest.
type PasswordPolicy struct {
	// MinLength is the minimum password length in Unicode code points. Zero
	// defaults to 8.
	MinLength int
	// MaxBytes is the maximum UTF-8 encoded password length. Zero defaults to 72,
	// bcrypt's safe input limit.
	MaxBytes int
	// BcryptCost controls bcrypt work. Zero uses bcrypt.DefaultCost. Increasing
	// it transparently upgrades older hashes after a successful login. Values
	// above 16 are rejected because they can make startup and login impractical.
	BcryptCost int
	// Validate adds application-specific password rules. Return a user-safe error
	// explaining how the candidate must change.
	Validate func(password string) error
}

// UploadConfig controls validation and storage behavior for an upload collection.
type UploadConfig struct {
	// MaxFileSize is the maximum accepted upload size in bytes. Zero uses the
	// framework default; values above 256 MiB are rejected so one request cannot
	// bypass the process-wide upload work budget.
	MaxFileSize int64
	// MimeTypes restricts uploads to the listed media types. Empty accepts any supported type.
	MimeTypes []string
	// Private requires authorized delivery instead of exposing storage objects publicly.
	Private bool
	// ImageSizes declares derived image variants generated from supported image uploads.
	ImageSizes []ImageSize
}

// ImageSize describes one derived image variant for an upload collection.
type ImageSize struct {
	// Name is the stable key used to address the generated variant.
	Name string
	// Width is the target width in pixels.
	Width int
	// Height is the target height in pixels.
	Height int
	// Fit controls resizing and must be either "cover" or "contain".
	Fit string
}

// VersionConfig controls revision and draft behavior for a versioned collection.
type VersionConfig struct {
	// Drafts allows unpublished document states.
	Drafts bool
	// MaxPerDocument limits retained revisions per document. Zero uses the framework default.
	MaxPerDocument int
	// AutosaveInterval controls draft autosave frequency. Zero disables autosave.
	AutosaveInterval time.Duration
}

// CollectionLabels overrides author-facing collection names.
type CollectionLabels struct {
	// Singular is used when referring to one document.
	Singular string
	// SingularTranslations overrides Singular for configured admin languages.
	SingularTranslations map[string]string
	// Plural is used in navigation and collection lists.
	Plural string
	// PluralTranslations overrides Plural for configured admin languages.
	PluralTranslations map[string]string
}

// Resolve applies config transforms in declared plugin order, validates and
// normalizes the result, and returns an immutable canonical manifest.
func Resolve(applicationConfig Config) (schema.Manifest, error) {
	_, manifest, err := resolveConfig(applicationConfig)
	return manifest, err
}

func resolveConfig(applicationConfig Config) (Config, schema.Manifest, error) {
	working := cloneConfig(applicationConfig)
	plugins := append([]Plugin(nil), working.Plugins...)
	if err := validateGraphPluginKeys(plugins); err != nil {
		return Config{}, schema.Manifest{}, err
	}
	if err := validatePluginCompatibility(plugins); err != nil {
		return Config{}, schema.Manifest{}, err
	}

	for index, plugin := range plugins {
		if isNilPlugin(plugin) {
			return Config{}, schema.Manifest{}, schema.NewValidationError([]schema.Issue{{
				Code:    "nil_plugin",
				Path:    fmt.Sprintf("plugins[%d]", index),
				Message: "plugin must not be nil",
			}})
		}

		transformer, transformsConfig := plugin.(ConfigTransformer)
		if !transformsConfig {
			continue
		}

		transformed, err := transformer.TransformConfig(cloneConfig(working))
		if err != nil {
			return Config{}, schema.Manifest{}, schema.NewValidationError([]schema.Issue{{
				Code:    "plugin_transform_failed",
				Path:    fmt.Sprintf("plugins[%d]", index),
				Message: fmt.Sprintf("plugin %q config transform failed: %v", plugin.Key(), err),
			}})
		}
		working = cloneConfig(transformed)
		working.Plugins = plugins
	}
	if err := bindConfigBlocks(&working); err != nil {
		return Config{}, schema.Manifest{}, err
	}
	if err := composeFieldGraphs(&working, plugins); err != nil {
		return Config{}, schema.Manifest{}, err
	}
	resolvedPlugins, pluginEndpoints, err := resolverPlugins(plugins)
	if err != nil {
		return Config{}, schema.Manifest{}, err
	}
	working.pluginEndpoints = pluginEndpoints
	if err := validateEndpointRuntime(working); err != nil {
		return Config{}, schema.Manifest{}, err
	}
	if err := validateAuthRuntime(working.Collections); err != nil {
		return Config{}, schema.Manifest{}, err
	}
	if _, issues := buildTaskRegistry(working.Tasks); len(issues) != 0 {
		return Config{}, schema.Manifest{}, schema.NewValidationError(issues)
	}
	manifest, graph, err := configresolver.ResolveGraph(configresolver.Input{
		Blocks:           working.Blocks,
		Name:             working.Name,
		NameTranslations: cloneStringMap(working.NameTranslations),
		AllowIDOnCreate:  working.AllowIDOnCreate,
		Admin:            resolverAdmin(working.Admin),
		Localization:     resolverLocalization(working.Localization),
		Collections:      resolverCollections(working.Collections),
		Globals:          resolverGlobals(working.Globals),
		Endpoints:        resolverEndpoints(working.Endpoints),
		Plugins:          resolvedPlugins,
	})
	if err != nil {
		return Config{}, schema.Manifest{}, err
	}
	working.fieldGraph = graph
	if err := validatePluginValidatorOwnership(plugins, manifest); err != nil {
		return Config{}, schema.Manifest{}, err
	}
	if err := applyPluginHooks(&working, plugins); err != nil {
		return Config{}, schema.Manifest{}, err
	}
	if err := validateResourceHooks(working); err != nil {
		return Config{}, schema.Manifest{}, err
	}
	return working, manifest, nil
}

// Field validators belong to declared field types, not whichever plugin happens to run last.
func validatePluginValidatorOwnership(plugins []Plugin, manifest schema.Manifest) error {
	owners := make(map[string]string)
	for _, plugin := range manifest.Snapshot().Plugins {
		if plugin.Version == "" {
			owners[plugin.Key] = plugin.Key
		}
		for _, fieldType := range plugin.FieldTypes {
			owners[fieldType.Key] = plugin.Key
		}
	}
	for index, plugin := range plugins {
		provider, ok := plugin.(FieldValidatorProvider)
		if !ok {
			continue
		}
		validators := provider.FieldValidators()
		keys := make([]string, 0, len(validators))
		for key := range validators {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			validator := validators[key]
			if owners[key] != plugin.Key() || validator == nil {
				return schema.NewValidationError([]schema.Issue{{Code: "invalid_plugin_field_validator", Path: fmt.Sprintf("plugins[%d].fieldValidators.%s", index, key), Message: fmt.Sprintf("plugin %q must supply non-nil validators only for its declared field types", plugin.Key())}})
			}
		}
	}
	return nil
}

func resolverAdmin(admin AdminConfig) configresolver.Admin {
	languages := make([]configresolver.AdminLanguage, len(admin.Localization.Languages))
	for index, language := range admin.Localization.Languages {
		languages[index] = configresolver.AdminLanguage{
			Code: language.Code, Label: language.Label,
			LabelTranslations: cloneStringMap(language.LabelTranslations), RTL: language.RTL,
		}
	}
	timeZones := make([]configresolver.AdminTimeZone, len(admin.Localization.TimeZones))
	for index, timeZone := range admin.Localization.TimeZones {
		timeZones[index] = configresolver.AdminTimeZone{ID: timeZone.ID, Label: timeZone.Label, LabelTranslations: cloneStringMap(timeZone.LabelTranslations)}
	}
	return configresolver.Admin{
		User: admin.User,
		Localization: configresolver.AdminLocalization{
			Languages: languages, DefaultLanguage: admin.Localization.DefaultLanguage,
			TimeZones: timeZones, DefaultTimeZone: admin.Localization.DefaultTimeZone,
		},
	}
}

func resolverLocalization(localization LocalizationConfig) *configresolver.Localization {
	if len(localization.Locales) == 0 && localization.DefaultLocale == "" && !localization.DisableFallback {
		return nil
	}
	locales := make([]configresolver.Locale, len(localization.Locales))
	for index, locale := range localization.Locales {
		locales[index] = configresolver.Locale{
			Code: locale.Code, Label: locale.Label, RTL: locale.RTL,
			FallbackLocales: append([]schema.LocaleCode(nil), locale.FallbackLocales...),
		}
	}
	return &configresolver.Localization{Locales: locales, DefaultLocale: localization.DefaultLocale, DisableFallback: localization.DisableFallback}
}

func validateAuthRuntime(collections []Collection) error {
	var issues []schema.Issue
	for index, collection := range collections {
		if !collection.Auth {
			continue
		}
		path := fmt.Sprintf("collections[%d].auth", index)
		if collection.AuthConfig.Verify != nil && collection.AuthConfig.Verify.Send == nil {
			issues = append(issues, schema.Issue{Code: "missing_verification_sender", Path: path + ".verify.send", Message: "email verification requires a Send callback"})
		}
		if collection.AuthConfig.PasswordReset.TokenDuration != 0 && collection.AuthConfig.PasswordReset.Send == nil {
			issues = append(issues, schema.Issue{Code: "missing_password_reset_sender", Path: path + ".passwordReset.send", Message: "a password reset token duration requires a Send callback"})
		}
		seenStrategies := make(map[string]struct{}, len(collection.AuthConfig.Strategies))
		for strategyIndex, strategy := range collection.AuthConfig.Strategies {
			strategyPath := fmt.Sprintf("%s.strategies[%d]", path, strategyIndex)
			if !schema.IsValidCollectionSlug(strategy.Name) {
				issues = append(issues, schema.Issue{Code: "invalid_auth_strategy_name", Path: strategyPath + ".name", Message: "auth strategy name must be lowercase kebab-case"})
			}
			if _, exists := seenStrategies[strategy.Name]; exists {
				issues = append(issues, schema.Issue{Code: "duplicate_auth_strategy", Path: strategyPath + ".name", Message: fmt.Sprintf("auth strategy %q is already declared", strategy.Name)})
			}
			seenStrategies[strategy.Name] = struct{}{}
			if strategy.Authenticate == nil {
				issues = append(issues, schema.Issue{Code: "missing_auth_strategy_handler", Path: strategyPath + ".authenticate", Message: "auth strategy requires an Authenticate callback"})
			}
		}
	}
	if len(issues) != 0 {
		return schema.NewValidationError(issues)
	}
	return nil
}

func validatePluginCompatibility(plugins []Plugin) error {
	var issues []schema.Issue
	for index, plugin := range plugins {
		if isNilPlugin(plugin) {
			continue
		}
		provider, ok := plugin.(DescriptorProvider)
		if !ok {
			_, endpoints := plugin.(EndpointProvider)
			_, hooks := plugin.(HookProvider)
			_, transports := plugin.(TransportProvider)
			_, generation := plugin.(GenerationProvider)
			if endpoints || hooks || transports || generation {
				issues = append(issues, schema.Issue{Code: "missing_plugin_descriptor", Path: fmt.Sprintf("plugins[%d]", index), Message: fmt.Sprintf("plugin %q must implement DescriptorProvider before exposing advanced runtime or generation capabilities", plugin.Key())})
			}
			continue
		}
		descriptor := provider.Descriptor()
		if !schema.SemanticVersionInRange(FrameworkVersion, descriptor.Ridu.Minimum, descriptor.Ridu.MaximumExclusive) {
			issues = append(issues, schema.Issue{Code: "incompatible_ridu_plugin", Path: fmt.Sprintf("plugins[%d].ridu", index), Message: fmt.Sprintf("plugin %q %s does not support Ridu %s", plugin.Key(), descriptor.Version, FrameworkVersion)})
		}
	}
	if len(issues) != 0 {
		return schema.NewValidationError(issues)
	}
	return nil
}

func applyPluginHooks(config *Config, plugins []Plugin) error {
	collections := make(map[schema.CollectionSlug]int, len(config.Collections))
	for index, collection := range config.Collections {
		collections[collection.Slug] = index
	}
	var issues []schema.Issue
	for pluginIndex, plugin := range plugins {
		provider, ok := plugin.(HookProvider)
		if !ok {
			continue
		}
		for hookIndex, contribution := range provider.Hooks() {
			path := fmt.Sprintf("plugins[%d].hooks[%d]", pluginIndex, hookIndex)
			collectionIndex, exists := collections[contribution.Collection]
			if !exists {
				issues = append(issues, schema.Issue{Code: "unknown_plugin_hook_collection", Path: path + ".collection", Message: fmt.Sprintf("plugin hook collection %q does not exist", contribution.Collection)})
				continue
			}
			config.Collections[collectionIndex].Hooks = appendCollectionHooks(config.Collections[collectionIndex].Hooks, contribution.Hooks)

		}
	}
	if len(issues) != 0 {
		return schema.NewValidationError(issues)
	}
	return nil
}

func appendCollectionHooks(existing, contribution CollectionHooks) CollectionHooks {
	existing.BeforeDuplicate = append(existing.BeforeDuplicate, contribution.BeforeDuplicate...)
	existing.BeforeValidate = append(existing.BeforeValidate, contribution.BeforeValidate...)
	existing.BeforeChange = append(existing.BeforeChange, contribution.BeforeChange...)
	existing.BeforeOperation = append(existing.BeforeOperation, contribution.BeforeOperation...)
	existing.BeforeRead = append(existing.BeforeRead, contribution.BeforeRead...)
	existing.BeforeDelete = append(existing.BeforeDelete, contribution.BeforeDelete...)
	existing.AfterChange = append(existing.AfterChange, contribution.AfterChange...)
	existing.AfterRead = append(existing.AfterRead, contribution.AfterRead...)
	existing.AfterDelete = append(existing.AfterDelete, contribution.AfterDelete...)
	existing.AfterOperation = append(existing.AfterOperation, contribution.AfterOperation...)
	existing.AfterError = append(existing.AfterError, contribution.AfterError...)
	existing.AfterCommit = append(existing.AfterCommit, contribution.AfterCommit...)
	return existing
}

func isNilPlugin(plugin Plugin) bool {
	if plugin == nil {
		return true
	}
	value := reflect.ValueOf(plugin)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func resolverCollections(collections []Collection) []configresolver.Collection {
	resolved := make([]configresolver.Collection, len(collections))
	for index, collection := range collections {
		resolved[index] = configresolver.Collection{
			Slug: collection.Slug,
			Labels: schema.CollectionLabels{
				Singular:             collection.Labels.Singular,
				SingularTranslations: cloneStringMap(collection.Labels.SingularTranslations),
				Plural:               collection.Labels.Plural,
				PluralTranslations:   cloneStringMap(collection.Labels.PluralTranslations),
			},
			Admin: configresolver.CollectionAdmin{
				UseAsTitle: collection.Admin.UseAsTitle, DefaultColumns: append([]string(nil), collection.Admin.DefaultColumns...),
				Group: collection.Admin.Group, GroupTranslations: cloneStringMap(collection.Admin.GroupTranslations),
				Description: collection.Admin.Description, DescriptionTranslations: cloneStringMap(collection.Admin.DescriptionTranslations),
				FolderField: collection.Admin.FolderField, ParentField: collection.Admin.ParentField,
				LivePreview: resolverLivePreview(collection.Admin.LivePreview),
			},
			Fields:             append(field.Fields(nil), collection.Fields...),
			Indexes:            resolverIndexes(collection.Indexes),
			Auth:               collection.Auth,
			SessionDuration:    collection.AuthConfig.SessionDuration,
			PasswordMinLength:  collection.AuthConfig.Password.MinLength,
			PasswordMaxBytes:   collection.AuthConfig.Password.MaxBytes,
			PasswordBcryptCost: collection.AuthConfig.Password.BcryptCost,
			MaxLoginAttempts:   collection.AuthConfig.MaxLoginAttempts,
			LockDuration:       collection.AuthConfig.LockDuration,
			PasswordReset:      collection.AuthConfig.PasswordReset.Send != nil,
			PasswordResetTTL:   collection.AuthConfig.PasswordReset.TokenDuration,
			VerifyEmail:        collection.AuthConfig.Verify != nil,
			VerificationTTL: func() time.Duration {
				if collection.AuthConfig.Verify == nil {
					return 0
				}
				return collection.AuthConfig.Verify.TokenDuration
			}(),
			APIKeys:              collection.AuthConfig.APIKeys,
			Upload:               collection.Upload,
			UploadConfig:         configresolver.UploadConfig{MaxFileSize: collection.UploadConfig.MaxFileSize, MimeTypes: append([]string(nil), collection.UploadConfig.MimeTypes...), Private: collection.UploadConfig.Private, ImageSizes: resolverImageSizes(collection.UploadConfig.ImageSizes)},
			Versions:             collection.Versions,
			Trash:                collection.Trash,
			LockDocuments:        collection.LockDocuments,
			DocumentLockDuration: collection.DocumentLockConfig.Duration,
			VersionConfig:        configresolver.VersionConfig{Drafts: collection.VersionConfig.Drafts, MaxPerDocument: collection.VersionConfig.MaxPerDocument, AutosaveInterval: collection.VersionConfig.AutosaveInterval},
			Endpoints:            resolverEndpoints(collection.Endpoints),
		}
	}
	return resolved
}

func resolverIndexes(indexes []CollectionIndex) []configresolver.CollectionIndex {
	resolved := make([]configresolver.CollectionIndex, len(indexes))
	for index, candidate := range indexes {
		resolved[index] = configresolver.CollectionIndex{Fields: append([]string(nil), candidate.Fields...), Unique: candidate.Unique}
	}
	return resolved
}

func resolverGlobals(globals []Global) []configresolver.Global {
	resolved := make([]configresolver.Global, len(globals))
	for index, global := range globals {
		resolved[index] = configresolver.Global{
			Slug: global.Slug, Label: global.Label, LabelTranslations: cloneStringMap(global.LabelTranslations),
			Admin: configresolver.GlobalAdmin{
				Group: global.Admin.Group, GroupTranslations: cloneStringMap(global.Admin.GroupTranslations),
				Description: global.Admin.Description, DescriptionTranslations: cloneStringMap(global.Admin.DescriptionTranslations),
				LivePreview: resolverLivePreview(global.Admin.LivePreview),
			},
			Fields:   append(field.Fields(nil), global.Fields...),
			Versions: global.Versions,
			VersionConfig: configresolver.VersionConfig{
				Drafts: global.VersionConfig.Drafts, MaxPerDocument: global.VersionConfig.MaxPerDocument,
				AutosaveInterval: global.VersionConfig.AutosaveInterval,
			},
			Endpoints: resolverEndpoints(global.Endpoints),
		}
	}
	return resolved
}

func resolverEndpoints(endpoints []Endpoint) []configresolver.Endpoint {
	resolved := make([]configresolver.Endpoint, len(endpoints))
	for index, endpoint := range endpoints {
		resolved[index] = configresolver.Endpoint{Method: endpoint.Method, Path: endpoint.Path, Summary: endpoint.Summary}
	}
	return resolved
}

func validateEndpointRuntime(config Config) error {
	var issues []schema.Issue
	inspect := func(endpoints []Endpoint, path string) {
		for index, endpoint := range endpoints {
			if endpoint.Handler == nil {
				issues = append(issues, schema.Issue{
					Code: "missing_endpoint_handler", Path: fmt.Sprintf("%s[%d].handler", path, index),
					Message: "custom endpoint handler must not be nil",
				})
			}
		}
	}
	inspect(config.Endpoints, "endpoints")
	for index, collection := range config.Collections {
		inspect(collection.Endpoints, fmt.Sprintf("collections[%d].endpoints", index))
	}
	for index, global := range config.Globals {
		inspect(global.Endpoints, fmt.Sprintf("globals[%d].endpoints", index))
	}
	if len(issues) != 0 {
		return schema.NewValidationError(issues)
	}
	return nil
}

func resolverLivePreview(preview LivePreviewConfig) configresolver.LivePreviewConfig {
	breakpoints := make([]configresolver.PreviewBreakpoint, len(preview.Breakpoints))
	for index, breakpoint := range preview.Breakpoints {
		breakpoints[index] = configresolver.PreviewBreakpoint{
			Name: breakpoint.Name, Label: breakpoint.Label, LabelTranslations: cloneStringMap(breakpoint.LabelTranslations), Width: breakpoint.Width, Height: breakpoint.Height,
		}
	}
	return configresolver.LivePreviewConfig{URL: preview.URL, Breakpoints: breakpoints}
}

func resolverImageSizes(sizes []ImageSize) []configresolver.ImageSize {
	resolved := make([]configresolver.ImageSize, len(sizes))
	for index, size := range sizes {
		resolved[index] = configresolver.ImageSize{Name: size.Name, Width: size.Width, Height: size.Height, Fit: size.Fit}
	}
	return resolved
}

type runtimePluginEndpoint struct {
	pluginKey string
	endpoint  PluginEndpoint
}

type runtimePluginTransport struct {
	pluginKey string
	transport PluginTransport
}

func resolverPlugins(plugins []Plugin) ([]configresolver.Plugin, []runtimePluginEndpoint, error) {
	resolved := make([]configresolver.Plugin, len(plugins))
	var runtimeEndpoints []runtimePluginEndpoint
	var issues []schema.Issue
	for index, plugin := range plugins {
		if plugin != nil {
			resolved[index] = configresolver.Plugin{Key: plugin.Key()}
			if provider, ok := plugin.(DescriptorProvider); ok {
				descriptor := provider.Descriptor()
				resolved[index] = configresolver.Plugin{
					Key: plugin.Key(), Version: descriptor.Version, GoPackage: descriptor.GoPackage,
					APIVersion:            descriptor.APIVersion,
					FieldTypes:            resolverPluginFieldTypes(descriptor.FieldTypes),
					DatabaseContributions: resolverPluginDatabaseContributions(descriptor.DatabaseContributions),
				}
				if descriptor.Ridu.Minimum != "" || descriptor.Ridu.MaximumExclusive != "" {
					resolved[index].Ridu = &configresolver.PluginCompatibility{Minimum: descriptor.Ridu.Minimum, MaximumExclusive: descriptor.Ridu.MaximumExclusive}
				}
				if descriptor.Admin != nil {
					resolved[index].Admin = resolverAdminPlugin(*descriptor.Admin)
				}
			}
			if provider, ok := plugin.(EndpointProvider); ok {
				for endpointIndex, endpoint := range provider.Endpoints() {
					if endpoint.Handler == nil {
						issues = append(issues, schema.Issue{Code: "missing_plugin_endpoint_handler", Path: fmt.Sprintf("plugins[%d].endpoints[%d].handler", index, endpointIndex), Message: fmt.Sprintf("plugin %q endpoint handler must not be nil", plugin.Key())})
					}
					resolved[index].Endpoints = append(resolved[index].Endpoints, configresolver.PluginEndpoint{Method: endpoint.Method, Path: endpoint.Path, Summary: endpoint.Summary})
					runtimeEndpoints = append(runtimeEndpoints, runtimePluginEndpoint{pluginKey: plugin.Key(), endpoint: endpoint})
				}
			}
		}
	}
	if len(issues) != 0 {
		return nil, nil, schema.NewValidationError(issues)
	}
	return resolved, runtimeEndpoints, nil
}

func resolverAdminPlugin(metadata AdminPluginMetadata) *configresolver.PluginAdmin {
	return &configresolver.PluginAdmin{
		Package: metadata.Package, Export: metadata.Export, APIVersion: metadata.APIVersion,
		PairingVersion: metadata.PairingVersion, Routes: append([]string(nil), metadata.Routes...), Assets: append([]string(nil), metadata.Assets...),
	}
}

func resolverPluginFieldTypes(types []PluginFieldType) []configresolver.PluginFieldType {
	result := make([]configresolver.PluginFieldType, len(types))
	for index, fieldType := range types {
		result[index] = configresolver.PluginFieldType{
			Key: fieldType.Key, TypeScriptPackage: fieldType.TypeScriptPackage,
			TypeScriptOutput: fieldType.TypeScriptOutput, TypeScriptInput: fieldType.TypeScriptInput,
			TypeScriptWhere: fieldType.TypeScriptWhere, GoPackage: fieldType.GoPackage,
			GoType: fieldType.GoType, JSONSchema: append([]byte(nil), fieldType.JSONSchema...), EmbeddedTypes: append([]string(nil), fieldType.EmbeddedTypes...),
		}
	}
	return result
}

func resolverPluginMigrations(migrations []PluginMigration) []configresolver.PluginMigration {
	result := make([]configresolver.PluginMigration, len(migrations))
	for index, pluginMigration := range migrations {
		result[index] = configresolver.PluginMigration{Version: pluginMigration.Version, Name: pluginMigration.Name, UpSQL: append([]string(nil), pluginMigration.UpSQL...), DownSQL: append([]string(nil), pluginMigration.DownSQL...)}
	}
	return result
}

func resolverPluginDatabaseContributions(contributions []PluginDatabaseContribution) []configresolver.PluginDatabaseContribution {
	result := make([]configresolver.PluginDatabaseContribution, len(contributions))
	for index, contribution := range contributions {
		result[index] = configresolver.PluginDatabaseContribution{
			Adapter:    contribution.Adapter,
			Migrations: resolverPluginMigrations(contribution.Migrations),
			Tables:     append([]string(nil), contribution.Tables...),
		}
	}
	return result
}

func cloneConfig(applicationConfig Config) Config {
	cloned := applicationConfig
	cloned.Blocks = make([]field.Block, len(applicationConfig.Blocks))
	for i, b := range applicationConfig.Blocks {
		cloned.Blocks[i] = b.Snapshot()
	}
	cloned.NameTranslations = cloneStringMap(applicationConfig.NameTranslations)
	cloned.Admin.Localization.Languages = append([]AdminLanguage(nil), applicationConfig.Admin.Localization.Languages...)
	for index := range cloned.Admin.Localization.Languages {
		cloned.Admin.Localization.Languages[index].LabelTranslations = cloneStringMap(applicationConfig.Admin.Localization.Languages[index].LabelTranslations)
	}
	cloned.Admin.Localization.TimeZones = append([]AdminTimeZone(nil), applicationConfig.Admin.Localization.TimeZones...)
	for index := range cloned.Admin.Localization.TimeZones {
		cloned.Admin.Localization.TimeZones[index].LabelTranslations = cloneStringMap(applicationConfig.Admin.Localization.TimeZones[index].LabelTranslations)
	}
	cloned.Localization.Locales = make([]Locale, len(applicationConfig.Localization.Locales))
	for index, locale := range applicationConfig.Localization.Locales {
		cloned.Localization.Locales[index] = locale
		cloned.Localization.Locales[index].FallbackLocales = append([]schema.LocaleCode(nil), locale.FallbackLocales...)
	}
	cloned.Hooks = applicationConfig.Hooks.clone()
	cloned.Endpoints = append([]Endpoint(nil), applicationConfig.Endpoints...)
	cloned.Collections = make([]Collection, len(applicationConfig.Collections))
	for index, collection := range applicationConfig.Collections {
		cloned.Collections[index] = collection
		cloned.Collections[index].Labels.SingularTranslations = cloneStringMap(collection.Labels.SingularTranslations)
		cloned.Collections[index].Labels.PluralTranslations = cloneStringMap(collection.Labels.PluralTranslations)
		cloned.Collections[index].Admin.GroupTranslations = cloneStringMap(collection.Admin.GroupTranslations)
		cloned.Collections[index].Admin.DescriptionTranslations = cloneStringMap(collection.Admin.DescriptionTranslations)
		cloned.Collections[index].AuthConfig = cloneAuthConfig(collection.AuthConfig)
		cloned.Collections[index].Fields = collection.Fields.Snapshot()
		cloned.Collections[index].Indexes = make([]CollectionIndex, len(collection.Indexes))
		for indexIndex, candidate := range collection.Indexes {
			cloned.Collections[index].Indexes[indexIndex] = candidate
			cloned.Collections[index].Indexes[indexIndex].Fields = append([]string(nil), candidate.Fields...)
		}
		cloned.Collections[index].Admin.DefaultColumns = append([]string(nil), collection.Admin.DefaultColumns...)
		cloned.Collections[index].Admin.LivePreview.Breakpoints = clonePreviewBreakpoints(collection.Admin.LivePreview.Breakpoints)
		cloned.Collections[index].UploadConfig.MimeTypes = append([]string(nil), collection.UploadConfig.MimeTypes...)
		cloned.Collections[index].UploadConfig.ImageSizes = append([]ImageSize(nil), collection.UploadConfig.ImageSizes...)
		cloned.Collections[index].Hooks = collection.Hooks.clone()
		cloned.Collections[index].Endpoints = append([]Endpoint(nil), collection.Endpoints...)
	}
	cloned.Globals = make([]Global, len(applicationConfig.Globals))
	for index, global := range applicationConfig.Globals {
		cloned.Globals[index] = global
		cloned.Globals[index].LabelTranslations = cloneStringMap(global.LabelTranslations)
		cloned.Globals[index].Admin.GroupTranslations = cloneStringMap(global.Admin.GroupTranslations)
		cloned.Globals[index].Admin.DescriptionTranslations = cloneStringMap(global.Admin.DescriptionTranslations)
		cloned.Globals[index].Admin.LivePreview.Breakpoints = clonePreviewBreakpoints(global.Admin.LivePreview.Breakpoints)
		cloned.Globals[index].Fields = global.Fields.Snapshot()
		cloned.Globals[index].Hooks = global.Hooks.clone()
		cloned.Globals[index].Endpoints = append([]Endpoint(nil), global.Endpoints...)
	}
	cloned.Tasks = append([]TaskDefinition(nil), applicationConfig.Tasks...)
	cloned.Plugins = append([]Plugin(nil), applicationConfig.Plugins...)
	cloned.pluginEndpoints = append([]runtimePluginEndpoint(nil), applicationConfig.pluginEndpoints...)
	cloned.Storage = applicationConfig.Storage
	return cloned
}

func clonePreviewBreakpoints(breakpoints []PreviewBreakpoint) []PreviewBreakpoint {
	cloned := make([]PreviewBreakpoint, len(breakpoints))
	for index, breakpoint := range breakpoints {
		cloned[index] = breakpoint
		cloned[index].LabelTranslations = cloneStringMap(breakpoint.LabelTranslations)
	}
	return cloned
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneAuthConfig(config AuthConfig) AuthConfig {
	cloned := config
	if config.Verify != nil {
		verification := *config.Verify
		cloned.Verify = &verification
	}
	cloned.Strategies = append([]AuthStrategy(nil), config.Strategies...)
	cloned.Hooks.BeforeLogin = append([]AuthHook(nil), config.Hooks.BeforeLogin...)
	cloned.Hooks.AfterLogin = append([]AuthHook(nil), config.Hooks.AfterLogin...)
	cloned.Hooks.AfterMe = append([]AuthHook(nil), config.Hooks.AfterMe...)
	cloned.Hooks.BeforeLogout = append([]AuthHook(nil), config.Hooks.BeforeLogout...)
	cloned.Hooks.AfterLogout = append([]AuthHook(nil), config.Hooks.AfterLogout...)
	cloned.Hooks.BeforeRefresh = append([]AuthHook(nil), config.Hooks.BeforeRefresh...)
	cloned.Hooks.AfterRefresh = append([]AuthHook(nil), config.Hooks.AfterRefresh...)
	cloned.Hooks.BeforeForgotPassword = append([]AuthHook(nil), config.Hooks.BeforeForgotPassword...)
	cloned.Hooks.AfterForgotPassword = append([]AuthHook(nil), config.Hooks.AfterForgotPassword...)
	cloned.Hooks.BeforePasswordReset = append([]AuthHook(nil), config.Hooks.BeforePasswordReset...)
	cloned.Hooks.AfterPasswordReset = append([]AuthHook(nil), config.Hooks.AfterPasswordReset...)
	cloned.Hooks.BeforeVerification = append([]AuthHook(nil), config.Hooks.BeforeVerification...)
	cloned.Hooks.AfterVerification = append([]AuthHook(nil), config.Hooks.AfterVerification...)
	cloned.Hooks.BeforeAPIKey = append([]AuthHook(nil), config.Hooks.BeforeAPIKey...)
	cloned.Hooks.AfterAPIKey = append([]AuthHook(nil), config.Hooks.AfterAPIKey...)
	return cloned
}
