package field

import "strings"

// Slug defines a required, unique, indexed text field whose value is generated
// from sourcePath until an author supplies a manual value. The source may be a
// direct string field or a string field beneath non-repeated groups.
//
// Slugs intentionally use ordinary text storage and generated string contracts.
// Localization and defaults are not supported by this first-class helper.
func Slug(name, sourcePath string) TextField {
	f := Text(name).Required().Unique().Index()
	f.definition.slugConfigured = true
	f.definition.slugSource = strings.TrimSpace(sourcePath)
	return f
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
