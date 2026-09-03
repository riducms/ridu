package field

import "strings"

// Slug defines a required, unique, indexed text field whose value is generated
// from sourcePath until an author supplies a manual value. The source may be a
// direct string field or a string field beneath non-repeated groups.
//
// Slugs intentionally use ordinary text storage and generated string contracts.
// Localization and defaults are not supported by this first-class helper.
func Slug(name, sourcePath string, options ...StringOption) Definition {
	definition := build(KindText, name, options)
	definition.slugSource = strings.TrimSpace(sourcePath)
	definition.slugConfigured = true
	definition.required = true
	definition.unique = true
	definition.index = true
	if definition.slugSource == "" {
		definition.issues = append(definition.issues, Issue{
			Code: "missing_slug_source", Path: "sourcePath",
			Message: "slug source path must not be empty",
		})
	}
	if definition.localized {
		definition.issues = append(definition.issues, Issue{
			Code: "unsupported_slug_localization", Path: "options.localized",
			Message: "slug fields cannot be localized; use separate explicit slug fields when locale-specific URLs are required",
		})
	}
	if definition.defaultValue != nil {
		definition.issues = append(definition.issues, Issue{
			Code: "unsupported_slug_default", Path: "options.default",
			Message: "slug fields derive their initial value from the configured source and cannot declare a default",
		})
	}
	return cloneDefinitions([]Definition{definition})[0]
}

// NormalizeSlug converts a source or manual value into Ridu's deterministic
// URL-segment form. ASCII letters are lowercased, digits and underscores are
// retained, whitespace and hyphen runs become one hyphen, and other characters
// are removed.
func NormalizeSlug(value string) string {
	value = strings.Trim(value, " \t\n\r\f\v")
	var normalized strings.Builder
	normalized.Grow(len(value))
	pendingSeparator := false
	for index := 0; index < len(value); index++ {
		character := value[index]
		switch {
		case character >= 'A' && character <= 'Z':
			if pendingSeparator && normalized.Len() != 0 {
				normalized.WriteByte('-')
			}
			normalized.WriteByte(character + ('a' - 'A'))
			pendingSeparator = false
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9', character == '_':
			if pendingSeparator && normalized.Len() != 0 {
				normalized.WriteByte('-')
			}
			normalized.WriteByte(character)
			pendingSeparator = false
		case character == '-' || asciiSlugWhitespace(character):
			pendingSeparator = normalized.Len() != 0
		}
	}
	return normalized.String()
}

func asciiSlugWhitespace(character byte) bool {
	switch character {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	default:
		return false
	}
}
