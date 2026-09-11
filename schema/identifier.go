package schema

import (
	"regexp"
	"strings"

	"golang.org/x/mod/semver"
)

var (
	blockTypeNamePattern  = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	stableIDPattern       = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	collectionSlugPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	fieldNamePattern      = regexp.MustCompile(`^[a-z][A-Za-z0-9_]*$`)
	pluginKeyPattern      = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	adminPackagePattern   = regexp.MustCompile(`^(?:@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*(?:/[A-Za-z0-9._-]+)*$`)
	adminExportPattern    = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)
	goPackagePattern      = regexp.MustCompile(`^[A-Za-z0-9._~-]+(?:/[A-Za-z0-9._~-]+)+$`)
	localeCodePattern     = regexp.MustCompile(`^[A-Za-z0-9]+(?:[-_][A-Za-z0-9]+)*$`)
)

// StableID is a deterministic internal schema identity. Migration artifacts
// record continuity when an authored collection slug or field path changes.
type StableID string

// CollectionSlug is the URL- and API-facing name of a collection.
type CollectionSlug string

// LocaleCode is the stable request and storage identity of one content locale.
// It commonly contains a BCP 47 language tag, but may also use an application-
// owned alphanumeric identifier separated by hyphens or underscores.
type LocaleCode string

// IsValidStableID reports whether a stable ID has the canonical kebab-case form.
func IsValidStableID(value string) bool { return stableIDPattern.MatchString(value) }

// IsValidCollectionSlug reports whether a collection slug has the canonical
// lowercase kebab-case form.
func IsValidCollectionSlug(value string) bool { return collectionSlugPattern.MatchString(value) }

// IsValidLocaleCode reports whether a locale code is safe in URLs, generated
// contracts, and deterministic PostgreSQL identifiers. Request control tokens
// are reserved and cannot be configured as locales.
func IsValidLocaleCode(value string) bool {
	if len(value) == 0 || len(value) > 35 || !localeCodePattern.MatchString(value) {
		return false
	}
	switch strings.ToLower(value) {
	case "all", "false", "none", "null":
		return false
	default:
		return value != "*"
	}
}

// IsValidFieldName reports whether a field name is safe as an unquoted
// generated property and query path segment.
func IsValidFieldName(value string) bool { return fieldNamePattern.MatchString(value) }

// IsValidPluginKey reports whether a plugin key has the canonical kebab-case form.
func IsValidPluginKey(value string) bool { return pluginKeyPattern.MatchString(value) }

// IsValidAdminPluginPackage reports whether value is a static JavaScript
// package specifier rather than a relative path or generated-code fragment.
func IsValidAdminPluginPackage(value string) bool { return adminPackagePattern.MatchString(value) }

// IsValidAdminPluginExport reports whether value can be used as a named
// JavaScript import without computed or executable syntax.
func IsValidAdminPluginExport(value string) bool { return adminExportPattern.MatchString(value) }

// IsValidSemanticVersion reports whether value is a complete semantic version.
func IsValidSemanticVersion(value string) bool {
	return value != "" && !strings.HasPrefix(value, "v") && semver.IsValid("v"+value)
}

// IsValidSemanticVersionRange reports whether minimum and the optional
// exclusive maximum form a non-empty semantic-version interval.
func IsValidSemanticVersionRange(minimum, maximumExclusive string) bool {
	if !IsValidSemanticVersion(minimum) || maximumExclusive != "" && !IsValidSemanticVersion(maximumExclusive) {
		return false
	}
	return maximumExclusive == "" || semver.Compare("v"+minimum, "v"+maximumExclusive) < 0
}

// SemanticVersionInRange reports whether version is inside a validated
// inclusive-minimum, exclusive-maximum interval.
func SemanticVersionInRange(version, minimum, maximumExclusive string) bool {
	if !IsValidSemanticVersion(version) || !IsValidSemanticVersionRange(minimum, maximumExclusive) {
		return false
	}
	canonical := "v" + version
	return semver.Compare(canonical, "v"+minimum) >= 0 && (maximumExclusive == "" || semver.Compare(canonical, "v"+maximumExclusive) < 0)
}

// IsValidAdminPluginRoute reports whether a relative plugin route cannot
// shadow framework-owned login or collection routes.
func IsValidAdminPluginRoute(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && !strings.ContainsAny(value, " \t\r\n") && !strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "./") && value != "login" && value != "collections" && !strings.HasPrefix(value, "collections/") && !strings.Contains(value, "..") && !strings.ContainsAny(value, "?#*")
}

// IsValidAdminPluginAsset reports whether value is a safe package-relative
// side-effect import generated into the static admin entry.
func IsValidAdminPluginAsset(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && !strings.ContainsAny(value, " \t\r\n") && !strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "./") && !strings.Contains(value, "..") && !strings.ContainsAny(value, "?#")
}

// IsValidGoPackage reports whether value is a portable Go import path.
func IsValidGoPackage(value string) bool { return goPackagePattern.MatchString(value) }

// IsValidBlockTypeName reports whether a generated block name is portable to Go and TypeScript.
func IsValidBlockTypeName(value string) bool { return blockTypeNamePattern.MatchString(value) }
