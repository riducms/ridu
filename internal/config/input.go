// Package config validates and normalizes the public authoring model into a
// canonical immutable schema manifest.
package config

import (
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"time"
)

type Input struct {
	Name             string
	NameTranslations map[string]string
	AllowIDOnCreate  bool
	Admin            Admin
	Localization     *Localization
	Collections      []Collection
	Globals          []Global
	Endpoints        []Endpoint
	Plugins          []Plugin
}

type Localization struct {
	Locales         []Locale
	DefaultLocale   schema.LocaleCode
	DisableFallback bool
}

type Locale struct {
	Code            schema.LocaleCode
	Label           string
	RTL             bool
	FallbackLocales []schema.LocaleCode
}

type Admin struct {
	User         schema.CollectionSlug
	Localization AdminLocalization
}

type AdminLocalization struct {
	Languages       []AdminLanguage
	DefaultLanguage string
	TimeZones       []AdminTimeZone
	DefaultTimeZone string
}

type AdminLanguage struct {
	Code              string
	Label             string
	LabelTranslations map[string]string
	RTL               bool
}

type AdminTimeZone struct {
	ID                string
	Label             string
	LabelTranslations map[string]string
}

type Collection struct {
	Slug                 schema.CollectionSlug
	Labels               schema.CollectionLabels
	Admin                CollectionAdmin
	Fields               []field.Definition
	Indexes              []CollectionIndex
	Auth                 bool
	SessionDuration      time.Duration
	PasswordMinLength    int
	PasswordMaxBytes     int
	PasswordBcryptCost   int
	MaxLoginAttempts     int
	LockDuration         time.Duration
	PasswordReset        bool
	PasswordResetTTL     time.Duration
	VerifyEmail          bool
	VerificationTTL      time.Duration
	APIKeys              bool
	Upload               bool
	UploadConfig         UploadConfig
	Versions             bool
	Trash                bool
	LockDocuments        bool
	DocumentLockDuration time.Duration
	VersionConfig        VersionConfig
	Endpoints            []Endpoint
}

type CollectionIndex struct {
	Fields []string
	Unique bool
}

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

type LivePreviewConfig struct {
	URL         string
	Breakpoints []PreviewBreakpoint
}

type PreviewBreakpoint struct {
	Name              string
	Label             string
	LabelTranslations map[string]string
	Width             int
	Height            int
}

type Global struct {
	Slug              schema.CollectionSlug
	Label             string
	LabelTranslations map[string]string
	Admin             GlobalAdmin
	Fields            []field.Definition
	Versions          bool
	VersionConfig     VersionConfig
	Endpoints         []Endpoint
}

type GlobalAdmin struct {
	Group                   string
	GroupTranslations       map[string]string
	Description             string
	DescriptionTranslations map[string]string
	LivePreview             LivePreviewConfig
}

type UploadConfig struct {
	MaxFileSize int64
	MimeTypes   []string
	Private     bool
	ImageSizes  []ImageSize
}

type ImageSize struct {
	Name   string
	Width  int
	Height int
	Fit    string
}
type VersionConfig struct {
	Drafts           bool
	MaxPerDocument   int
	AutosaveInterval time.Duration
}

type Plugin struct {
	Key                   string
	Version               string
	GoPackage             string
	APIVersion            uint32
	Ridu                  *PluginCompatibility
	Admin                 *PluginAdmin
	FieldTypes            []PluginFieldType
	DatabaseContributions []PluginDatabaseContribution
	Endpoints             []PluginEndpoint
}

type PluginCompatibility struct {
	Minimum          string
	MaximumExclusive string
}

type PluginAdmin struct {
	Package        string
	Export         string
	APIVersion     uint32
	PairingVersion uint32
	Routes         []string
	Assets         []string
}

type PluginFieldType struct {
	Key               string
	TypeScriptPackage string
	TypeScriptOutput  string
	TypeScriptInput   string
	TypeScriptWhere   string
	GoPackage         string
	GoType            string
	JSONSchema        []byte
}

type PluginMigration struct {
	Version uint32
	Name    string
	UpSQL   []string
	DownSQL []string
}

type PluginDatabaseContribution struct {
	Adapter    schema.PluginDatabaseAdapter
	Migrations []PluginMigration
	Tables     []string
}

type PluginEndpoint struct {
	Method  string
	Path    string
	Summary string
}

type Endpoint struct {
	Method  string
	Path    string
	Summary string
}
