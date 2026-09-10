package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode"
	"unicode/utf8"

	"github.com/riducms/ridu/query"
	"golang.org/x/text/language"
)

// Version is the schema manifest format version. It changes independently from
// the Ridu binary version.
type Version uint32

// CurrentVersion is the manifest format produced and accepted by this build.
const CurrentVersion Version = 1

// CurrentAdminPluginAPIVersion is the static backend/admin pairing contract
// understood by this manifest version.
const CurrentAdminPluginAPIVersion uint32 = 1

// CurrentPluginAPIVersion is the compiled backend plugin contract represented
// by this manifest version.
const CurrentPluginAPIVersion uint32 = 1

// FieldType identifies the concrete manifest contract for a field.
type FieldType string

const (
	FieldTypeText         FieldType = "text"
	FieldTypeTextList     FieldType = "text-list"
	FieldTypeNumberList   FieldType = "number-list"
	FieldTypeCode         FieldType = "code"
	FieldTypeSelect       FieldType = "select"
	FieldTypeRadio        FieldType = "radio"
	FieldTypePoint        FieldType = "point"
	FieldTypeUI           FieldType = "ui"
	FieldTypeJoin         FieldType = "join"
	FieldTypeVirtual      FieldType = "virtual"
	FieldTypeRelationship FieldType = "relationship"
	FieldTypeUpload       FieldType = "upload"
	FieldTypeGroup        FieldType = "group"
	FieldTypeTextarea     FieldType = "textarea"
	FieldTypeEmail        FieldType = "email"
	FieldTypeDate         FieldType = "date"
	FieldTypeNumber       FieldType = "number"
	FieldTypeCheckbox     FieldType = "checkbox"
	FieldTypeJSON         FieldType = "json"
	FieldTypeArray        FieldType = "array"
	FieldTypeBlocks       FieldType = "blocks"
	FieldTypePlugin       FieldType = "plugin"
)

// FieldCategory distinguishes broad field behavior without a stringly typed
// capability map.
type FieldCategory string

const (
	FieldCategoryScalar       FieldCategory = "scalar"
	FieldCategoryNested       FieldCategory = "nested"
	FieldCategoryPresentation FieldCategory = "presentation"
	FieldCategoryRelationship FieldCategory = "relationship"
	FieldCategoryUpload       FieldCategory = "upload"
	FieldCategoryPlugin       FieldCategory = "plugin"
)

// Snapshot is a detached, serializable view of a Manifest.
type Snapshot struct {
	Version     Version      `json:"version"`
	Application Application  `json:"application"`
	Blocks      []BlockType  `json:"blocks,omitempty"`
	Collections []Collection `json:"collections"`
	Globals     []Global     `json:"globals,omitempty"`
	Plugins     []Plugin     `json:"plugins"`
}

// Application contains framework-level application metadata.
type Application struct {
	// Name is the resolved author-facing application name.
	Name string `json:"name"`
	// NameTranslations overrides Name for configured admin interface languages.
	NameTranslations map[string]string `json:"nameTranslations,omitempty"`
	// AllowIDOnCreate permits callers to supply a validated canonical string ID
	// on ordinary creates. Migration imports preserve source IDs independently.
	AllowIDOnCreate bool `json:"allowIDOnCreate,omitempty"`
	// Admin contains admin authentication policy when auth is enabled.
	Admin *AdminSettings `json:"admin,omitempty"`
	// AdminLocalization is the deterministic interface language and timezone
	// contract. Catalog text is supplied by the statically built admin.
	AdminLocalization *AdminLocalizationSettings `json:"adminLocalization,omitempty"`
	// Localization is the deterministic content-locale contract, when enabled.
	Localization *LocalizationSettings `json:"localization,omitempty"`
	// Endpoints lists application-level custom endpoint metadata. Executable
	// handlers remain in Go runtime configuration.
	Endpoints []Endpoint `json:"endpoints,omitempty"`
}

// Endpoint is the deterministic public metadata for one application-authored
// custom endpoint. Handler code and body limits are runtime-only.
type Endpoint struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Summary string `json:"summary,omitempty"`
}

// AdminLocalizationSettings configures translated interface presentation.
// It remains independent from content LocalizationSettings.
type AdminLocalizationSettings struct {
	Languages       []AdminLanguage `json:"languages"`
	DefaultLanguage string          `json:"defaultLanguage"`
	TimeZones       []AdminTimeZone `json:"timeZones,omitempty"`
	DefaultTimeZone string          `json:"defaultTimeZone,omitempty"`
}

// AdminLanguage is one statically bundled interface language.
type AdminLanguage struct {
	Code              string            `json:"code"`
	Label             string            `json:"label"`
	LabelTranslations map[string]string `json:"labelTranslations,omitempty"`
	RTL               bool              `json:"rtl,omitempty"`
}

// AdminTimeZone is one IANA timezone offered to editors.
type AdminTimeZone struct {
	ID                string            `json:"id"`
	Label             string            `json:"label"`
	LabelTranslations map[string]string `json:"labelTranslations,omitempty"`
}

// LocalizationSettings is the public, serializable content localization
// contract. Executable locale availability rules remain in Go configuration.
type LocalizationSettings struct {
	Locales       []Locale   `json:"locales"`
	DefaultLocale LocaleCode `json:"defaultLocale"`
	Fallback      bool       `json:"fallback"`
}

// LocaleCodes returns configured locale identities in deterministic authoring order.
func (settings LocalizationSettings) LocaleCodes() []LocaleCode {
	codes := make([]LocaleCode, len(settings.Locales))
	for index, locale := range settings.Locales {
		codes[index] = locale.Code
	}
	return codes
}

// Locale is one configured content locale in deterministic authoring order.
type Locale struct {
	Code            LocaleCode   `json:"code"`
	Label           string       `json:"label"`
	RTL             bool         `json:"rtl,omitempty"`
	FallbackLocales []LocaleCode `json:"fallbackLocale,omitempty"`
}

// AdminSettings identifies the auth collection trusted by the admin UI.
type AdminSettings struct {
	// UserCollectionID is the durable identity of the selected auth collection.
	UserCollectionID StableID `json:"userCollectionId"`
	// UserCollectionSlug is the API-facing slug of the selected auth collection.
	UserCollectionSlug CollectionSlug `json:"userCollectionSlug"`
}

// Plugin records one compiled plugin that participated in config resolution.
type Plugin struct {
	Key                   string                       `json:"key"`
	Version               string                       `json:"version,omitempty"`
	GoPackage             string                       `json:"goPackage,omitempty"`
	APIVersion            uint32                       `json:"apiVersion,omitempty"`
	Ridu                  *PluginCompatibility         `json:"ridu,omitempty"`
	Admin                 *PluginAdmin                 `json:"admin,omitempty"`
	FieldTypes            []PluginFieldType            `json:"fieldTypes,omitempty"`
	DatabaseContributions []PluginDatabaseContribution `json:"databaseContributions,omitempty"`
	Endpoints             []PluginEndpoint             `json:"endpoints,omitempty"`
}

// PluginDatabaseAdapter is a closed database dialect identity for exceptional
// plugin-owned schema. Raw SQL is meaningful only to the named adapter.
type PluginDatabaseAdapter string

const (
	PluginDatabaseAdapterPostgres PluginDatabaseAdapter = "postgres"
	PluginDatabaseAdapterSQLite   PluginDatabaseAdapter = "sqlite"
)

// PluginDatabaseContribution is one adapter-specific migration history and
// its plugin-owned physical tables.
type PluginDatabaseContribution struct {
	Adapter    PluginDatabaseAdapter `json:"adapter"`
	Migrations []PluginMigration     `json:"migrations,omitempty"`
	Tables     []string              `json:"tables,omitempty"`
}

// DatabaseContribution returns the private-schema bundle for adapter.
func (plugin Plugin) DatabaseContribution(adapter PluginDatabaseAdapter) (PluginDatabaseContribution, bool) {
	for _, contribution := range plugin.DatabaseContributions {
		if contribution.Adapter == adapter {
			return contribution, true
		}
	}
	return PluginDatabaseContribution{}, false
}

// HasDatabaseContributions reports whether a plugin requires private physical
// schema outside ordinary Ridu collections and fields.
func (plugin Plugin) HasDatabaseContributions() bool {
	return len(plugin.DatabaseContributions) != 0
}

// PluginCompatibility is a half-open semantic-version interval.
type PluginCompatibility struct {
	Minimum          string `json:"minimum"`
	MaximumExclusive string `json:"maximumExclusive,omitempty"`
}

// PluginAdmin describes one statically imported admin package paired with a
// compiled backend plugin. It contains only public build metadata, never
// executable code or secrets.
type PluginAdmin struct {
	Package        string   `json:"package"`
	Export         string   `json:"export"`
	APIVersion     uint32   `json:"apiVersion"`
	PairingVersion uint32   `json:"pairingVersion"`
	Routes         []string `json:"routes,omitempty"`
	Assets         []string `json:"assets,omitempty"`
}

// PluginFieldType describes generated language types for one plugin field key.
type PluginFieldType struct {
	// EmbeddedTypes lists tree.case selectors supplying ordered generic payload type arguments.
	EmbeddedTypes     []string        `json:"embeddedTypes,omitempty"`
	Key               string          `json:"key"`
	TypeScriptPackage string          `json:"typescriptPackage"`
	TypeScriptOutput  string          `json:"typescriptOutput"`
	TypeScriptInput   string          `json:"typescriptInput"`
	TypeScriptWhere   string          `json:"typescriptWhere,omitempty"`
	GoPackage         string          `json:"goPackage,omitempty"`
	GoType            string          `json:"goType,omitempty"`
	JSONSchema        json.RawMessage `json:"jsonSchema,omitempty"`
}

// PluginMigration is a declarative, contiguous adapter-specific transition.
type PluginMigration struct {
	Version uint32   `json:"version"`
	Name    string   `json:"name"`
	UpSQL   []string `json:"upSQL"`
	DownSQL []string `json:"downSQL"`
}

// PluginEndpoint is the public manifest contract for one compiled endpoint.
type PluginEndpoint struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Summary string `json:"summary"`
}

// Collection is one resolved collection schema.
type Collection struct {
	ID           StableID              `json:"id"`
	Slug         CollectionSlug        `json:"slug"`
	Labels       CollectionLabels      `json:"labels"`
	Admin        CollectionAdmin       `json:"admin"`
	Capabilities Capabilities          `json:"capabilities"`
	Auth         *AuthSettings         `json:"authSettings,omitempty"`
	Upload       *UploadSettings       `json:"uploadSettings,omitempty"`
	Versions     *VersionSettings      `json:"versionSettings,omitempty"`
	DocumentLock *DocumentLockSettings `json:"documentLockSettings,omitempty"`
	Fields       []Field               `json:"fields"`
	Indexes      []CollectionIndex     `json:"indexes,omitempty"`
	Endpoints    []Endpoint            `json:"endpoints,omitempty"`
}

// CollectionIndex is one ordered compound index over supported field paths.
// Unique indexes use NULLS DISTINCT semantics in every official store.
type CollectionIndex struct {
	Fields []query.Path `json:"fields"`
	Unique bool         `json:"unique,omitempty"`
}

type CollectionAdmin struct {
	UseAsTitle              string            `json:"useAsTitle,omitempty"`
	DefaultColumns          []string          `json:"defaultColumns,omitempty"`
	Group                   string            `json:"group,omitempty"`
	GroupTranslations       map[string]string `json:"groupTranslations,omitempty"`
	Description             string            `json:"description,omitempty"`
	DescriptionTranslations map[string]string `json:"descriptionTranslations,omitempty"`
	FolderField             string            `json:"folderField,omitempty"`
	ParentField             string            `json:"parentField,omitempty"`
	LivePreview             *LivePreview      `json:"livePreview,omitempty"`
}

// DocumentLockSettings controls the admin lock lease for one collection.
type DocumentLockSettings struct {
	DurationSeconds int64 `json:"durationSeconds"`
}

// LivePreview is the serializable document-preview contract consumed by the admin.
type LivePreview struct {
	URL         string              `json:"url"`
	Breakpoints []PreviewBreakpoint `json:"breakpoints,omitempty"`
}

// PreviewBreakpoint describes one named live-preview viewport.
type PreviewBreakpoint struct {
	Name              string            `json:"name"`
	Label             string            `json:"label"`
	LabelTranslations map[string]string `json:"labelTranslations,omitempty"`
	Width             int               `json:"width"`
	Height            int               `json:"height"`
}

// Global is one resolved singleton schema. It deliberately shares the field,
// version, and presentation contracts used by collections while living in a
// separate manifest namespace and API route.
type Global = Collection

type AuthSettings struct {
	IdentityField                     string `json:"identityField"`
	SessionDurationSeconds            int64  `json:"sessionDurationSeconds"`
	PasswordMinLength                 int    `json:"passwordMinLength"`
	PasswordMaxBytes                  int    `json:"passwordMaxBytes"`
	PasswordBcryptCost                int    `json:"passwordBcryptCost"`
	MaxLoginAttempts                  int    `json:"maxLoginAttempts"`
	LockDurationSeconds               int64  `json:"lockDurationSeconds"`
	PasswordReset                     bool   `json:"passwordReset"`
	PasswordResetTokenDurationSeconds int64  `json:"passwordResetTokenDurationSeconds"`
	VerifyEmail                       bool   `json:"verifyEmail"`
	VerificationTokenDurationSeconds  int64  `json:"verificationTokenDurationSeconds"`
	APIKeys                           bool   `json:"apiKeys"`
}

// UploadSettings defines collection-owned media validation and delivery.
type UploadSettings struct {
	MaxFileSize int64       `json:"maxFileSize"`
	MimeTypes   []string    `json:"mimeTypes"`
	Private     bool        `json:"private"`
	ImageSizes  []ImageSize `json:"imageSizes,omitempty"`
}

type ImageSize struct {
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Fit    string `json:"fit"`
}

// VersionSettings defines drafts, snapshot retention, autosave, and
// optimistic-revision behavior for one collection.
type VersionSettings struct {
	Drafts                  bool  `json:"drafts"`
	MaxPerDocument          int   `json:"maxPerDocument"`
	AutosaveIntervalSeconds int64 `json:"autosaveIntervalSeconds"`
}

// CollectionLabels contains normalized author-facing names.
type CollectionLabels struct {
	Singular             string            `json:"singular"`
	SingularTranslations map[string]string `json:"singularTranslations,omitempty"`
	Plural               string            `json:"plural"`
	PluralTranslations   map[string]string `json:"pluralTranslations,omitempty"`
}

// Capabilities reserves explicit, typed manifest positions for collection
// behaviors that arrive in later vertical slices.
type Capabilities struct {
	Auth     bool `json:"auth"`
	Upload   bool `json:"upload"`
	Versions bool `json:"versions"`
	Trash    bool `json:"trash"`
	Locking  bool `json:"locking,omitempty"`
	Global   bool `json:"global,omitempty"`
}

// Field is one resolved field with exactly one matching typed detail contract.
type Field struct {
	ID        StableID      `json:"id"`
	Name      string        `json:"name"`
	Path      query.Path    `json:"path"`
	Type      FieldType     `json:"type"`
	Category  FieldCategory `json:"category"`
	Required  bool          `json:"required"`
	Unique    bool          `json:"unique"`
	Index     bool          `json:"index,omitempty"`
	Localized bool          `json:"localized,omitempty"`
	// QueryRestricted records an attached read policy's static prohibition on
	// caller filtering, sorting and distinct queries. It is not an access verdict.
	QueryRestricted bool    `json:"queryRestricted,omitempty"`
	Default         *string `json:"default,omitempty"`
	// DynamicDefault permits omitted input to reach server initialization. It does
	// not contain executable code or promise that the callback supplies a value.
	DynamicDefault bool `json:"dynamicDefault,omitempty"`
	// LiveValidation enables explicit advisory server feedback without serializing callbacks.
	LiveValidation bool                `json:"liveValidation,omitempty"`
	Admin          FieldAdmin          `json:"admin"`
	Text           *TextField          `json:"text,omitempty"`
	Textarea       *TextField          `json:"textarea,omitempty"`
	Code           *CodeField          `json:"code,omitempty"`
	Number         *NumberField        `json:"number,omitempty"`
	Date           *DateField          `json:"date,omitempty"`
	Select         *SelectField        `json:"select,omitempty"`
	Point          *PointField         `json:"point,omitempty"`
	UI             *UIField            `json:"ui,omitempty"`
	Join           *JoinField          `json:"join,omitempty"`
	Virtual        *VirtualField       `json:"virtual,omitempty"`
	Relationship   *RelationshipField  `json:"relationship,omitempty"`
	List           *PrimitiveListField `json:"list,omitempty"`
	Upload         *UploadField        `json:"upload,omitempty"`
	Nested         *NestedField        `json:"nested,omitempty"`
	Blocks         *BlocksField        `json:"blocks,omitempty"`
	Plugin         *PluginField        `json:"plugin,omitempty"`
}

// FieldAdmin contains serializable presentation metadata, never authorization.
type FieldAdmin struct {
	// Extensions are public finite JSON attachments owned by this field.
	Extensions              map[string]json.RawMessage `json:"extensions,omitempty"`
	Editor                  *FieldEditor               `json:"editor,omitempty"`
	Label                   string                     `json:"label"`
	LabelTranslations       map[string]string          `json:"labelTranslations,omitempty"`
	Description             string                     `json:"description,omitempty"`
	DescriptionTranslations map[string]string          `json:"descriptionTranslations,omitempty"`
	Placeholder             string                     `json:"placeholder,omitempty"`
	PlaceholderTranslations map[string]string          `json:"placeholderTranslations,omitempty"`
	ReadOnly                bool                       `json:"readOnly,omitempty"`
	Hidden                  bool                       `json:"hidden,omitempty"`
	Sidebar                 bool                       `json:"sidebar,omitempty"`
	Columns                 int                        `json:"columns,omitempty"`
	Row                     *FieldRow                  `json:"row,omitempty"`
	Collapsible             *FieldCollapsible          `json:"collapsible,omitempty"`
	Tab                     string                     `json:"tab,omitempty"`
	TabTranslations         map[string]string          `json:"tabTranslations,omitempty"`
	TabGroup                *FieldTabGroup             `json:"tabGroup,omitempty"`
	NamedTab                bool                       `json:"namedTab,omitempty"`
	Condition               *FieldCondition            `json:"condition,omitempty"`
	Component               *FieldAdminComponent       `json:"component,omitempty"`
}

// FieldEditor selects an application-local value editor. Config is public JSON.
type FieldEditor struct {
	Reference string          `json:"reference"`
	Config    json.RawMessage `json:"config,omitempty"`
}

// FieldAdminComponent selects one statically registered plugin renderer for a
// field without changing its storage, validation, access, or generated type.
type FieldAdminComponent struct {
	// Reference selects an application-local row label; it is not valid for field renderers.
	Reference string          `json:"reference,omitempty"`
	Plugin    string          `json:"plugin,omitempty"`
	Component string          `json:"component,omitempty"`
	Config    json.RawMessage `json:"config,omitempty"`
}

// FieldRow identifies fields authored as one presentation-only layout row.
// It never contributes a document path or stored value.
type FieldRow struct {
	ID         StableID                   `json:"id"`
	Extensions map[string]json.RawMessage `json:"extensions,omitempty"`
}

// FieldCollapsible identifies flattened fields presented inside one disclosure.
type FieldCollapsible struct {
	ID                 StableID                   `json:"id"`
	Label              string                     `json:"label"`
	LabelTranslations  map[string]string          `json:"labelTranslations,omitempty"`
	InitiallyCollapsed bool                       `json:"initiallyCollapsed,omitempty"`
	Extensions         map[string]json.RawMessage `json:"extensions,omitempty"`
}

// FieldTabGroup identifies fields authored by one presentation tabs definition.
// It preserves local tab-group boundaries without contributing a document path.
type FieldTabGroup struct {
	ID         StableID                   `json:"id"`
	Extensions map[string]json.RawMessage `json:"extensions,omitempty"`
}

// FieldConditionKind identifies one node in a deterministic presentation expression.
type FieldConditionKind string

const (
	FieldConditionKindAll       FieldConditionKind = "all"
	FieldConditionKindAny       FieldConditionKind = "any"
	FieldConditionKindNot       FieldConditionKind = "not"
	FieldConditionKindPredicate FieldConditionKind = "predicate"
)

// FieldConditionScope selects the value tree used by a condition predicate.
type FieldConditionScope string

const (
	FieldConditionDocument FieldConditionScope = "document"
	FieldConditionSibling  FieldConditionScope = "sibling"
)

// FieldConditionOperator identifies one scalar condition comparison.
type FieldConditionOperator string

const (
	FieldConditionEquals    FieldConditionOperator = "equals"
	FieldConditionNotEquals FieldConditionOperator = "notEquals"
	FieldConditionOneOf     FieldConditionOperator = "oneOf"
)

// FieldCondition is one recursive, presentation-only condition node.
type FieldCondition struct {
	Kind       FieldConditionKind       `json:"kind"`
	Conditions []FieldCondition         `json:"conditions,omitempty"`
	Predicate  *FieldConditionPredicate `json:"predicate,omitempty"`
}

// FieldConditionPredicate compares one scoped document value with typed scalars.
type FieldConditionPredicate struct {
	Scope    FieldConditionScope    `json:"scope"`
	Path     query.Path             `json:"path"`
	Operator FieldConditionOperator `json:"operator"`
	Values   []FieldConditionValue  `json:"values"`
}

// FieldConditionValue stores one canonical scalar operand.
type FieldConditionValue struct {
	Type  ValueType `json:"type"`
	Value string    `json:"value"`
}

// TextField contains inclusive Unicode code-point length constraints and
// optional first-class slug behavior while retaining ordinary text storage.
type TextField struct {
	MinLength *int       `json:"minLength,omitempty"`
	MaxLength *int       `json:"maxLength,omitempty"`
	Slug      *SlugField `json:"slug,omitempty"`
}

// SlugField derives a normalized URL segment from one non-repeated string field.
// The operation engine remains authoritative; the admin mirrors this contract.
type SlugField struct {
	SourcePath query.Path `json:"sourcePath"`
}

// CodeField contains editor metadata for a stored source string.
type CodeField struct {
	Language  string `json:"language,omitempty"`
	MinLength *int   `json:"minLength,omitempty"`
	MaxLength *int   `json:"maxLength,omitempty"`
}

// NumberField contains inclusive runtime bounds and the admin input step.
// Step is presentation metadata and is not a divisibility constraint.
type NumberField struct {
	Min  *float64 `json:"min,omitempty"`
	Max  *float64 `json:"max,omitempty"`
	Step *float64 `json:"step,omitempty"`
}

// DateFormat identifies the accepted date string contract across APIs and admin.
type DateFormat string

const (
	DateOnly DateFormat = "date"
	DateTime DateFormat = "date-time"
	TimeOnly DateFormat = "time"
)

// DateField describes how a stored date string is authored and presented.
type DateField struct {
	Format DateFormat `json:"format"`
}

// PointField marks a [longitude, latitude] value.
type PointField struct{}

// UIField marks presentation-only content omitted from document values.
type UIField struct{}

// ValueType is the finite generated output vocabulary for computed fields.
type ValueType string

const (
	ValueTypeString  ValueType = "string"
	ValueTypeNumber  ValueType = "number"
	ValueTypeBoolean ValueType = "boolean"
	ValueTypeJSON    ValueType = "json"
)

// JoinField describes a read-only inverse relationship.
type JoinField struct {
	CollectionID   StableID       `json:"collectionId"`
	CollectionSlug CollectionSlug `json:"collectionSlug"`
	On             query.Path     `json:"on"`
	Limit          int            `json:"limit"`
	DefaultColumns []string       `json:"defaultColumns,omitempty"`
	DefaultSort    string         `json:"defaultSort,omitempty"`
	AllowCreate    *bool          `json:"allowCreate,omitempty"`
}

// VirtualField describes the serialized type of an executable computed value.
type VirtualField struct {
	ValueType ValueType `json:"valueType"`
}

// SelectField contains the finite options for a select field.
type SelectField struct {
	Options       []SelectOption `json:"options"`
	HasMany       bool           `json:"hasMany,omitempty"`
	DefaultValues []string       `json:"defaultValues,omitempty"`
}

// SelectOption is one normalized select value and label.
type SelectOption struct {
	Value             string            `json:"value"`
	Label             string            `json:"label"`
	LabelTranslations map[string]string `json:"labelTranslations,omitempty"`
}

// RelationshipField links to a resolved collection by stable ID and current slug.
type RelationshipField struct {
	CollectionID   StableID              `json:"collectionId,omitempty"`
	CollectionSlug CollectionSlug        `json:"collectionSlug,omitempty"`
	Targets        []RelationshipTarget  `json:"targets,omitempty"`
	HasMany        bool                  `json:"hasMany,omitempty"`
	Polymorphic    bool                  `json:"polymorphic,omitempty"`
	OptionFilters  []RelationshipFilter  `json:"optionFilters,omitempty"`
	OnDelete       ReferenceDeleteAction `json:"onDelete"`
}

// RelationshipFilter derives an admin option predicate from document data.
type RelationshipFilter struct {
	CollectionSlug CollectionSlug           `json:"collectionSlug,omitempty"`
	TargetPath     query.Path               `json:"targetPath"`
	Operator       string                   `json:"operator,omitempty"`
	SourcePath     *query.Path              `json:"sourcePath,omitempty"`
	Value          *RelationshipFilterValue `json:"value,omitempty"`
}

// RelationshipFilterValue is one typed static scalar operand.
type RelationshipFilterValue struct {
	Type  ValueType `json:"type"`
	Value string    `json:"value"`
}

type RelationshipTarget struct {
	CollectionID   StableID       `json:"collectionId"`
	CollectionSlug CollectionSlug `json:"collectionSlug"`
}

// UploadField references exactly one upload-enabled collection.
type UploadField struct {
	CollectionID   StableID              `json:"collectionId"`
	CollectionSlug CollectionSlug        `json:"collectionSlug"`
	HasMany        bool                  `json:"hasMany,omitempty"`
	OptionFilters  []RelationshipFilter  `json:"optionFilters,omitempty"`
	OnDelete       ReferenceDeleteAction `json:"onDelete"`
}

// ReferenceDeleteAction is the resolved current-document policy applied when
// a relationship or upload target is hard deleted.
type ReferenceDeleteAction string

const (
	ReferenceDeleteNullify  ReferenceDeleteAction = "nullify"
	ReferenceDeleteRestrict ReferenceDeleteAction = "restrict"
)

// NestedField contains recursively resolved child fields.
type NestedField struct {
	bound             *boundFields
	Fields            []Field              `json:"fields"`
	MinRows           int                  `json:"minRows,omitempty"`
	MaxRows           int                  `json:"maxRows,omitempty"`
	RowLabel          string               `json:"rowLabel,omitempty"`
	RowLabelComponent *FieldAdminComponent `json:"rowLabelComponent,omitempty"`
	RowLabels         *ArrayRowLabels      `json:"rowLabels,omitempty"`
}

// ArrayRowLabels contains singular and plural author-facing array item names.
type ArrayRowLabels struct {
	Singular             string            `json:"singular"`
	SingularTranslations map[string]string `json:"singularTranslations,omitempty"`
	Plural               string            `json:"plural"`
	PluralTranslations   map[string]string `json:"pluralTranslations,omitempty"`
}

type BlocksField struct {
	BlockReferences []string `json:"blockReferences,omitempty"`
	bound           *boundBlocks
	MinRows         int         `json:"minRows,omitempty"`
	MaxRows         int         `json:"maxRows,omitempty"`
	Types           []BlockType `json:"types,omitempty"`
}

// BlockAdmin contains the narrow content-derived block heading contract.
type BlockAdmin struct {
	RowLabel string `json:"rowLabel,omitempty"`
}

// BlockLabels contains resolved author-facing names and admin language overrides.
type BlockLabels struct {
	Singular             string            `json:"singular"`
	Plural               string            `json:"plural"`
	SingularTranslations map[string]string `json:"singularTranslations,omitempty"`
	PluralTranslations   map[string]string `json:"pluralTranslations,omitempty"`
}

type BlockType struct {
	bound    *boundFields
	TypeName string      `json:"typeName,omitempty"`
	Admin    *BlockAdmin `json:"admin,omitempty"`
	Slug     string      `json:"slug"`
	Labels   BlockLabels `json:"labels"`
	Fields   []Field     `json:"fields"`
}

type PluginField struct {
	EmbeddedTrees []EmbeddedTree  `json:"embeddedTrees,omitempty"`
	Key           string          `json:"key"`
	Config        json.RawMessage `json:"config"`
	ReferenceKeys []string        `json:"referenceKeys,omitempty"`
}

// Manifest owns a deeply copied snapshot. Its state cannot be changed through
// any public accessor.
type Manifest struct {
	snapshot Snapshot
}

// NewManifest freezes an already validated manifest snapshot.
func NewManifest(snapshot Snapshot) Manifest {
	return Manifest{snapshot: cloneSnapshot(snapshot)}
}

// Parse decodes a manifest, rejects unknown properties, and verifies that its
// format version is supported by this schema package.
func Parse(encoded []byte) (Manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()

	var snapshot Snapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return Manifest{}, fmt.Errorf("decode schema manifest: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return Manifest{}, err
	}
	if snapshot.Version != CurrentVersion {
		return Manifest{}, fmt.Errorf("unsupported schema manifest version %d; this Ridu build supports version %d", snapshot.Version, CurrentVersion)
	}
	if err := BindBlockReferences(&snapshot); err != nil {
		return Manifest{}, err
	}
	if err := ValidateEmbeddedMetadata(snapshot); err != nil {
		return Manifest{}, err
	}
	if err := validatePluginBuildMetadata(snapshot.Plugins); err != nil {
		return Manifest{}, err
	}
	if err := validateAdminFieldComponents(snapshot); err != nil {
		return Manifest{}, err
	}
	if err := validatePrimitiveListMetadata(snapshot); err != nil {
		return Manifest{}, err
	}
	if err := validateSelectMetadata(snapshot); err != nil {
		return Manifest{}, err
	}
	if err := validateFieldConditionMetadata(snapshot); err != nil {
		return Manifest{}, err
	}
	if err := validateSlugMetadata(snapshot); err != nil {
		return Manifest{}, err
	}
	if err := validateDocumentLockMetadata(snapshot.Collections); err != nil {
		return Manifest{}, err
	}
	if err := validateLocalizationMetadata(snapshot); err != nil {
		return Manifest{}, err
	}
	if err := validateAdminLocalizationMetadata(snapshot); err != nil {
		return Manifest{}, err
	}
	if err := validateAdminFieldMetadata(snapshot); err != nil {
		return Manifest{}, err
	}
	if err := validateAdminDisplayTranslations(snapshot); err != nil {
		return Manifest{}, err
	}
	if err := validateUniqueMetadata(snapshot); err != nil {
		return Manifest{}, err
	}
	if err := validateReferenceFilterMetadata(snapshot); err != nil {
		return Manifest{}, err
	}
	if err := validateReferenceDeleteMetadata(snapshot); err != nil {
		return Manifest{}, err
	}
	if err := validateConstraintAndIndexMetadata(snapshot); err != nil {
		return Manifest{}, err
	}
	if err := validateEndpointMetadata(snapshot); err != nil {
		return Manifest{}, err
	}
	return NewManifest(snapshot), nil
}

func validateSelectMetadata(snapshot Snapshot) error {
	validateFields := func(fields []Field, fieldsPath string) error {
		var inspect func([]Field, string) error
		inspect = func(candidates []Field, path string) error {
			for index, candidate := range candidates {
				fieldPath := fmt.Sprintf("%s[%d]", path, index)
				isSelect := candidate.Type == FieldTypeSelect || candidate.Type == FieldTypeRadio
				switch {
				case isSelect && (candidate.Category != FieldCategoryScalar || candidate.Select == nil):
					return fmt.Errorf("invalid select metadata at %s.select: select and radio fields require scalar select details", fieldPath)
				case !isSelect && candidate.Select != nil:
					return fmt.Errorf("invalid select metadata at %s.select: field type %q cannot declare select details", fieldPath, candidate.Type)
				}
				if candidate.Select != nil {
					metadata := candidate.Select
					if len(metadata.Options) == 0 {
						return fmt.Errorf("missing select options at %s.select.options", fieldPath)
					}
					seenOptions := make(map[string]struct{}, len(metadata.Options))
					for optionIndex, option := range metadata.Options {
						optionPath := fmt.Sprintf("%s.select.options[%d]", fieldPath, optionIndex)
						if option.Value == "" {
							return fmt.Errorf("missing select option value at %s.value", optionPath)
						}
						if _, duplicate := seenOptions[option.Value]; duplicate {
							return fmt.Errorf("duplicate select option value %q at %s.value", option.Value, optionPath)
						}
						seenOptions[option.Value] = struct{}{}
						if option.Label == "" || option.Label != strings.TrimSpace(option.Label) {
							return fmt.Errorf("invalid canonical select option label at %s.label", optionPath)
						}
					}
					if candidate.Type == FieldTypeRadio && metadata.HasMany {
						return fmt.Errorf("invalid radio cardinality at %s.select.hasMany", fieldPath)
					}
					if metadata.HasMany {
						if candidate.Default != nil {
							return fmt.Errorf("invalid multi-select default at %s.default: use select.defaultValues", fieldPath)
						}
						seenDefaults := make(map[string]struct{}, len(metadata.DefaultValues))
						for defaultIndex, value := range metadata.DefaultValues {
							defaultPath := fmt.Sprintf("%s.select.defaultValues[%d]", fieldPath, defaultIndex)
							if _, valid := seenOptions[value]; !valid {
								return fmt.Errorf("invalid multi-select default %q at %s", value, defaultPath)
							}
							if _, duplicate := seenDefaults[value]; duplicate {
								return fmt.Errorf("duplicate multi-select default %q at %s", value, defaultPath)
							}
							seenDefaults[value] = struct{}{}
						}
					} else {
						if len(metadata.DefaultValues) != 0 {
							return fmt.Errorf("invalid scalar select defaults at %s.select.defaultValues", fieldPath)
						}
						if candidate.Default != nil {
							if _, valid := seenOptions[*candidate.Default]; !valid {
								return fmt.Errorf("invalid select default %q at %s.default", *candidate.Default, fieldPath)
							}
						}
					}
				}
				if err := inspect(EmbeddedBlocks(candidate), fieldPath+".plugin.embeddedTrees"); err != nil {
					return err
				}
				if candidate.Nested != nil {
					if err := inspect(candidate.Nested.ResolvedFields(), fieldPath+".nested.fields"); err != nil {
						return err
					}
				}
				if candidate.Blocks != nil {
					for blockIndex, block := range candidate.Blocks.ResolvedTypes() {
						if block.TypeName != "" && !IsValidBlockTypeName(block.TypeName) {
							return fmt.Errorf("invalid block type name at %s.blocks.types[%d].typeName", fieldPath, blockIndex)
						}

						if err := inspect(block.ResolvedFields(), fmt.Sprintf("%s.blocks.types[%d].fields", fieldPath, blockIndex)); err != nil {
							return err
						}
					}
				}
			}
			return nil
		}
		return inspect(fields, fieldsPath)
	}
	for index, collection := range snapshot.Collections {
		if err := validateFields(collection.Fields, fmt.Sprintf("collections[%d].fields", index)); err != nil {
			return err
		}
	}
	for index, global := range snapshot.Globals {
		if err := validateFields(global.Fields, fmt.Sprintf("globals[%d].fields", index)); err != nil {
			return err
		}
	}
	return nil
}

func validateAdminFieldMetadata(snapshot Snapshot) error {
	var validateFields func([]Field, string, bool) error
	validateFields = func(fields []Field, path string, nested bool) error {
		for fieldIndex, candidate := range fields {
			fieldPath := fmt.Sprintf("%s[%d]", path, fieldIndex)
			if nested && candidate.Admin.Sidebar {
				return fmt.Errorf("invalid sidebar placement at %s.admin.sidebar: sidebar fields must be resource-root fields", fieldPath)
			}
			placeholder := candidate.Admin.Placeholder
			if placeholder != strings.TrimSpace(placeholder) || placeholder == "" && len(candidate.Admin.PlaceholderTranslations) != 0 {
				return fmt.Errorf("invalid canonical placeholder at %s.admin.placeholder", fieldPath)
			}
			if placeholder != "" {
				switch candidate.Type {
				case FieldTypeText, FieldTypeTextList, FieldTypeNumberList, FieldTypeTextarea, FieldTypeEmail, FieldTypeNumber, FieldTypeSelect, FieldTypeRelationship, FieldTypeUpload:
				default:
					return fmt.Errorf("unsupported placeholder at %s.admin.placeholder for field type %q", fieldPath, candidate.Type)
				}
			}
			if err := validateFields(EmbeddedBlocks(candidate), fieldPath+".plugin.embeddedTrees", true); err != nil {
				return err
			}
			if candidate.Nested != nil {
				if err := validateFields(candidate.Nested.ResolvedFields(), fieldPath+".nested.fields", true); err != nil {
					return err
				}
			}
			if candidate.Blocks != nil {
				for blockIndex, block := range candidate.Blocks.ResolvedTypes() {
					if err := validateFields(block.ResolvedFields(), fmt.Sprintf("%s.blocks.types[%d].fields", fieldPath, blockIndex), true); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	for index, collection := range snapshot.Collections {
		if err := validateFields(collection.Fields, fmt.Sprintf("collections[%d].fields", index), false); err != nil {
			return err
		}
	}
	for index, global := range snapshot.Globals {
		if err := validateFields(global.Fields, fmt.Sprintf("globals[%d].fields", index), false); err != nil {
			return err
		}
	}
	return nil
}

func validateEndpointMetadata(snapshot Snapshot) error {
	validate := func(endpoints []Endpoint, path string) error {
		seen := make(map[string]struct{}, len(endpoints))
		shapes := make(map[string]string, len(endpoints))
		for index, endpoint := range endpoints {
			shape := endpointPathShape(endpoint.Path)
			identity := endpoint.Method + " " + shape
			if endpoint.Method != strings.ToUpper(strings.TrimSpace(endpoint.Method)) || !IsSupportedEndpointMethod(endpoint.Method) || !IsValidEndpointPath(endpoint.Path) || endpoint.Summary != strings.TrimSpace(endpoint.Summary) {
				return fmt.Errorf("invalid custom endpoint at %s[%d]", path, index)
			}
			if _, duplicate := seen[identity]; duplicate {
				return fmt.Errorf("duplicate custom endpoint %q at %s[%d]", identity, path, index)
			}
			if previousPath, exists := shapes[shape]; exists && previousPath != endpoint.Path {
				return fmt.Errorf("conflicting custom endpoint parameter names at %s[%d]", path, index)
			}
			seen[identity] = struct{}{}
			shapes[shape] = endpoint.Path
		}
		return nil
	}
	if err := validate(snapshot.Application.Endpoints, "application.endpoints"); err != nil {
		return err
	}
	for index, endpoint := range snapshot.Application.Endpoints {
		firstSegment, _, _ := strings.Cut(strings.TrimPrefix(endpoint.Path, "/"), "/")
		if strings.HasPrefix(firstSegment, ":") || strings.HasPrefix(endpoint.Path, "/collections/") || strings.HasPrefix(endpoint.Path, "/globals/") {
			return fmt.Errorf("custom endpoint enters a reserved resource namespace at application.endpoints[%d]", index)
		}
	}
	for index, collection := range snapshot.Collections {
		if err := validate(collection.Endpoints, fmt.Sprintf("collections[%d].endpoints", index)); err != nil {
			return err
		}
	}
	for index, global := range snapshot.Globals {
		if err := validate(global.Endpoints, fmt.Sprintf("globals[%d].endpoints", index)); err != nil {
			return err
		}
	}
	return nil
}

func validateConstraintAndIndexMetadata(snapshot Snapshot) error {
	validateFields := func(fields []Field, fieldsPath string) error {
		var inspect func([]Field, string, bool) error
		inspect = func(candidates []Field, path string, repeated bool) error {
			for index, candidate := range candidates {
				fieldPath := fmt.Sprintf("%s[%d]", path, index)
				if candidate.Index && (repeated || !manifestSupportsIndexField(candidate)) {
					return fmt.Errorf("unsupported index field at %s.index", fieldPath)
				}
				if candidate.DynamicDefault {
					if candidate.Default != nil || candidate.Select != nil && len(candidate.Select.DefaultValues) != 0 {
						return fmt.Errorf("conflicting defaults at %s.dynamicDefault: choose a literal or dynamic default", fieldPath)
					}
					switch candidate.Type {
					case FieldTypeText, FieldTypeTextList, FieldTypeNumberList, FieldTypeCode, FieldTypeTextarea, FieldTypeEmail, FieldTypeDate, FieldTypeNumber, FieldTypeCheckbox, FieldTypeSelect, FieldTypeRadio:
					default:
						return fmt.Errorf("unsupported dynamic default at %s.dynamicDefault: field type %q does not support value defaults", fieldPath, candidate.Type)
					}
				}
				if candidate.LiveValidation && (candidate.Category == FieldCategoryPresentation || candidate.Type == FieldTypeUI || candidate.Type == FieldTypeJoin || candidate.Type == FieldTypeVirtual) {
					return fmt.Errorf("unsupported live validation at %s.liveValidation: field type %q has no writable value", fieldPath, candidate.Type)
				}
				var minLength, maxLength *int
				switch candidate.Type {
				case FieldTypeText, FieldTypeTextList:
					if candidate.Text != nil {
						minLength, maxLength = candidate.Text.MinLength, candidate.Text.MaxLength
					}
				case FieldTypeTextarea:
					if candidate.Textarea != nil {
						minLength, maxLength = candidate.Textarea.MinLength, candidate.Textarea.MaxLength
					}
				case FieldTypeCode:
					if candidate.Code != nil {
						minLength, maxLength = candidate.Code.MinLength, candidate.Code.MaxLength
					}
				default:
					if candidate.Textarea != nil || candidate.Text != nil && (candidate.Text.MinLength != nil || candidate.Text.MaxLength != nil) || candidate.Code != nil && (candidate.Code.MinLength != nil || candidate.Code.MaxLength != nil) {
						return fmt.Errorf("invalid length constraints at %s: field type %q does not support them", fieldPath, candidate.Type)
					}
				}
				if minLength != nil && *minLength < 0 || maxLength != nil && *maxLength < 0 || minLength != nil && maxLength != nil && *minLength > *maxLength {
					return fmt.Errorf("invalid length constraints at %s", fieldPath)
				}
				if candidate.Type != FieldTypeTextList && candidate.Default != nil && (minLength != nil || maxLength != nil) {
					length := utf8.RuneCountInString(*candidate.Default)
					if minLength != nil && length < *minLength || maxLength != nil && length > *maxLength {
						return fmt.Errorf("default violates length constraints at %s.default", fieldPath)
					}
				}
				if candidate.Number != nil {
					if (candidate.Type != FieldTypeNumber && candidate.Type != FieldTypeNumberList) || candidate.Number.Min != nil && (math.IsNaN(*candidate.Number.Min) || math.IsInf(*candidate.Number.Min, 0)) || candidate.Number.Max != nil && (math.IsNaN(*candidate.Number.Max) || math.IsInf(*candidate.Number.Max, 0)) || candidate.Number.Step != nil && (math.IsNaN(*candidate.Number.Step) || math.IsInf(*candidate.Number.Step, 0) || *candidate.Number.Step <= 0) || candidate.Number.Min != nil && candidate.Number.Max != nil && *candidate.Number.Min > *candidate.Number.Max {
						return fmt.Errorf("invalid number constraints at %s.number", fieldPath)
					}
					if candidate.Type != FieldTypeNumberList && candidate.Default != nil {
						value, err := strconv.ParseFloat(*candidate.Default, 64)
						if err != nil || candidate.Number.Min != nil && value < *candidate.Number.Min || candidate.Number.Max != nil && value > *candidate.Number.Max {
							return fmt.Errorf("default violates number constraints at %s.default", fieldPath)
						}
					}
				}
				if err := inspect(EmbeddedBlocks(candidate), fieldPath+".plugin.embeddedTrees", true); err != nil {
					return err
				}
				if candidate.Nested != nil {
					if err := inspect(candidate.Nested.ResolvedFields(), fieldPath+".nested.fields", repeated || candidate.Type == FieldTypeArray); err != nil {
						return err
					}
				}
				if candidate.Blocks != nil {
					if candidate.Type != FieldTypeBlocks || candidate.Blocks.MinRows < 0 || candidate.Blocks.MaxRows < 0 || candidate.Blocks.MaxRows > 0 && candidate.Blocks.MinRows > candidate.Blocks.MaxRows {
						return fmt.Errorf("invalid block row bounds at %s.blocks", fieldPath)
					}

					for blockIndex, block := range candidate.Blocks.ResolvedTypes() {
						if block.Admin != nil && block.Admin.RowLabel != "" {
							found := false
							for _, child := range block.ResolvedFields() {
								if child.Name == block.Admin.RowLabel && child.Category == FieldCategoryScalar && child.Type != FieldTypeJSON && child.Type != FieldTypePoint && child.Type != FieldTypeTextList && child.Type != FieldTypeNumberList && (child.Select == nil || !child.Select.HasMany) {
									found = true
									break
								}
							}
							if !found {
								return fmt.Errorf("invalid block row label at %s.blocks.types[%d].admin.rowLabel", fieldPath, blockIndex)
							}
						}

						if err := inspect(block.ResolvedFields(), fmt.Sprintf("%s.blocks.types[%d].fields", fieldPath, blockIndex), true); err != nil {
							return err
						}
					}
				}
			}
			return nil
		}
		return inspect(fields, fieldsPath, false)
	}
	for collectionIndex, collection := range snapshot.Collections {
		prefix := fmt.Sprintf("collections[%d]", collectionIndex)
		if err := validateFields(collection.Fields, prefix+".fields"); err != nil {
			return err
		}
		seenIndexes := make(map[string]struct{}, len(collection.Indexes))
		for index, candidate := range collection.Indexes {
			indexPath := fmt.Sprintf("%s.indexes[%d]", prefix, index)
			if len(candidate.Fields) < 2 || len(candidate.Fields) > 32 {
				return fmt.Errorf("invalid compound index size at %s.fields", indexPath)
			}
			seenFields := make(map[string]struct{}, len(candidate.Fields))
			parts := make([]string, len(candidate.Fields))
			for fieldIndex, path := range candidate.Fields {
				canonical := path.String()
				if canonical == "" {
					return fmt.Errorf("empty index field at %s.fields[%d]", indexPath, fieldIndex)
				}
				if _, duplicate := seenFields[canonical]; duplicate {
					return fmt.Errorf("duplicate index field %q at %s.fields[%d]", canonical, indexPath, fieldIndex)
				}
				seenFields[canonical] = struct{}{}
				terminal := manifestFilterFieldByPath(collection.Fields, path.Segments())
				if terminal == nil || !manifestSupportsIndexField(*terminal) {
					return fmt.Errorf("unsupported compound index field %q at %s.fields[%d]", canonical, indexPath, fieldIndex)
				}
				parts[fieldIndex] = canonical
			}
			signature := strings.Join(parts, "\x00")
			if _, duplicate := seenIndexes[signature]; duplicate {
				return fmt.Errorf("duplicate compound index at %s.fields", indexPath)
			}
			seenIndexes[signature] = struct{}{}
		}
	}
	for globalIndex, global := range snapshot.Globals {
		if len(global.Indexes) != 0 {
			return fmt.Errorf("globals[%d] cannot declare collection indexes", globalIndex)
		}
		if err := validateFields(global.Fields, fmt.Sprintf("globals[%d].fields", globalIndex)); err != nil {
			return err
		}
	}
	return nil
}

func validateSlugMetadata(snapshot Snapshot) error {
	validateResource := func(fields []Field, fieldsPath string) error {
		var inspect func([]Field, string, bool) error
		inspect = func(candidates []Field, path string, nested bool) error {
			for index, candidate := range candidates {
				fieldPath := fmt.Sprintf("%s[%d]", path, index)
				if candidate.Text != nil && candidate.Text.Slug != nil {
					slug := candidate.Text.Slug
					sourcePath := slug.SourcePath.String()
					switch {
					case nested:
						return fmt.Errorf("invalid slug field at %s.text.slug: slug fields must be resource-root fields", fieldPath)
					case candidate.Type != FieldTypeText || candidate.Category != FieldCategoryScalar:
						return fmt.Errorf("invalid slug field at %s.text.slug: slug behavior requires scalar text storage", fieldPath)
					case !candidate.Required || !candidate.Unique || !candidate.Index:
						return fmt.Errorf("invalid slug constraints at %s: slug fields must be required, unique, and indexed", fieldPath)
					case candidate.Localized:
						return fmt.Errorf("invalid localized slug at %s.localized", fieldPath)
					case candidate.Default != nil:
						return fmt.Errorf("invalid slug default at %s.default", fieldPath)
					case candidate.DynamicDefault:
						return fmt.Errorf("invalid slug default at %s.dynamicDefault: slug initialization owns its default", fieldPath)
					case sourcePath == "" || sourcePath == candidate.Path.String():
						return fmt.Errorf("invalid slug source at %s.text.slug.sourcePath", fieldPath)
					}
					sourceChain := manifestSlugSourceChain(fields, slug.SourcePath.Segments())
					if len(sourceChain) == 0 || !manifestSlugSourceType(sourceChain[len(sourceChain)-1]) {
						return fmt.Errorf("invalid slug source %q at %s.text.slug.sourcePath: expected a string field through non-repeated groups", sourcePath, fieldPath)
					}
					for _, sourceField := range sourceChain {
						if sourceField.Localized {
							return fmt.Errorf("invalid localized slug source %q at %s.text.slug.sourcePath", sourcePath, fieldPath)
						}
					}
					source := sourceChain[len(sourceChain)-1]
					if source.Text != nil && source.Text.Slug != nil {
						return fmt.Errorf("invalid slug source %q at %s.text.slug.sourcePath: slug fields cannot derive from other slug fields", sourcePath, fieldPath)
					}
				}
				if err := inspect(EmbeddedBlocks(candidate), fieldPath+".plugin.embeddedTrees", true); err != nil {
					return err
				}
				if candidate.Nested != nil {
					if err := inspect(candidate.Nested.ResolvedFields(), fieldPath+".nested.fields", true); err != nil {
						return err
					}
				}
				if candidate.Blocks != nil {
					for blockIndex, block := range candidate.Blocks.ResolvedTypes() {
						if err := inspect(block.ResolvedFields(), fmt.Sprintf("%s.blocks.types[%d].fields", fieldPath, blockIndex), true); err != nil {
							return err
						}
					}
				}
			}
			return nil
		}
		return inspect(fields, fieldsPath, false)
	}
	for index, collection := range snapshot.Collections {
		if err := validateResource(collection.Fields, fmt.Sprintf("collections[%d].fields", index)); err != nil {
			return err
		}
	}
	for index, global := range snapshot.Globals {
		if err := validateResource(global.Fields, fmt.Sprintf("globals[%d].fields", index)); err != nil {
			return err
		}
	}
	return nil
}

func manifestSlugSourceChain(fields []Field, segments []string) []Field {
	if len(segments) == 0 {
		return nil
	}
	for _, candidate := range fields {
		if candidate.Name != segments[0] {
			continue
		}
		chain := []Field{candidate}
		if len(segments) == 1 {
			return chain
		}
		if candidate.Type != FieldTypeGroup || candidate.Nested == nil {
			return nil
		}
		childChain := manifestSlugSourceChain(candidate.Nested.ResolvedFields(), segments[1:])
		if len(childChain) == 0 {
			return nil
		}
		return append(chain, childChain...)
	}
	return nil
}

func manifestSlugSourceType(candidate Field) bool {
	switch candidate.Type {
	case FieldTypeText, FieldTypeTextarea, FieldTypeCode, FieldTypeEmail:
		return candidate.Category == FieldCategoryScalar
	default:
		return false
	}
}

func manifestSupportsIndexField(candidate Field) bool {
	switch candidate.Type {
	case FieldTypeText, FieldTypeCode, FieldTypeTextarea, FieldTypeEmail, FieldTypeDate,
		FieldTypeNumber, FieldTypeCheckbox, FieldTypeRadio:
		return candidate.Category == FieldCategoryScalar || candidate.Category == FieldCategoryUpload
	case FieldTypeSelect:
		return (candidate.Category == FieldCategoryScalar || candidate.Category == FieldCategoryUpload) && candidate.Select != nil && !candidate.Select.HasMany
	case FieldTypeRelationship:
		return candidate.Category == FieldCategoryRelationship && candidate.Relationship != nil && !candidate.Relationship.HasMany && !candidate.Relationship.Polymorphic
	case FieldTypeUpload:
		return candidate.Category == FieldCategoryUpload && candidate.Upload != nil && !candidate.Upload.HasMany
	default:
		return false
	}
}

func validateReferenceDeleteMetadata(snapshot Snapshot) error {
	validate := func(fields []Field, fieldsPath string) error {
		var inspect func([]Field, string) error
		inspect = func(candidates []Field, path string) error {
			for index, candidate := range candidates {
				fieldPath := fmt.Sprintf("%s[%d]", path, index)
				var action ReferenceDeleteAction
				switch {
				case candidate.Relationship != nil:
					action = candidate.Relationship.OnDelete
				case candidate.Upload != nil:
					action = candidate.Upload.OnDelete
				}
				if candidate.Relationship != nil || candidate.Upload != nil {
					if action != ReferenceDeleteNullify && action != ReferenceDeleteRestrict {
						return fmt.Errorf("invalid reference delete action at %s.onDelete: expected nullify or restrict", fieldPath)
					}
					if candidate.Required && action == ReferenceDeleteNullify {
						return fmt.Errorf("invalid reference delete action at %s.onDelete: required references cannot be nullified", fieldPath)
					}
				}
				if err := inspect(EmbeddedBlocks(candidate), fieldPath+".plugin.embeddedTrees"); err != nil {
					return err
				}
				if candidate.Nested != nil {
					if err := inspect(candidate.Nested.ResolvedFields(), fieldPath+".nested.fields"); err != nil {
						return err
					}
				}
				if candidate.Blocks != nil {
					for blockIndex, block := range candidate.Blocks.ResolvedTypes() {
						if err := inspect(block.ResolvedFields(), fmt.Sprintf("%s.blocks.types[%d].fields", fieldPath, blockIndex)); err != nil {
							return err
						}
					}
				}
			}
			return nil
		}
		return inspect(fields, fieldsPath)
	}
	for index, collection := range snapshot.Collections {
		if err := validate(collection.Fields, fmt.Sprintf("collections[%d].fields", index)); err != nil {
			return err
		}
	}
	for index, global := range snapshot.Globals {
		if err := validate(global.Fields, fmt.Sprintf("globals[%d].fields", index)); err != nil {
			return err
		}
	}
	return nil
}

func validateReferenceFilterMetadata(snapshot Snapshot) error {
	collections := make(map[StableID]Collection, len(snapshot.Collections))
	for _, collection := range snapshot.Collections {
		collections[collection.ID] = collection
	}

	validateResource := func(resource Collection, prefix string) error {
		var inspect func([]Field, string) error
		inspect = func(fields []Field, fieldsPath string) error {
			for fieldIndex, candidate := range fields {
				fieldPath := fmt.Sprintf("%s[%d]", fieldsPath, fieldIndex)
				var rules []RelationshipFilter
				var targets []RelationshipTarget
				switch {
				case candidate.Relationship != nil:
					rules = candidate.Relationship.OptionFilters
					targets = append([]RelationshipTarget(nil), candidate.Relationship.Targets...)
					if !candidate.Relationship.Polymorphic {
						targets = []RelationshipTarget{{CollectionID: candidate.Relationship.CollectionID, CollectionSlug: candidate.Relationship.CollectionSlug}}
					}
				case candidate.Upload != nil:
					rules = candidate.Upload.OptionFilters
					targets = []RelationshipTarget{{CollectionID: candidate.Upload.CollectionID, CollectionSlug: candidate.Upload.CollectionSlug}}
				}

				validateFilter := func(filter RelationshipFilter, path string) error {
					hasSource := filter.SourcePath != nil
					hasValue := filter.Value != nil
					if hasSource == hasValue {
						return fmt.Errorf("invalid reference filter at %s: expected exactly one sourcePath or value", path)
					}
					var operandKind manifestReferenceFilterValueKind
					if filter.SourcePath != nil {
						sourceField := manifestFilterFieldByPath(resource.Fields, filter.SourcePath.Segments())
						if sourceField == nil {
							return fmt.Errorf("invalid reference filter at %s.sourcePath: source field %q does not exist or cannot be queried", path, filter.SourcePath)
						}
						operandKind = manifestReferenceFilterFieldKind(sourceField)
					} else {
						var valid bool
						operandKind, valid = manifestReferenceFilterLiteralKind(filter.Value)
						if !valid {
							return fmt.Errorf("invalid reference filter at %s.value: literal value is not a valid %q scalar", path, filter.Value.Type)
						}
					}
					if !validReferenceFilterOperator(filter.Operator) {
						return fmt.Errorf("invalid reference filter at %s.operator: unsupported operator %q", path, filter.Operator)
					}

					matched := false
					for _, targetReference := range targets {
						if filter.CollectionSlug != "" && filter.CollectionSlug != targetReference.CollectionSlug {
							continue
						}
						matched = true
						target, exists := collections[targetReference.CollectionID]
						if !exists || target.Slug != targetReference.CollectionSlug {
							return fmt.Errorf("invalid reference filter at %s: target collection %q is unavailable", path, targetReference.CollectionSlug)
						}
						targetField := manifestFilterFieldByPath(target.Fields, filter.TargetPath.Segments())
						targetKind := manifestReferenceFilterFieldKind(targetField)
						if filter.TargetPath.String() == "_status" && target.Versions != nil && target.Versions.Drafts {
							targetKind = manifestReferenceFilterString
						}
						if targetKind == manifestReferenceFilterInvalid {
							return fmt.Errorf("invalid reference filter at %s.targetPath: target field %q does not exist or cannot be queried in collection %q", path, filter.TargetPath, target.Slug)
						}
						operator := filter.Operator
						if !manifestReferenceFilterKindsCompatible(operandKind, targetKind, operator) {
							return fmt.Errorf("invalid reference filter at %s: operator %q requires compatible scalar operand and target types", path, operator)
						}
					}
					if !matched {
						return fmt.Errorf("invalid reference filter at %s.collectionSlug: reference does not target collection %q", path, filter.CollectionSlug)
					}
					return nil
				}

				for ruleIndex, rule := range rules {
					if err := validateFilter(rule, fmt.Sprintf("%s.optionFilters[%d]", fieldPath, ruleIndex)); err != nil {
						return err
					}
				}
				if err := inspect(EmbeddedBlocks(candidate), fieldPath+".plugin.embeddedTrees"); err != nil {
					return err
				}
				if candidate.Nested != nil {
					if err := inspect(candidate.Nested.ResolvedFields(), fieldPath+".nested.fields"); err != nil {
						return err
					}
				}
				if candidate.Blocks != nil {
					for blockIndex, block := range candidate.Blocks.ResolvedTypes() {
						if err := inspect(block.ResolvedFields(), fmt.Sprintf("%s.blocks.types[%d].fields", fieldPath, blockIndex)); err != nil {
							return err
						}
					}
				}
			}
			return nil
		}
		return inspect(resource.Fields, prefix+".fields")
	}

	for collectionIndex, collection := range snapshot.Collections {
		if err := validateResource(collection, fmt.Sprintf("collections[%d]", collectionIndex)); err != nil {
			return err
		}
	}
	for globalIndex, global := range snapshot.Globals {
		if err := validateResource(global, fmt.Sprintf("globals[%d]", globalIndex)); err != nil {
			return err
		}
	}
	return nil
}

func validReferenceFilterOperator(operator string) bool {
	switch operator {
	case "equals", "notEquals", "like", "contains", "greaterThan", "greaterThanEqual", "lessThan", "lessThanEqual":
		return true
	default:
		return false
	}
}

func manifestFilterFieldByPath(fields []Field, segments []string) *Field {
	if len(segments) == 0 {
		return nil
	}
	for index := range fields {
		candidate := &fields[index]
		if candidate.Name != segments[0] {
			continue
		}
		if len(segments) == 1 {
			if candidate.Category == FieldCategoryPresentation || candidate.Category == FieldCategoryNested || candidate.Category == FieldCategoryPlugin || candidate.Type == FieldTypeJSON || candidate.Type == FieldTypePoint {
				return nil
			}
			return candidate
		}
		if candidate.Type == FieldTypeGroup && candidate.Nested != nil {
			return manifestFilterFieldByPath(candidate.Nested.ResolvedFields(), segments[1:])
		}
	}
	return nil
}

type manifestReferenceFilterValueKind uint8

const (
	manifestReferenceFilterInvalid manifestReferenceFilterValueKind = iota
	manifestReferenceFilterString
	manifestReferenceFilterNumber
	manifestReferenceFilterBoolean
)

func manifestReferenceFilterFieldsCompatible(source, target *Field, operator string) bool {
	return manifestReferenceFilterKindsCompatible(manifestReferenceFilterFieldKind(source), manifestReferenceFilterFieldKind(target), operator)
}

func manifestReferenceFilterKindsCompatible(sourceKind, targetKind manifestReferenceFilterValueKind, operator string) bool {
	if sourceKind == manifestReferenceFilterInvalid || sourceKind != targetKind {
		return false
	}
	switch operator {
	case "equals", "notEquals":
		return true
	case "like", "contains":
		return sourceKind == manifestReferenceFilterString
	case "greaterThan", "greaterThanEqual", "lessThan", "lessThanEqual":
		return sourceKind == manifestReferenceFilterString || sourceKind == manifestReferenceFilterNumber
	default:
		return false
	}
}

func manifestReferenceFilterLiteralKind(value *RelationshipFilterValue) (manifestReferenceFilterValueKind, bool) {
	if value == nil {
		return manifestReferenceFilterInvalid, false
	}
	switch value.Type {
	case ValueTypeString:
		return manifestReferenceFilterString, true
	case ValueTypeNumber:
		number, err := strconv.ParseFloat(value.Value, 64)
		return manifestReferenceFilterNumber, err == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
	case ValueTypeBoolean:
		_, err := strconv.ParseBool(value.Value)
		return manifestReferenceFilterBoolean, err == nil
	default:
		return manifestReferenceFilterInvalid, false
	}
}

func manifestReferenceFilterFieldKind(candidate *Field) manifestReferenceFilterValueKind {
	if candidate == nil || candidate.Category == FieldCategoryPresentation || candidate.Category == FieldCategoryNested || candidate.Category == FieldCategoryPlugin {
		return manifestReferenceFilterInvalid
	}
	switch candidate.Type {
	case FieldTypeText, FieldTypeCode, FieldTypeTextarea, FieldTypeEmail, FieldTypeDate, FieldTypeRadio:
		return manifestReferenceFilterString
	case FieldTypeSelect:
		if candidate.Select != nil && !candidate.Select.HasMany {
			return manifestReferenceFilterString
		}
	case FieldTypeNumber:
		return manifestReferenceFilterNumber
	case FieldTypeCheckbox:
		return manifestReferenceFilterBoolean
	case FieldTypeRelationship:
		if candidate.Relationship != nil && !candidate.Relationship.HasMany && !candidate.Relationship.Polymorphic {
			return manifestReferenceFilterString
		}
	case FieldTypeUpload:
		if candidate.Upload != nil && !candidate.Upload.HasMany {
			return manifestReferenceFilterString
		}
	}
	return manifestReferenceFilterInvalid
}

func validateUniqueMetadata(snapshot Snapshot) error {
	validate := func(fields []Field, prefix string) error {
		var inspect func([]Field, string, bool) error
		inspect = func(candidates []Field, path string, nested bool) error {
			for fieldIndex, candidate := range candidates {
				fieldPath := fmt.Sprintf("%s[%d]", path, fieldIndex)
				if candidate.Unique {
					switch {
					case nested:
						return fmt.Errorf("unsupported unique field at %s.unique: nested fields are not enforced by Ridu stores", fieldPath)
					case candidate.Category == FieldCategoryPresentation || candidate.Type == FieldTypeUI || candidate.Type == FieldTypeJoin || candidate.Type == FieldTypeVirtual:
						return fmt.Errorf("unsupported unique field at %s.unique: output-only and presentation fields have no persisted value to constrain", fieldPath)
					case !supportsUniqueFieldType(candidate):
						return fmt.Errorf("unsupported unique field at %s.unique: field type %q does not have a supported scalar or reference uniqueness contract", fieldPath, candidate.Type)
					case candidate.Relationship != nil && candidate.Relationship.Polymorphic:
						return fmt.Errorf("unsupported unique field at %s.unique: polymorphic references are not enforced by Ridu stores", fieldPath)
					case candidate.Relationship != nil && candidate.Relationship.HasMany || candidate.Upload != nil && candidate.Upload.HasMany:
						return fmt.Errorf("unsupported unique field at %s.unique: list-valued references are not enforced by Ridu stores", fieldPath)
					}
				}
				if err := inspect(EmbeddedBlocks(candidate), fieldPath+".plugin.embeddedTrees", true); err != nil {
					return err
				}
				if candidate.Nested != nil {
					if err := inspect(candidate.Nested.ResolvedFields(), fieldPath+".nested.fields", true); err != nil {
						return err
					}
				}
				if candidate.Blocks != nil {
					for blockIndex, block := range candidate.Blocks.ResolvedTypes() {
						if err := inspect(block.ResolvedFields(), fmt.Sprintf("%s.blocks.types[%d].fields", fieldPath, blockIndex), true); err != nil {
							return err
						}
					}
				}
			}
			return nil
		}
		return inspect(fields, prefix, false)
	}
	for collectionIndex, collection := range snapshot.Collections {
		if err := validate(collection.Fields, fmt.Sprintf("collections[%d].fields", collectionIndex)); err != nil {
			return err
		}
	}
	for globalIndex, global := range snapshot.Globals {
		if err := validate(global.Fields, fmt.Sprintf("globals[%d].fields", globalIndex)); err != nil {
			return err
		}
	}
	return nil
}

// supportsUniqueFieldType mirrors the authoring-level field.Unique option
// contract. Keep this allowlist explicit: accepting an arbitrary persisted
// shape here can make official stores disagree about equality (for example,
// PostgreSQL can compare JSONB while the strict in-memory store deliberately
// has no object/list uniqueness semantics).
func supportsUniqueFieldType(candidate Field) bool {
	switch candidate.Type {
	case FieldTypeText, FieldTypeCode, FieldTypeTextarea, FieldTypeEmail, FieldTypeDate,
		FieldTypeNumber, FieldTypeCheckbox, FieldTypeRadio:
		return candidate.Category == FieldCategoryScalar
	case FieldTypeSelect:
		return candidate.Category == FieldCategoryScalar && candidate.Select != nil && !candidate.Select.HasMany
	case FieldTypeRelationship:
		return candidate.Category == FieldCategoryRelationship && candidate.Relationship != nil
	case FieldTypeUpload:
		return candidate.Category == FieldCategoryUpload && candidate.Upload != nil
	default:
		return false
	}
}

func validateLocalizationMetadata(snapshot Snapshot) error {
	settings := snapshot.Application.Localization
	if settings == nil {
		for collectionIndex, collection := range append(append([]Collection(nil), snapshot.Collections...), snapshot.Globals...) {
			if path, localized := localizedFieldPath(collection.Fields, ""); localized {
				return fmt.Errorf("localized field at resources[%d].fields.%s requires application localization settings", collectionIndex, path)
			}
		}
		return nil
	}
	if len(settings.Locales) == 0 {
		return fmt.Errorf("application localization requires at least one locale")
	}
	codes := make(map[LocaleCode]struct{}, len(settings.Locales))
	for index, locale := range settings.Locales {
		if !IsValidLocaleCode(string(locale.Code)) {
			return fmt.Errorf("invalid locale code at application.localization.locales[%d].code", index)
		}
		if strings.TrimSpace(locale.Label) == "" {
			return fmt.Errorf("missing locale label at application.localization.locales[%d].label", index)
		}
		if _, exists := codes[locale.Code]; exists {
			return fmt.Errorf("duplicate locale code %q", locale.Code)
		}
		codes[locale.Code] = struct{}{}
	}
	if _, exists := codes[settings.DefaultLocale]; !exists {
		return fmt.Errorf("default locale %q is not configured", settings.DefaultLocale)
	}
	for index, locale := range settings.Locales {
		seen := make(map[LocaleCode]struct{}, len(locale.FallbackLocales))
		for fallbackIndex, fallback := range locale.FallbackLocales {
			if _, exists := codes[fallback]; !exists {
				return fmt.Errorf("unknown fallback locale at application.localization.locales[%d].fallbackLocale[%d]", index, fallbackIndex)
			}
			if fallback == locale.Code {
				return fmt.Errorf("locale %q cannot fall back to itself", locale.Code)
			}
			if _, exists := seen[fallback]; exists {
				return fmt.Errorf("duplicate fallback locale %q for locale %q", fallback, locale.Code)
			}
			seen[fallback] = struct{}{}
		}
	}
	if cycle := localizationFallbackCycle(settings.Locales); len(cycle) != 0 {
		return fmt.Errorf("locale fallback cycle: %s", strings.Join(cycle, " -> "))
	}
	return nil
}

func validateAdminLocalizationMetadata(snapshot Snapshot) error {
	settings := snapshot.Application.AdminLocalization
	if settings == nil {
		return nil
	}
	if len(settings.Languages) == 0 {
		return fmt.Errorf("application admin localization requires at least one language")
	}
	languages := make(map[string]struct{}, len(settings.Languages))
	for index, language := range settings.Languages {
		if !validAdminLanguageCode(language.Code) {
			return fmt.Errorf("invalid admin language code at application.adminLocalization.languages[%d].code", index)
		}
		if strings.TrimSpace(language.Label) == "" {
			return fmt.Errorf("missing admin language label at application.adminLocalization.languages[%d].label", index)
		}
		if _, exists := languages[language.Code]; exists {
			return fmt.Errorf("duplicate admin language code %q", language.Code)
		}
		languages[language.Code] = struct{}{}
	}
	if _, exists := languages[settings.DefaultLanguage]; !exists {
		return fmt.Errorf("default admin language %q is not configured", settings.DefaultLanguage)
	}
	timeZones := make(map[string]struct{}, len(settings.TimeZones))
	for index, timeZone := range settings.TimeZones {
		if !validAdminTimeZoneID(timeZone.ID) {
			return fmt.Errorf("invalid admin timezone at application.adminLocalization.timeZones[%d].id", index)
		}
		if strings.TrimSpace(timeZone.Label) == "" {
			return fmt.Errorf("missing admin timezone label at application.adminLocalization.timeZones[%d].label", index)
		}
		if _, exists := timeZones[timeZone.ID]; exists {
			return fmt.Errorf("duplicate admin timezone %q", timeZone.ID)
		}
		timeZones[timeZone.ID] = struct{}{}
	}
	if settings.DefaultTimeZone != "" {
		if _, exists := timeZones[settings.DefaultTimeZone]; !exists {
			return fmt.Errorf("default admin timezone %q is not configured", settings.DefaultTimeZone)
		}
	}
	return nil
}

func validateAdminDisplayTranslations(snapshot Snapshot) error {
	languages := make(map[string]struct{})
	if snapshot.Application.AdminLocalization != nil {
		for _, language := range snapshot.Application.AdminLocalization.Languages {
			languages[language.Code] = struct{}{}
		}
	}
	validate := func(translations map[string]string, path string) error {
		keys := make([]string, 0, len(translations))
		for language := range translations {
			keys = append(keys, language)
		}
		sort.Strings(keys)
		for _, language := range keys {
			if _, configured := languages[language]; !configured {
				return fmt.Errorf("unknown admin translation language %q at %s[%q]", language, path, language)
			}
			if strings.TrimSpace(translations[language]) == "" {
				return fmt.Errorf("blank admin translation value at %s[%q]", path, language)
			}
		}
		return nil
	}
	if err := validate(snapshot.Application.NameTranslations, "application.nameTranslations"); err != nil {
		return err
	}
	if snapshot.Application.AdminLocalization != nil {
		for index, language := range snapshot.Application.AdminLocalization.Languages {
			if err := validate(language.LabelTranslations, fmt.Sprintf("application.adminLocalization.languages[%d].labelTranslations", index)); err != nil {
				return err
			}
		}
		for index, timeZone := range snapshot.Application.AdminLocalization.TimeZones {
			if err := validate(timeZone.LabelTranslations, fmt.Sprintf("application.adminLocalization.timeZones[%d].labelTranslations", index)); err != nil {
				return err
			}
		}
	}
	var validateFields func([]Field, string) error
	validateFields = func(fields []Field, path string) error {
		for fieldIndex, candidate := range fields {
			fieldPath := fmt.Sprintf("%s[%d]", path, fieldIndex)
			for _, value := range []struct {
				translations map[string]string
				path         string
			}{
				{candidate.Admin.LabelTranslations, fieldPath + ".admin.labelTranslations"},
				{candidate.Admin.DescriptionTranslations, fieldPath + ".admin.descriptionTranslations"},
				{candidate.Admin.PlaceholderTranslations, fieldPath + ".admin.placeholderTranslations"},
				{candidate.Admin.TabTranslations, fieldPath + ".admin.tabTranslations"},
			} {
				if err := validate(value.translations, value.path); err != nil {
					return err
				}
			}
			if candidate.Admin.Collapsible != nil {
				if err := validate(candidate.Admin.Collapsible.LabelTranslations, fieldPath+".admin.collapsible.labelTranslations"); err != nil {
					return err
				}
			}
			if candidate.Select != nil {
				for optionIndex, option := range candidate.Select.Options {
					if err := validate(option.LabelTranslations, fmt.Sprintf("%s.select.options[%d].labelTranslations", fieldPath, optionIndex)); err != nil {
						return err
					}
				}
			}
			if err := validateFields(EmbeddedBlocks(candidate), fieldPath+".plugin.embeddedTrees"); err != nil {
				return err
			}
			if candidate.Nested != nil {
				if candidate.Nested.RowLabels != nil {
					if err := validate(candidate.Nested.RowLabels.SingularTranslations, fieldPath+".nested.rowLabels.singularTranslations"); err != nil {
						return err
					}
					if err := validate(candidate.Nested.RowLabels.PluralTranslations, fieldPath+".nested.rowLabels.pluralTranslations"); err != nil {
						return err
					}
				}
				if err := validateFields(candidate.Nested.ResolvedFields(), fieldPath+".nested.fields"); err != nil {
					return err
				}
			}
			if candidate.Blocks != nil {
				for blockIndex, block := range candidate.Blocks.ResolvedTypes() {
					blockPath := fmt.Sprintf("%s.blocks.types[%d]", fieldPath, blockIndex)
					if err := validate(block.Labels.SingularTranslations, blockPath+".labels.singularTranslations"); err != nil {
						return err
					}
					if err := validate(block.Labels.PluralTranslations, blockPath+".labels.pluralTranslations"); err != nil {
						return err
					}
					if err := validateFields(block.ResolvedFields(), blockPath+".fields"); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	for index, block := range snapshot.Blocks {
		path := fmt.Sprintf("blocks[%d]", index)
		if err := validate(block.Labels.SingularTranslations, path+".labels.singularTranslations"); err != nil {
			return err
		}
		if err := validate(block.Labels.PluralTranslations, path+".labels.pluralTranslations"); err != nil {
			return err
		}
		if err := validateFields(block.ResolvedFields(), path+".fields"); err != nil {
			return err
		}
	}
	resources := append(append([]Collection(nil), snapshot.Collections...), snapshot.Globals...)
	for resourceIndex, resource := range resources {
		path := fmt.Sprintf("resources[%d]", resourceIndex)
		for _, value := range []struct {
			translations map[string]string
			path         string
		}{
			{resource.Labels.SingularTranslations, path + ".labels.singularTranslations"},
			{resource.Labels.PluralTranslations, path + ".labels.pluralTranslations"},
			{resource.Admin.GroupTranslations, path + ".admin.groupTranslations"},
			{resource.Admin.DescriptionTranslations, path + ".admin.descriptionTranslations"},
		} {
			if err := validate(value.translations, value.path); err != nil {
				return err
			}
		}
		if resource.Admin.LivePreview != nil {
			for breakpointIndex, breakpoint := range resource.Admin.LivePreview.Breakpoints {
				if err := validate(breakpoint.LabelTranslations, fmt.Sprintf("%s.admin.livePreview.breakpoints[%d].labelTranslations", path, breakpointIndex)); err != nil {
					return err
				}
			}
		}
		if err := validateFields(resource.Fields, path+".fields"); err != nil {
			return err
		}
	}
	return nil
}

func validAdminTimeZoneID(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 128 {
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
	if strings.Contains(value, "..") || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") {
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

func validAdminLanguageCode(value string) bool {
	if value == "" || value != strings.TrimSpace(value) {
		return false
	}
	tag, err := language.Parse(value)
	return err == nil && tag != language.Und && tag.String() == value
}

func localizationFallbackCycle(locales []Locale) []string {
	edges := make(map[LocaleCode][]LocaleCode, len(locales))
	for _, locale := range locales {
		edges[locale.Code] = locale.FallbackLocales
	}
	state := make(map[LocaleCode]uint8, len(locales))
	var stack []LocaleCode
	var visit func(LocaleCode) []string
	visit = func(code LocaleCode) []string {
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

func localizedFieldPath(fields []Field, prefix string) (string, bool) {
	for _, field := range fields {
		path := field.Name
		if prefix != "" {
			path = prefix + "." + path
		}
		if field.Localized {
			return path, true
		}
		if nested, found := localizedFieldPath(EmbeddedBlocks(field), path); found {
			return nested, true
		}
		if field.Nested != nil {
			if nested, found := localizedFieldPath(field.Nested.ResolvedFields(), path); found {
				return nested, true
			}
		}
		if field.Blocks != nil {
			for _, block := range field.Blocks.ResolvedTypes() {
				if nested, found := localizedFieldPath(block.ResolvedFields(), path+"."+block.Slug); found {
					return nested, true
				}
			}
		}
	}
	return "", false
}

func validateDocumentLockMetadata(collections []Collection) error {
	for index, collection := range collections {
		if collection.DocumentLock == nil {
			if collection.Capabilities.Locking {
				return fmt.Errorf("collection at collections[%d] enables locking without document lock settings", index)
			}
			continue
		}
		if !collection.Capabilities.Locking {
			return fmt.Errorf("collection at collections[%d] has document lock settings without locking capability", index)
		}
		if collection.DocumentLock.DurationSeconds < 10 {
			return fmt.Errorf("invalid document lock duration at collections[%d].documentLockSettings.durationSeconds", index)
		}
	}
	return nil
}

func validatePluginBuildMetadata(plugins []Plugin) error {
	fieldOwners := make(map[string]string)
	keys := make(map[string]struct{}, len(plugins))
	targets := make(map[string]struct{}, len(plugins))
	adminRoutes := make(map[string]struct{})
	for index, plugin := range plugins {
		if !IsValidPluginKey(plugin.Key) {
			return fmt.Errorf("invalid schema plugin key at plugins[%d].key", index)
		}
		if _, exists := keys[plugin.Key]; exists {
			return fmt.Errorf("duplicate schema plugin key %q", plugin.Key)
		}
		keys[plugin.Key] = struct{}{}
		hasDescriptor := plugin.Version != "" || plugin.GoPackage != "" || plugin.APIVersion != 0 || plugin.Ridu != nil || len(plugin.FieldTypes) != 0 || len(plugin.DatabaseContributions) != 0 || plugin.Admin != nil || len(plugin.Endpoints) != 0
		if !hasDescriptor {
			if owner, exists := fieldOwners[plugin.Key]; exists {
				return fmt.Errorf("plugin field type %q is already owned by %s", plugin.Key, owner)
			}
			fieldOwners[plugin.Key] = plugin.Key
		}
		if hasDescriptor {
			if !IsValidSemanticVersion(plugin.Version) || !IsValidGoPackage(plugin.GoPackage) || plugin.APIVersion != CurrentPluginAPIVersion || plugin.Ridu == nil || !IsValidSemanticVersionRange(plugin.Ridu.Minimum, plugin.Ridu.MaximumExclusive) {
				return fmt.Errorf("invalid versioned plugin descriptor at plugins[%d]", index)
			}
			fieldKeys := make(map[string]struct{}, len(plugin.FieldTypes))
			for fieldIndex, fieldType := range plugin.FieldTypes {
				if !IsValidPluginKey(fieldType.Key) || !IsValidAdminPluginPackage(fieldType.TypeScriptPackage) || !IsValidAdminPluginExport(fieldType.TypeScriptOutput) || !IsValidAdminPluginExport(fieldType.TypeScriptInput) || fieldType.TypeScriptWhere != "" && !IsValidAdminPluginExport(fieldType.TypeScriptWhere) || (fieldType.GoPackage == "") != (fieldType.GoType == "") || fieldType.GoPackage != "" && (!IsValidGoPackage(fieldType.GoPackage) || !IsValidAdminPluginExport(fieldType.GoType)) || len(fieldType.JSONSchema) != 0 && !json.Valid(fieldType.JSONSchema) {
					return fmt.Errorf("invalid plugin field type at plugins[%d].fieldTypes[%d]", index, fieldIndex)
				}
				if _, exists := fieldKeys[fieldType.Key]; exists {
					return fmt.Errorf("duplicate plugin field type %q", fieldType.Key)
				}
				fieldKeys[fieldType.Key] = struct{}{}
				if owner, exists := fieldOwners[fieldType.Key]; exists {
					return fmt.Errorf("plugin field type %q is already owned by %s", fieldType.Key, owner)
				}
				fieldOwners[fieldType.Key] = plugin.Key
			}
			prefix := "ridu_plugin_" + strings.ReplaceAll(plugin.Key, "-", "_") + "_"
			adapters := make(map[PluginDatabaseAdapter]struct{}, len(plugin.DatabaseContributions))
			for contributionIndex, contribution := range plugin.DatabaseContributions {
				if contribution.Adapter != PluginDatabaseAdapterPostgres && contribution.Adapter != PluginDatabaseAdapterSQLite {
					return fmt.Errorf("invalid plugin database adapter at plugins[%d].databaseContributions[%d].adapter", index, contributionIndex)
				}
				if _, duplicate := adapters[contribution.Adapter]; duplicate {
					return fmt.Errorf("duplicate plugin database adapter %q", contribution.Adapter)
				}
				adapters[contribution.Adapter] = struct{}{}
				if len(contribution.Migrations) == 0 && len(contribution.Tables) == 0 {
					return fmt.Errorf("empty plugin database contribution at plugins[%d].databaseContributions[%d]", index, contributionIndex)
				}
				if len(contribution.Tables) != 0 && len(contribution.Migrations) == 0 {
					return fmt.Errorf("plugin database tables require migrations at plugins[%d].databaseContributions[%d]", index, contributionIndex)
				}
				for migrationIndex, pluginMigration := range contribution.Migrations {
					if pluginMigration.Version != uint32(migrationIndex+1) || !IsValidPluginKey(pluginMigration.Name) || len(pluginMigration.UpSQL) == 0 || len(pluginMigration.DownSQL) == 0 {
						return fmt.Errorf("invalid plugin migration at plugins[%d].databaseContributions[%d].migrations[%d]", index, contributionIndex, migrationIndex)
					}
					for _, statement := range append(append([]string(nil), pluginMigration.UpSQL...), pluginMigration.DownSQL...) {
						if !IsValidPluginMigrationSQL(contribution.Adapter, statement) {
							return fmt.Errorf("invalid plugin migration SQL at plugins[%d].databaseContributions[%d].migrations[%d]", index, contributionIndex, migrationIndex)
						}
					}
				}
				tables := make(map[string]struct{}, len(contribution.Tables))
				for tableIndex, table := range contribution.Tables {
					if !IsValidPluginTable(table) || !strings.HasPrefix(table, prefix) {
						return fmt.Errorf("invalid plugin database table at plugins[%d].databaseContributions[%d].tables[%d]", index, contributionIndex, tableIndex)
					}
					if _, duplicate := tables[table]; duplicate {
						return fmt.Errorf("duplicate plugin database table %q", table)
					}
					tables[table] = struct{}{}
				}
			}
		}
		if plugin.Admin == nil {
			// Backend-only plugins may still contribute endpoints.
		} else {
			if !IsValidAdminPluginPackage(plugin.Admin.Package) {
				return fmt.Errorf("invalid admin plugin package at plugins[%d].admin.package", index)
			}
			if !IsValidAdminPluginExport(plugin.Admin.Export) {
				return fmt.Errorf("invalid admin plugin export at plugins[%d].admin.export", index)
			}
			if plugin.Admin.APIVersion != CurrentAdminPluginAPIVersion {
				return fmt.Errorf("incompatible admin plugin API %d at plugins[%d].admin.apiVersion; this build supports %d", plugin.Admin.APIVersion, index, CurrentAdminPluginAPIVersion)
			}
			if plugin.Admin.PairingVersion == 0 {
				return fmt.Errorf("invalid admin plugin pairing version at plugins[%d].admin.pairingVersion", index)
			}
			target := plugin.Admin.Package + "#" + plugin.Admin.Export
			if _, exists := targets[target]; exists {
				return fmt.Errorf("duplicate admin plugin export %q", target)
			}
			targets[target] = struct{}{}
			routes := make(map[string]struct{}, len(plugin.Admin.Routes))
			for routeIndex, route := range plugin.Admin.Routes {
				if !IsValidAdminPluginRoute(route) {
					return fmt.Errorf("invalid admin plugin route at plugins[%d].admin.routes[%d]", index, routeIndex)
				}
				if _, duplicate := routes[route]; duplicate {
					return fmt.Errorf("duplicate admin plugin route %q", route)
				}
				if _, duplicate := adminRoutes[route]; duplicate {
					return fmt.Errorf("admin plugin route %q collides with another plugin", route)
				}
				routes[route] = struct{}{}
				adminRoutes[route] = struct{}{}
			}
			assets := make(map[string]struct{}, len(plugin.Admin.Assets))
			for assetIndex, asset := range plugin.Admin.Assets {
				if !IsValidAdminPluginAsset(asset) {
					return fmt.Errorf("invalid admin plugin asset at plugins[%d].admin.assets[%d]", index, assetIndex)
				}
				if _, duplicate := assets[asset]; duplicate {
					return fmt.Errorf("duplicate admin plugin asset %q", asset)
				}
				assets[asset] = struct{}{}
			}
		}
		endpointKeys := make(map[string]struct{}, len(plugin.Endpoints))
		for endpointIndex, endpoint := range plugin.Endpoints {
			identity := endpoint.Method + " " + endpoint.Path
			if !validPluginEndpointMetadata(endpoint) {
				return fmt.Errorf("invalid plugin endpoint at plugins[%d].endpoints[%d]", index, endpointIndex)
			}
			if _, exists := endpointKeys[identity]; exists {
				return fmt.Errorf("duplicate plugin endpoint %q", identity)
			}
			endpointKeys[identity] = struct{}{}
		}
	}
	return nil
}

func validPluginEndpointMetadata(endpoint PluginEndpoint) bool {
	switch endpoint.Method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		return false
	}
	return endpoint.Path != "" && endpoint.Path == strings.TrimSpace(endpoint.Path) && !strings.ContainsAny(endpoint.Path, " \t\r\n") && !strings.HasPrefix(endpoint.Path, "/") && !strings.HasPrefix(endpoint.Path, "./") && !strings.Contains(endpoint.Path, "..") && !strings.ContainsAny(endpoint.Path, "?#*") && strings.TrimSpace(endpoint.Summary) != ""
}

func validateAdminFieldComponents(snapshot Snapshot) error {
	if err := ValidateFieldEditors(snapshot); err != nil {
		return err
	}
	plugins := make(map[string]bool, len(snapshot.Plugins))
	for _, plugin := range snapshot.Plugins {
		plugins[plugin.Key] = plugin.Admin != nil
	}
	var inspect func([]Field, string) error
	inspect = func(fields []Field, path string) error {
		for index, field := range fields {
			fieldPath := fmt.Sprintf("%s[%d]", path, index)
			if component := field.Admin.Component; component != nil {
				if err := validatePairedAdminComponent(component, fieldPath+".admin.component", plugins, "admin field component"); err != nil {
					return err
				}
			}
			if err := inspect(EmbeddedBlocks(field), fieldPath+".plugin.embeddedTrees"); err != nil {
				return err
			}
			if field.Nested != nil {
				if component := field.Nested.RowLabelComponent; component != nil {
					if field.Type != FieldTypeArray && field.Type != FieldTypeBlocks {
						return fmt.Errorf("row label component at %s.nested.rowLabelComponent requires an array or blocks field", fieldPath)
					}
					if err := validateRowLabelComponent(component, fieldPath+".nested.rowLabelComponent", plugins); err != nil {
						return err
					}
				}
				if err := inspect(field.Nested.ResolvedFields(), fieldPath+".nested.fields"); err != nil {
					return err
				}
			}
			if field.Blocks != nil {
				for blockIndex, block := range field.Blocks.ResolvedTypes() {
					if err := inspect(block.ResolvedFields(), fmt.Sprintf("%s.blocks.types[%d].fields", fieldPath, blockIndex)); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	for index, collection := range snapshot.Collections {
		if err := inspect(collection.Fields, fmt.Sprintf("collections[%d].fields", index)); err != nil {
			return err
		}
	}
	for index, global := range snapshot.Globals {
		if err := inspect(global.Fields, fmt.Sprintf("globals[%d].fields", index)); err != nil {
			return err
		}
	}
	return nil
}

func validateRowLabelComponent(component *FieldAdminComponent, path string, plugins map[string]bool) error {
	if component.Reference == "" {
		return validatePairedAdminComponent(component, path, plugins, "admin row label component")
	}
	if !localEditorReference.MatchString(component.Reference) || component.Plugin != "" || component.Component != "" || len(component.Config) != 0 && !isJSONObject(component.Config) {
		return fmt.Errorf("invalid local row label at %s", path)
	}
	return nil
}

func validatePairedAdminComponent(component *FieldAdminComponent, path string, plugins map[string]bool, label string) error {
	if component.Reference != "" || !IsValidPluginKey(component.Plugin) || !IsValidAdminPluginExport(component.Component) || len(component.Config) != 0 && !isJSONObject(component.Config) {
		return fmt.Errorf("invalid %s at %s", label, path)
	}
	if !plugins[component.Plugin] {
		return fmt.Errorf("%s at %s requires paired admin plugin %q", label, path, component.Plugin)
	}
	return nil
}

func validateFieldConditionMetadata(snapshot Snapshot) error {
	validateFields := func(fields []Field, fieldsPath string) error {
		var inspect func([]Field, string) error
		inspect = func(candidates []Field, path string) error {
			for index, candidate := range candidates {
				fieldPath := fmt.Sprintf("%s[%d]", path, index)
				if candidate.Admin.Condition != nil {
					nodes := 0
					if err := validateFieldCondition(*candidate.Admin.Condition, fieldPath+".admin.condition", 0, &nodes); err != nil {
						return err
					}
					if err := validateFieldConditionReferences(*candidate.Admin.Condition, fields, candidates, fieldPath+".admin.condition"); err != nil {
						return err
					}
				}
				if err := inspect(EmbeddedBlocks(candidate), fieldPath+".plugin.embeddedTrees"); err != nil {
					return err
				}
				if candidate.Nested != nil {
					if err := inspect(candidate.Nested.ResolvedFields(), fieldPath+".nested.fields"); err != nil {
						return err
					}
				}
				if candidate.Blocks != nil {
					for blockIndex, block := range candidate.Blocks.ResolvedTypes() {
						if err := inspect(block.ResolvedFields(), fmt.Sprintf("%s.blocks.types[%d].fields", fieldPath, blockIndex)); err != nil {
							return err
						}
					}
				}
			}
			return nil
		}
		return inspect(fields, fieldsPath)
	}
	for index, collection := range snapshot.Collections {
		if err := validateFields(collection.Fields, fmt.Sprintf("collections[%d].fields", index)); err != nil {
			return err
		}
	}
	for index, global := range snapshot.Globals {
		if err := validateFields(global.Fields, fmt.Sprintf("globals[%d].fields", index)); err != nil {
			return err
		}
	}
	return nil
}

func validateFieldConditionReferences(condition FieldCondition, root, siblings []Field, path string) error {
	if condition.Predicate != nil {
		predicate := condition.Predicate
		candidates := root
		if predicate.Scope == FieldConditionSibling {
			candidates = siblings
		}
		target := manifestFilterFieldByPath(candidates, predicate.Path.Segments())
		targetType := manifestFieldConditionValueType(target)
		if targetType == "" {
			return fmt.Errorf(
				"invalid field condition path at %s.predicate.path: %s path %q must name a scalar field through non-repeated groups",
				path,
				predicate.Scope,
				predicate.Path.String(),
			)
		}
		for index, value := range predicate.Values {
			if value.Type != targetType {
				return fmt.Errorf(
					"invalid field condition operand at %s.predicate.values[%d].type: path %q stores %s values, got %s",
					path,
					index,
					predicate.Path.String(),
					targetType,
					value.Type,
				)
			}
		}
	}
	for index, child := range condition.Conditions {
		if err := validateFieldConditionReferences(child, root, siblings, fmt.Sprintf("%s.conditions[%d]", path, index)); err != nil {
			return err
		}
	}
	return nil
}

func manifestFieldConditionValueType(candidate *Field) ValueType {
	switch manifestReferenceFilterFieldKind(candidate) {
	case manifestReferenceFilterString:
		return ValueTypeString
	case manifestReferenceFilterNumber:
		return ValueTypeNumber
	case manifestReferenceFilterBoolean:
		return ValueTypeBoolean
	default:
		return ""
	}
}

func validateFieldCondition(condition FieldCondition, path string, depth int, nodes *int) error {
	const (
		maximumDepth = 32
		maximumNodes = 256
	)
	(*nodes)++
	if depth > maximumDepth {
		return fmt.Errorf("field condition exceeds %d nested levels at %s", maximumDepth, path)
	}
	if *nodes > maximumNodes {
		return fmt.Errorf("field condition exceeds %d nodes at %s", maximumNodes, path)
	}
	switch condition.Kind {
	case FieldConditionKindAll, FieldConditionKindAny:
		if len(condition.Conditions) < 2 || condition.Predicate != nil {
			return fmt.Errorf("invalid %s field condition at %s", condition.Kind, path)
		}
	case FieldConditionKindNot:
		if len(condition.Conditions) != 1 || condition.Predicate != nil {
			return fmt.Errorf("invalid not field condition at %s", path)
		}
	case FieldConditionKindPredicate:
		if len(condition.Conditions) != 0 || condition.Predicate == nil {
			return fmt.Errorf("invalid predicate field condition at %s", path)
		}
		if err := validateFieldConditionPredicate(*condition.Predicate, path+".predicate"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid field condition kind %q at %s.kind", condition.Kind, path)
	}
	for index, child := range condition.Conditions {
		if err := validateFieldCondition(child, fmt.Sprintf("%s.conditions[%d]", path, index), depth+1, nodes); err != nil {
			return err
		}
	}
	return nil
}

func validateFieldConditionPredicate(predicate FieldConditionPredicate, path string) error {
	if predicate.Scope != FieldConditionDocument && predicate.Scope != FieldConditionSibling {
		return fmt.Errorf("invalid field condition scope %q at %s.scope", predicate.Scope, path)
	}
	if predicate.Path.String() == "" {
		return fmt.Errorf("missing field condition path at %s.path", path)
	}
	switch predicate.Operator {
	case FieldConditionEquals, FieldConditionNotEquals:
		if len(predicate.Values) != 1 {
			return fmt.Errorf("field condition operator %q requires exactly one value at %s.values", predicate.Operator, path)
		}
	case FieldConditionOneOf:
		if len(predicate.Values) == 0 {
			return fmt.Errorf("field condition operator %q requires at least one value at %s.values", predicate.Operator, path)
		}
	default:
		return fmt.Errorf("invalid field condition operator %q at %s.operator", predicate.Operator, path)
	}
	seen := make(map[string]struct{}, len(predicate.Values))
	for index, value := range predicate.Values {
		valuePath := fmt.Sprintf("%s.values[%d]", path, index)
		if index > 0 && value.Type != predicate.Values[0].Type {
			return fmt.Errorf("mixed field condition value types at %s.type", valuePath)
		}
		switch value.Type {
		case ValueTypeString:
		case ValueTypeNumber:
			number, err := strconv.ParseFloat(value.Value, 64)
			if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
				return fmt.Errorf("invalid number field condition value at %s.value", valuePath)
			}
		case ValueTypeBoolean:
			if value.Value != "true" && value.Value != "false" {
				return fmt.Errorf("invalid boolean field condition value at %s.value", valuePath)
			}
		default:
			return fmt.Errorf("invalid field condition value type %q at %s.type", value.Type, valuePath)
		}
		key := string(value.Type) + "\x00" + value.Value
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("duplicate field condition value at %s", valuePath)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func isJSONObject(value json.RawMessage) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(value, &object) == nil && object != nil
}

// Snapshot returns a deep copy of the manifest.
func (manifest Manifest) Snapshot() Snapshot {
	return cloneSnapshot(manifest.snapshot)
}

// MarshalJSON implements json.Marshaler without exposing mutable internals.
func (manifest Manifest) MarshalJSON() ([]byte, error) {
	return json.Marshal(manifest.snapshot)
}

// UnmarshalJSON replaces a manifest only after Parse validates the complete
// encoded value.
func (manifest *Manifest) UnmarshalJSON(encoded []byte) error {
	parsed, err := Parse(encoded)
	if err != nil {
		return err
	}
	manifest.snapshot = parsed.snapshot
	return nil
}

// Bytes returns the canonical indented manifest encoding with one trailing
// newline, suitable for generated artifacts and golden fixtures.
func (manifest Manifest) Bytes() ([]byte, error) {
	encoded, err := json.MarshalIndent(manifest.snapshot, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// Equal reports whether two manifests have byte-identical canonical encodings.
func (manifest Manifest) Equal(other Manifest) bool {
	left, leftError := manifest.Bytes()
	right, rightError := other.Bytes()
	return leftError == nil && rightError == nil && bytes.Equal(left, right)
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	cloned := snapshot
	cloned.Blocks = cloneBlockTypes(snapshot.Blocks)
	cloned.Application.NameTranslations = cloneStringMap(snapshot.Application.NameTranslations)
	cloned.Application.Endpoints = append([]Endpoint(nil), snapshot.Application.Endpoints...)
	if snapshot.Application.Admin != nil {
		admin := *snapshot.Application.Admin
		cloned.Application.Admin = &admin
	}
	if snapshot.Application.AdminLocalization != nil {
		localization := *snapshot.Application.AdminLocalization
		localization.Languages = append([]AdminLanguage(nil), snapshot.Application.AdminLocalization.Languages...)
		for index := range localization.Languages {
			localization.Languages[index].LabelTranslations = cloneStringMap(snapshot.Application.AdminLocalization.Languages[index].LabelTranslations)
		}
		localization.TimeZones = append([]AdminTimeZone(nil), snapshot.Application.AdminLocalization.TimeZones...)
		for index := range localization.TimeZones {
			localization.TimeZones[index].LabelTranslations = cloneStringMap(snapshot.Application.AdminLocalization.TimeZones[index].LabelTranslations)
		}
		cloned.Application.AdminLocalization = &localization
	}
	if snapshot.Application.Localization != nil {
		localization := *snapshot.Application.Localization
		localization.Locales = make([]Locale, len(snapshot.Application.Localization.Locales))
		for index, locale := range snapshot.Application.Localization.Locales {
			localization.Locales[index] = locale
			localization.Locales[index].FallbackLocales = append([]LocaleCode(nil), locale.FallbackLocales...)
		}
		cloned.Application.Localization = &localization
	}
	cloned.Collections = make([]Collection, len(snapshot.Collections))
	for index, collection := range snapshot.Collections {
		cloned.Collections[index] = collection
		cloned.Collections[index].Labels.SingularTranslations = cloneStringMap(collection.Labels.SingularTranslations)
		cloned.Collections[index].Labels.PluralTranslations = cloneStringMap(collection.Labels.PluralTranslations)
		cloned.Collections[index].Admin.GroupTranslations = cloneStringMap(collection.Admin.GroupTranslations)
		cloned.Collections[index].Admin.DescriptionTranslations = cloneStringMap(collection.Admin.DescriptionTranslations)
		cloned.Collections[index].Admin.DefaultColumns = append([]string(nil), collection.Admin.DefaultColumns...)
		cloned.Collections[index].Admin.LivePreview = cloneLivePreview(collection.Admin.LivePreview)
		if collection.Auth != nil {
			auth := *collection.Auth
			cloned.Collections[index].Auth = &auth
		}
		if collection.Upload != nil {
			upload := *collection.Upload
			upload.MimeTypes = append([]string(nil), collection.Upload.MimeTypes...)
			upload.ImageSizes = append([]ImageSize(nil), collection.Upload.ImageSizes...)
			cloned.Collections[index].Upload = &upload
		}
		if collection.Versions != nil {
			versions := *collection.Versions
			cloned.Collections[index].Versions = &versions
		}
		if collection.DocumentLock != nil {
			settings := *collection.DocumentLock
			cloned.Collections[index].DocumentLock = &settings
		}
		cloned.Collections[index].Fields = cloneFields(collection.Fields)
		cloned.Collections[index].Indexes = cloneIndexes(collection.Indexes)
		cloned.Collections[index].Endpoints = append([]Endpoint(nil), collection.Endpoints...)
	}
	cloned.Globals = make([]Global, len(snapshot.Globals))
	for index, global := range snapshot.Globals {
		cloned.Globals[index] = global
		cloned.Globals[index].Labels.SingularTranslations = cloneStringMap(global.Labels.SingularTranslations)
		cloned.Globals[index].Labels.PluralTranslations = cloneStringMap(global.Labels.PluralTranslations)
		cloned.Globals[index].Admin.GroupTranslations = cloneStringMap(global.Admin.GroupTranslations)
		cloned.Globals[index].Admin.DescriptionTranslations = cloneStringMap(global.Admin.DescriptionTranslations)
		cloned.Globals[index].Admin.LivePreview = cloneLivePreview(global.Admin.LivePreview)
		if global.Versions != nil {
			versions := *global.Versions
			cloned.Globals[index].Versions = &versions
		}
		cloned.Globals[index].Fields = cloneFields(global.Fields)
		cloned.Globals[index].Indexes = cloneIndexes(global.Indexes)
		cloned.Globals[index].Endpoints = append([]Endpoint(nil), global.Endpoints...)
	}
	cloned.Plugins = make([]Plugin, len(snapshot.Plugins))
	for index, plugin := range snapshot.Plugins {
		cloned.Plugins[index] = plugin
		if plugin.Admin != nil {
			admin := *plugin.Admin
			admin.Routes = append([]string(nil), plugin.Admin.Routes...)
			admin.Assets = append([]string(nil), plugin.Admin.Assets...)
			cloned.Plugins[index].Admin = &admin
		}
		if plugin.Ridu != nil {
			compatibility := *plugin.Ridu
			cloned.Plugins[index].Ridu = &compatibility
		}
		cloned.Plugins[index].FieldTypes = make([]PluginFieldType, len(plugin.FieldTypes))
		for fieldIndex, fieldType := range plugin.FieldTypes {
			cloned.Plugins[index].FieldTypes[fieldIndex] = fieldType
			cloned.Plugins[index].FieldTypes[fieldIndex].EmbeddedTypes = append([]string(nil), fieldType.EmbeddedTypes...)
			cloned.Plugins[index].FieldTypes[fieldIndex].JSONSchema = append(json.RawMessage(nil), fieldType.JSONSchema...)
		}
		cloned.Plugins[index].DatabaseContributions = make([]PluginDatabaseContribution, len(plugin.DatabaseContributions))
		for contributionIndex, contribution := range plugin.DatabaseContributions {
			cloned.Plugins[index].DatabaseContributions[contributionIndex] = contribution
			cloned.Plugins[index].DatabaseContributions[contributionIndex].Tables = append([]string(nil), contribution.Tables...)
			cloned.Plugins[index].DatabaseContributions[contributionIndex].Migrations = make([]PluginMigration, len(contribution.Migrations))
			for migrationIndex, pluginMigration := range contribution.Migrations {
				cloned.Plugins[index].DatabaseContributions[contributionIndex].Migrations[migrationIndex] = pluginMigration
				cloned.Plugins[index].DatabaseContributions[contributionIndex].Migrations[migrationIndex].UpSQL = append([]string(nil), pluginMigration.UpSQL...)
				cloned.Plugins[index].DatabaseContributions[contributionIndex].Migrations[migrationIndex].DownSQL = append([]string(nil), pluginMigration.DownSQL...)
			}
		}
		cloned.Plugins[index].Endpoints = append([]PluginEndpoint(nil), plugin.Endpoints...)
	}
	_ = BindBlockReferences(&cloned)
	return cloned
}

func cloneIndexes(indexes []CollectionIndex) []CollectionIndex {
	cloned := make([]CollectionIndex, len(indexes))
	for index, candidate := range indexes {
		cloned[index] = candidate
		cloned[index].Fields = append([]query.Path(nil), candidate.Fields...)
	}
	return cloned
}

func cloneLivePreview(preview *LivePreview) *LivePreview {
	if preview == nil {
		return nil
	}
	cloned := *preview
	cloned.Breakpoints = make([]PreviewBreakpoint, len(preview.Breakpoints))
	for index, breakpoint := range preview.Breakpoints {
		cloned.Breakpoints[index] = breakpoint
		cloned.Breakpoints[index].LabelTranslations = cloneStringMap(breakpoint.LabelTranslations)
	}
	return &cloned
}

func cloneFields(fields []Field) []Field {
	cloned := make([]Field, len(fields))
	for index, field := range fields {
		cloned[index] = field
		cloned[index].Admin.Extensions = cloneAdminExtensions(field.Admin.Extensions)
		if field.Admin.Editor != nil {
			editor := *field.Admin.Editor
			editor.Config = append(json.RawMessage(nil), editor.Config...)
			cloned[index].Admin.Editor = &editor
		}
		cloned[index].Admin.LabelTranslations = cloneStringMap(field.Admin.LabelTranslations)
		cloned[index].Admin.DescriptionTranslations = cloneStringMap(field.Admin.DescriptionTranslations)
		cloned[index].Admin.PlaceholderTranslations = cloneStringMap(field.Admin.PlaceholderTranslations)
		cloned[index].Admin.TabTranslations = cloneStringMap(field.Admin.TabTranslations)
		if field.Admin.Row != nil {
			row := *field.Admin.Row
			row.Extensions = cloneAdminExtensions(field.Admin.Row.Extensions)
			cloned[index].Admin.Row = &row
		}
		if field.Admin.Collapsible != nil {
			collapsible := *field.Admin.Collapsible
			collapsible.LabelTranslations = cloneStringMap(field.Admin.Collapsible.LabelTranslations)
			collapsible.Extensions = cloneAdminExtensions(field.Admin.Collapsible.Extensions)
			cloned[index].Admin.Collapsible = &collapsible
		}
		if field.Admin.TabGroup != nil {
			group := *field.Admin.TabGroup
			group.Extensions = cloneAdminExtensions(field.Admin.TabGroup.Extensions)
			cloned[index].Admin.TabGroup = &group
		}
		if field.Admin.Condition != nil {
			cloned[index].Admin.Condition = cloneFieldCondition(field.Admin.Condition)
		}
		if field.Admin.Component != nil {
			component := *field.Admin.Component
			component.Config = append(json.RawMessage(nil), field.Admin.Component.Config...)
			cloned[index].Admin.Component = &component
		}
		if field.Default != nil {
			value := *field.Default
			cloned[index].Default = &value
		}
		if field.List != nil {
			list := *field.List
			cloned[index].List = &list
		}
		if field.Text != nil {
			text := *field.Text
			text.MinLength = cloneInt(field.Text.MinLength)
			text.MaxLength = cloneInt(field.Text.MaxLength)
			if field.Text.Slug != nil {
				slug := *field.Text.Slug
				slug.SourcePath, _ = query.NewPath(field.Text.Slug.SourcePath.Segments()...)
				text.Slug = &slug
			}
			cloned[index].Text = &text
		}
		if field.Textarea != nil {
			textarea := *field.Textarea
			textarea.MinLength = cloneInt(field.Textarea.MinLength)
			textarea.MaxLength = cloneInt(field.Textarea.MaxLength)
			cloned[index].Textarea = &textarea
		}
		if field.Code != nil {
			code := *field.Code
			code.MinLength = cloneInt(field.Code.MinLength)
			code.MaxLength = cloneInt(field.Code.MaxLength)
			cloned[index].Code = &code
		}
		if field.Number != nil {
			number := *field.Number
			number.Min = cloneFloat(field.Number.Min)
			number.Max = cloneFloat(field.Number.Max)
			number.Step = cloneFloat(field.Number.Step)
			cloned[index].Number = &number
		}
		if field.Date != nil {
			date := *field.Date
			cloned[index].Date = &date
		}
		if field.Point != nil {
			point := *field.Point
			cloned[index].Point = &point
		}
		if field.UI != nil {
			ui := *field.UI
			cloned[index].UI = &ui
		}
		if field.Join != nil {
			join := *field.Join
			join.DefaultColumns = append([]string(nil), field.Join.DefaultColumns...)
			if field.Join.AllowCreate != nil {
				allowCreate := *field.Join.AllowCreate
				join.AllowCreate = &allowCreate
			}
			cloned[index].Join = &join
		}
		if field.Virtual != nil {
			virtual := *field.Virtual
			cloned[index].Virtual = &virtual
		}
		if field.Select != nil {
			selectField := *field.Select
			selectField.DefaultValues = append([]string(nil), field.Select.DefaultValues...)
			selectField.Options = make([]SelectOption, len(field.Select.Options))
			for optionIndex, option := range field.Select.Options {
				selectField.Options[optionIndex] = option
				selectField.Options[optionIndex].LabelTranslations = cloneStringMap(option.LabelTranslations)
			}
			cloned[index].Select = &selectField
		}
		if field.Relationship != nil {
			relationship := *field.Relationship
			relationship.Targets = append([]RelationshipTarget(nil), field.Relationship.Targets...)
			relationship.OptionFilters = cloneRelationshipFilters(field.Relationship.OptionFilters)
			cloned[index].Relationship = &relationship
		}
		if field.Upload != nil {
			upload := *field.Upload
			upload.OptionFilters = cloneRelationshipFilters(field.Upload.OptionFilters)
			cloned[index].Upload = &upload
		}
		if field.Nested != nil {
			nested := *field.Nested
			nested.bound = nil
			nested.Fields = cloneFields(field.Nested.Fields)
			if field.Nested.RowLabelComponent != nil {
				component := *field.Nested.RowLabelComponent
				component.Config = append(json.RawMessage(nil), field.Nested.RowLabelComponent.Config...)
				nested.RowLabelComponent = &component
			}
			if field.Nested.RowLabels != nil {
				rowLabels := *field.Nested.RowLabels
				rowLabels.SingularTranslations = cloneStringMap(field.Nested.RowLabels.SingularTranslations)
				rowLabels.PluralTranslations = cloneStringMap(field.Nested.RowLabels.PluralTranslations)
				nested.RowLabels = &rowLabels
			}
			cloned[index].Nested = &nested
		}
		if field.Blocks != nil {
			blocks := *field.Blocks
			blocks.bound = nil
			blocks.BlockReferences = append([]string(nil), field.Blocks.BlockReferences...)
			blocks.Types = nil
			if field.Blocks.Types != nil {
				blocks.Types = make([]BlockType, len(field.Blocks.Types))
			}
			for blockIndex, block := range field.Blocks.Types {
				blocks.Types[blockIndex] = block
				blocks.Types[blockIndex].bound = nil
				blocks.Types[blockIndex].Labels.SingularTranslations = cloneStringMap(block.Labels.SingularTranslations)
				blocks.Types[blockIndex].Labels.PluralTranslations = cloneStringMap(block.Labels.PluralTranslations)
				blocks.Types[blockIndex].Fields = cloneFields(block.Fields)
				if block.Admin != nil {
					admin := *block.Admin
					blocks.Types[blockIndex].Admin = &admin
				}
			}
			cloned[index].Blocks = &blocks
		}
		if field.Plugin != nil {
			plugin := *field.Plugin
			plugin.Config = append(json.RawMessage(nil), field.Plugin.Config...)
			plugin.ReferenceKeys = append([]string(nil), field.Plugin.ReferenceKeys...)
			plugin.EmbeddedTrees = cloneEmbeddedTrees(field.Plugin.EmbeddedTrees)
			cloned[index].Plugin = &plugin
		}
	}
	return cloned
}

func cloneRelationshipFilters(filters []RelationshipFilter) []RelationshipFilter {
	cloned := append([]RelationshipFilter(nil), filters...)
	for index, filter := range filters {
		if filter.SourcePath != nil {
			path := *filter.SourcePath
			cloned[index].SourcePath = &path
		}
		if filter.Value != nil {
			value := *filter.Value
			cloned[index].Value = &value
		}
	}
	return cloned
}

func cloneFieldCondition(condition *FieldCondition) *FieldCondition {
	if condition == nil {
		return nil
	}
	cloned := *condition
	cloned.Conditions = make([]FieldCondition, len(condition.Conditions))
	for index := range condition.Conditions {
		cloned.Conditions[index] = *cloneFieldCondition(&condition.Conditions[index])
	}
	if condition.Predicate != nil {
		predicate := *condition.Predicate
		predicate.Values = append([]FieldConditionValue(nil), condition.Predicate.Values...)
		cloned.Predicate = &predicate
	}
	return &cloned
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

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode schema manifest trailing data: %w", err)
	}
	return fmt.Errorf("decode schema manifest: unexpected trailing JSON value")
}
