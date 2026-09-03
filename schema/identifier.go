package schema

import (
	"regexp"
	"strings"

	"golang.org/x/mod/semver"
)

var (
	stableIDPattern       = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	collectionSlugPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	fieldNamePattern      = regexp.MustCompile(`^[a-z][A-Za-z0-9_]*$`)
	pluginKeyPattern      = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	adminPackagePattern   = regexp.MustCompile(`^(?:@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*(?:/[A-Za-z0-9._-]+)*$`)
	adminExportPattern    = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)
	goPackagePattern      = regexp.MustCompile(`^[A-Za-z0-9._~-]+(?:/[A-Za-z0-9._~-]+)+$`)
	pluginTablePattern    = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
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

// IsValidPluginTable reports whether value is a portable unquoted identifier
// allowed for an explicitly adapter-owned plugin table.
func IsValidPluginTable(value string) bool { return pluginTablePattern.MatchString(value) }

// IsValidPluginMigrationSQL reports whether one adapter-specific plugin
// migration entry contains exactly one non-empty statement and cannot take
// control of the adapter-owned transaction or connection.
func IsValidPluginMigrationSQL(adapter PluginDatabaseAdapter, value string) bool {
	if strings.TrimSpace(value) == "" || strings.ContainsRune(value, '\x00') {
		return false
	}
	if adapter != PluginDatabaseAdapterPostgres && adapter != PluginDatabaseAdapterSQLite {
		return false
	}
	keyword, secondKeyword, singleStatement := pluginSQLStatementBoundary(adapter, value)
	if !singleStatement {
		return false
	}
	switch keyword {
	case "BEGIN", "COMMIT", "END", "ROLLBACK", "SAVEPOINT", "RELEASE":
		return false
	}
	if adapter == PluginDatabaseAdapterPostgres && (keyword == "ABORT" ||
		keyword == "START" && secondKeyword == "TRANSACTION" ||
		keyword == "PREPARE" && secondKeyword == "TRANSACTION") {
		return false
	}
	if adapter == PluginDatabaseAdapterSQLite && (keyword == "ATTACH" || keyword == "DETACH" || keyword == "PRAGMA") {
		return false
	}
	return keyword != ""
}

func pluginSQLStatementBoundary(adapter PluginDatabaseAdapter, value string) (string, string, bool) {
	var first string
	var initial []string
	terminated := false
	trigger := false
	triggerBody := false
	triggerEnded := false
	caseDepth := 0
	for index := 0; index < len(value); {
		switch {
		case isPluginSQLSpace(value[index]):
			index++
		case index+1 < len(value) && value[index:index+2] == "--":
			index += 2
			for index < len(value) && value[index] != '\n' && value[index] != '\r' {
				index++
			}
		case index+1 < len(value) && value[index:index+2] == "/*":
			end, ok := pluginSQLBlockCommentEnd(value, index+2, adapter == PluginDatabaseAdapterPostgres)
			if !ok {
				return "", "", false
			}
			index = end
		case value[index] == '\'' || value[index] == '"' || adapter == PluginDatabaseAdapterSQLite && value[index] == '`':
			backslashEscapes := adapter == PluginDatabaseAdapterPostgres && value[index] == '\'' && pluginSQLPostgresEscapeString(value, index)
			end, ok := pluginSQLQuotedEnd(value, index, value[index], backslashEscapes)
			if !ok || terminated {
				return "", "", false
			}
			index = end
		case adapter == PluginDatabaseAdapterSQLite && value[index] == '[':
			end, ok := pluginSQLBracketEnd(value, index+1)
			if !ok || terminated {
				return "", "", false
			}
			index = end
		case adapter == PluginDatabaseAdapterPostgres && value[index] == '$':
			end, matched, ok := pluginSQLDollarQuoteEnd(value, index)
			if !ok || terminated {
				return "", "", false
			}
			if matched {
				index = end
				continue
			}
			index++
		case isPluginSQLWord(value[index]):
			start := index
			for index < len(value) && isPluginSQLWord(value[index]) {
				index++
			}
			if terminated {
				return "", "", false
			}
			word := strings.ToUpper(value[start:index])
			if first == "" {
				first = word
			}
			if len(initial) < 4 {
				initial = append(initial, word)
				trigger = adapter == PluginDatabaseAdapterSQLite && pluginSQLStartsTrigger(initial)
			}
			if trigger {
				switch word {
				case "BEGIN":
					if !triggerBody {
						triggerBody = true
					}
				case "CASE":
					if triggerBody {
						caseDepth++
					}
				case "END":
					if triggerBody && caseDepth > 0 {
						caseDepth--
					} else if triggerBody {
						triggerBody = false
						triggerEnded = true
					}
				}
			}
		case value[index] == ';':
			index++
			if terminated {
				continue
			}
			if trigger && triggerBody && !triggerEnded {
				continue
			}
			terminated = true
		default:
			if terminated {
				return "", "", false
			}
			index++
		}
	}
	second := ""
	if len(initial) > 1 {
		second = initial[1]
	}
	return first, second, first != ""
}

func pluginSQLStartsTrigger(words []string) bool {
	if len(words) < 2 || words[0] != "CREATE" {
		return false
	}
	if words[1] == "TRIGGER" {
		return true
	}
	if len(words) >= 3 && (words[1] == "TEMP" || words[1] == "TEMPORARY") && words[2] == "TRIGGER" {
		return true
	}
	return len(words) >= 4 && words[1] == "OR" && words[2] == "REPLACE" && words[3] == "TRIGGER"
}

func pluginSQLBlockCommentEnd(value string, index int, nested bool) (int, bool) {
	depth := 1
	for index < len(value) {
		switch {
		case nested && index+1 < len(value) && value[index:index+2] == "/*":
			depth++
			index += 2
		case index+1 < len(value) && value[index:index+2] == "*/":
			depth--
			index += 2
			if depth == 0 {
				return index, true
			}
		default:
			index++
		}
	}
	return 0, false
}

func pluginSQLPostgresEscapeString(value string, index int) bool {
	if index == 0 || value[index-1] != 'E' && value[index-1] != 'e' {
		return false
	}
	return index == 1 || !isPluginSQLIdentifierByte(value[index-2])
}

func pluginSQLQuotedEnd(value string, index int, quote byte, backslashEscapes bool) (int, bool) {
	for index++; index < len(value); index++ {
		if backslashEscapes && value[index] == '\\' {
			index++
			if index >= len(value) {
				return 0, false
			}
			continue
		}
		if value[index] != quote {
			continue
		}
		if index+1 < len(value) && value[index+1] == quote {
			index++
			continue
		}
		return index + 1, true
	}
	return 0, false
}

func pluginSQLBracketEnd(value string, index int) (int, bool) {
	for index < len(value) {
		if value[index] != ']' {
			index++
			continue
		}
		if index+1 < len(value) && value[index+1] == ']' {
			index += 2
			continue
		}
		return index + 1, true
	}
	return 0, false
}

func pluginSQLDollarQuoteEnd(value string, index int) (int, bool, bool) {
	if index > 0 && (isPluginSQLWord(value[index-1]) || value[index-1] == '$' || value[index-1] >= 0x80) {
		return index + 1, false, true
	}
	tagEnd := index + 1
	for tagEnd < len(value) && (isPluginSQLWord(value[tagEnd]) || value[tagEnd] == '_') {
		tagEnd++
	}
	if tagEnd >= len(value) || value[tagEnd] != '$' {
		return index + 1, false, true
	}
	tag := value[index : tagEnd+1]
	closing := strings.Index(value[tagEnd+1:], tag)
	if closing < 0 {
		return 0, true, false
	}
	return tagEnd + 1 + closing + len(tag), true, true
}

func isPluginSQLSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}

func isPluginSQLWord(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' || value >= '0' && value <= '9' || value == '_'
}

func isPluginSQLIdentifierByte(value byte) bool {
	return isPluginSQLWord(value) || value == '$' || value >= 0x80
}
