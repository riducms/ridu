package core

import (
	"context"
	"net/http"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Plugin is compiled, trusted application code with a stable manifest key.
// Focused capability interfaces extend it without growing one universal bag of
// optional methods.
type Plugin interface {
	Key() string
}

// PluginAPIVersion is the compiled backend extension contract understood by
// this Ridu release. It changes only when public plugin capability interfaces
// become incompatible.
const PluginAPIVersion = schema.CurrentPluginAPIVersion

// PluginDescriptor is deterministic public metadata for one compiled plugin.
// It is copied into the canonical manifest so generators, migration planning,
// the admin build, and compatibility tooling all inspect the same declaration.
// Executable hooks, handlers, validators, and secrets never belong here.
type PluginDescriptor struct {
	// Version is the plugin's complete semantic release without a leading v.
	Version string
	// GoPackage is the canonical import path that provides the plugin.
	GoPackage string
	// APIVersion must equal PluginAPIVersion for this Ridu release.
	APIVersion uint32
	// Ridu limits the framework releases allowed to compile this plugin.
	Ridu RiduCompatibility
	// Admin describes the optional statically bundled admin half.
	Admin *AdminPluginMetadata
	// FieldTypes maps every reusable plugin field to generated public types.
	FieldTypes []PluginFieldType
	// DatabaseContributions contains exceptional, adapter-specific private
	// database state. Ordinary plugin data should be added as collections or
	// fields through ConfigTransformer instead.
	DatabaseContributions []PluginDatabaseContribution
}

// PluginDatabaseAdapter identifies one database dialect supported by a
// plugin's exceptional private-schema contribution.
type PluginDatabaseAdapter = schema.PluginDatabaseAdapter

const (
	// PluginDatabaseAdapterPostgres selects PostgreSQL-specific SQL.
	PluginDatabaseAdapterPostgres = schema.PluginDatabaseAdapterPostgres
	// PluginDatabaseAdapterSQLite selects SQLite-specific SQL.
	PluginDatabaseAdapterSQLite = schema.PluginDatabaseAdapterSQLite
)

// PluginDatabaseContribution is one explicitly adapter-scoped private-schema
// bundle. It is an escape hatch for database features that cannot be modeled
// as ordinary Ridu collections or fields; its SQL is never treated as portable.
type PluginDatabaseContribution struct {
	Adapter    PluginDatabaseAdapter
	Migrations []PluginMigration
	Tables     []string
}

// RiduCompatibility declares the supported framework-version interval.
// MaximumExclusive is optional; an empty maximum leaves the upper bound open.
type RiduCompatibility struct {
	// Minimum is the oldest supported Ridu semantic version, inclusive.
	Minimum string
	// MaximumExclusive is the optional first unsupported Ridu version.
	MaximumExclusive string
}

// PluginFieldType gives generated contracts exact types for a plugin-owned
// field. TypeScript names are imported with `import type`; GoPackage and GoType
// are optional and fall back to encoding/json.RawMessage when omitted.
type PluginFieldType struct {
	// Key matches the PluginField key stored in the schema manifest.
	Key string
	// TypeScriptPackage exports the named output, input, and where types.
	TypeScriptPackage string
	// TypeScriptOutput is the stored document value type export.
	TypeScriptOutput string
	// TypeScriptInput is the create and update value type export.
	TypeScriptInput string
	// TypeScriptWhere is the optional query operand type export.
	TypeScriptWhere string
	// GoPackage and GoType optionally name the generated Go model type.
	GoPackage string
	// GoType is an exported type in GoPackage; both Go fields are optional together.
	GoType string
	// JSONSchema is the deterministic OpenAPI 3.1 schema for one field value.
	JSONSchema []byte
}

// PluginMigration is one contiguous, reversible adapter-specific transition.
// Version migrates from Version-1 to Version. Statements are copied into the
// immutable migration artifact and checksum-verified before execution.
type PluginMigration struct {
	// Version starts at 1 and increments without gaps.
	Version uint32
	// Name is a stable lowercase kebab-case review label.
	Name string
	// UpSQL executes in order when entering Version.
	UpSQL []string
	// DownSQL executes in order when leaving Version.
	DownSQL []string
}

// DescriptorProvider opts a plugin into versioned generation, migration, and
// compatibility tooling. Plugins exposing any advanced capability must provide
// a descriptor; key-only plugins remain supported for config transforms and
// validators that need no build metadata.
type DescriptorProvider interface {
	Plugin
	Descriptor() PluginDescriptor
}

// PluginGenerationContext supplies immutable schema state to a compiled
// plugin while the application project command is producing generated files.
// Generation runs without a store, storage backend, or network listener.
type PluginGenerationContext struct {
	Manifest schema.Manifest
}

// PluginGeneratedArtifact is one deterministic, plugin-owned generated file.
// Name is scoped beneath the provider's plugin key; the portable CLI owns the
// project-relative destination and atomic installation of Content.
type PluginGeneratedArtifact struct {
	Name    string
	Content []byte
}

// GenerationProvider contributes requested deterministic build artifacts that
// require executable plugin configuration and therefore cannot be
// reconstructed from the serializable manifest alone. Providers do not run
// unless ridu.toml maps one of their artifacts to a destination.
type GenerationProvider interface {
	Plugin
	GeneratedArtifacts(PluginGenerationContext) ([]PluginGeneratedArtifact, error)
}

// AdminPluginAPIVersion is the build-time contract understood by this Ridu
// release. Admin packages declare the same value so generated registries can
// reject incompatible packages before the application is used.
const AdminPluginAPIVersion = schema.CurrentAdminPluginAPIVersion

// AdminPluginMetadata describes the statically imported admin half of a
// compiled backend plugin. Package must be an installed JavaScript package
// specifier, Export must be its named AdminPlugin export, and PairingVersion
// must match the admin export. Increment PairingVersion when the backend and
// admin halves stop being mutually compatible.
type AdminPluginMetadata struct {
	// Package is an installed bare JavaScript package specifier.
	Package string
	// Export is the named AdminPlugin export in Package.
	Export string
	// APIVersion must equal AdminPluginAPIVersion.
	APIVersion uint32
	// PairingVersion changes when backend and admin halves cease to match.
	PairingVersion uint32
	// Routes lists authenticated admin route paths in exact registration order.
	Routes []string
	// Assets lists package-relative static assets in exact registration order.
	Assets []string
}

// PluginHookContribution appends hooks to one resolved collection or field.
// Contributions run in Config.Plugins order after application-authored hooks.
type PluginHookContribution struct {
	// Collection identifies the collection receiving Hooks.
	Collection schema.CollectionSlug
	// FieldPath optionally limits Hooks to one canonical field path.
	FieldPath string
	// Hooks are appended after application-authored hooks.
	Hooks CollectionHooks
}

// HookProvider contributes compiled lifecycle hooks without mutating authoring
// config through a generic transformer.
type HookProvider interface {
	Plugin
	Hooks() []PluginHookContribution
}

// PluginEndpoint is one exact, method-specific HTTP endpoint. Path is relative
// to /api/plugins/<plugin-key>/ and must not contain traversal segments.
type PluginEndpoint struct {
	// Method is an exact supported HTTP method such as GET or POST.
	Method string
	// Path is relative to the plugin's namespaced API prefix.
	Path string
	// Summary appears in generated OpenAPI.
	Summary string
	// MaxBodyBytes bounds the raw request body before Handler receives it. Zero
	// inherits HandlerOptions.MaxBodyBytes; a negative value explicitly opts a
	// trusted streaming endpoint out of that byte bound.
	MaxBodyBytes int64
	// Handler is trusted compiled endpoint code.
	Handler PluginEndpointHandler
}

// PluginEndpointContext exposes the authenticated actor and access-controlled
// local API while retaining ordinary net/http request and response contracts.
type PluginEndpointContext struct {
	// Writer receives the endpoint response.
	Writer http.ResponseWriter
	// Request is the original namespaced HTTP request.
	Request *http.Request
	// ClientIP is the direct or trusted-forwarded client address resolved by
	// the framework HTTP boundary. Authentication transports should use this
	// value instead of interpreting forwarding headers independently.
	ClientIP string
	// Actor is the authenticated user, or nil for an anonymous request.
	Actor *store.Document
	// ActorCollection identifies the auth collection that owns Actor.
	ActorCollection schema.CollectionSlug
	// Local enters the same access-controlled operation engine as other APIs.
	Local *LocalAPI
	// AdmitAuthAttempt applies the application's distributed IP and identity
	// admission policy. Authentication transports must call it before work that
	// can be amplified by aliases or repeated requests.
	AdmitAuthAttempt func(context.Context, string, string) error
	// ReportError records a stable transport error code and sends trusted error
	// detail to HandlerOptions.RequestError without exposing it to the client.
	// Plugins that implement their own error envelopes should call this for
	// internal failures. Passing nil records only Code in request observations.
	ReportError func(error, string)
}

// PluginEndpointHandler handles one trusted compiled plugin endpoint.
type PluginEndpointHandler func(PluginEndpointContext)

// EndpointProvider contributes exact REST endpoints under the provider's
// namespaced /api/plugins/<key>/ prefix.
type EndpointProvider interface {
	Plugin
	Endpoints() []PluginEndpoint
}

// PluginTransportContext binds one protocol transport to the fully resolved
// application runtime. The manifest is immutable and Local enters the same
// access-controlled operation engine as REST and jobs.
type PluginTransportContext struct {
	Manifest schema.Manifest
	Local    *LocalAPI
	// App exposes public authentication and other application-owned operations
	// that do not belong to LocalAPI. It is fully initialized before binding.
	App *App
}

// PluginTransport is one exact application-level HTTP transport. Unlike
// PluginEndpoint, Path is absolute and is not placed below a plugin namespace.
// It exists for established protocol locations such as /api/graphql, not for
// ordinary plugin REST endpoints.
type PluginTransport struct {
	Method       string
	Path         string
	Summary      string
	MaxBodyBytes int64
	Handler      PluginEndpointHandler
}

// TransportProvider binds an optional compiled protocol after the manifest and
// operation engine are ready. Binding happens once during New; transports must
// not lazily rebuild schema state per request.
type TransportProvider interface {
	Plugin
	BindTransports(PluginTransportContext) ([]PluginTransport, error)
}

// ConfigTransformer is the Phase 1 plugin capability for contributing or
// transforming authoring config before final validation. Implementations
// receive and return defensive copies; transform order is the Config.Plugins
// declaration order.
type ConfigTransformer interface {
	Plugin
	TransformConfig(Config) (Config, error)
}

// PluginFieldValidationContext supplies a resolved plugin field and candidate value.
type PluginFieldValidationContext struct {
	// Field is the resolved manifest definition owned by the plugin.
	Field schema.Field
	// RuntimePath identifies this concrete value occurrence. Unlike Field.Path,
	// it includes array and block indexes and omits block schema discriminators.
	RuntimePath string
	// Value is the candidate document value to validate.
	Value store.Value
}

// PluginFieldValidator returns path-aware issues for a plugin-owned value.
type PluginFieldValidator func(PluginFieldValidationContext) []schema.Issue

// FieldValidatorProvider is a focused runtime capability for plugin-owned
// value contracts. Keys must match manifest plugin field keys.
type FieldValidatorProvider interface {
	Plugin
	FieldValidators() map[string]PluginFieldValidator
}
