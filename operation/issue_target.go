package operation

import (
	"fmt"
	"slices"
	"strings"

	"github.com/riducms/ridu/schema"
)

// IssueTarget selects the current field or a descendant of its candidate value.
// It is immutable. Repeated rows use their _key, never their display position.
// Configuration and runtime identities are supplied by Ridu when validation runs.
type IssueTarget struct {
	segments []IssueTargetSegment
	err      string
}

// IssueTargetSegment is one field, repeated row, or exact translation selector.
// Segments returns detached selectors for inspection; construct targets with At,
// Field, Row, Block, and Locale.
type IssueTargetSegment struct {
	Field     string
	RowKey    string
	BlockType string
	Locale    schema.LocaleCode
}

// At starts a field-relative target. With no arguments it selects the current
// field. Field paths may contain dots, such as At("seo.title").
func At(paths ...string) IssueTarget {
	var target IssueTarget
	for _, path := range paths {
		target = target.Field(path)
	}
	return target
}

// Field selects a child field, optionally through dot-separated groups.
// Crossing an array or Blocks field requires an explicit Row or Block selector.
// Each traversed group must contain an object in the candidate. For an absent,
// null or malformed group, target that group itself instead. A missing scalar
// child of an existing object remains a valid target.
func (target IssueTarget) Field(path string) IssueTarget {
	for _, name := range strings.Split(path, ".") {
		if !schema.IsValidFieldName(name) {
			return target.invalid(fmt.Sprintf("target field %q must be an authored field name; use Row or Block for repeated values", name))
		}
		target = target.append(IssueTargetSegment{Field: name})
	}
	return target
}

// Row selects one array row by its nonempty _key.
func (target IssueTarget) Row(key string) IssueTarget {
	if key == "" {
		return target.invalid("target row requires a nonempty _key")
	}
	return target.append(IssueTargetSegment{RowKey: key})
}

// Block selects one Blocks row by _key and its expected blockType. A different
// Block case is a different target even when a caller reuses the same key.
func (target IssueTarget) Block(key, blockType string) IssueTarget {
	if key == "" || !schema.IsValidPluginKey(blockType) {
		return target.invalid("target block requires a nonempty _key and a valid blockType")
	}
	return target.append(IssueTargetSegment{RowKey: key, BlockType: blockType})
}

// Locale selects an exact translation in an all-locales candidate envelope.
// Ordinary callbacks inherit their exact locale and do not need this selector.
// A target cannot request fallback or leave an exact-locale callback's scope.
func (target IssueTarget) Locale(locale schema.LocaleCode) IssueTarget {
	if locale == "" {
		return target.invalid("target locale must not be empty")
	}
	return target.append(IssueTargetSegment{Locale: locale})
}

// Segments returns a detached view of the relative selectors.
func (target IssueTarget) Segments() []IssueTargetSegment { return slices.Clone(target.segments) }

// Err reports malformed selector syntax. Ridu also checks field names, keys,
// Block cases and locales against the candidate when the validator returns.
func (target IssueTarget) Err() error {
	if target.err != "" {
		return fmt.Errorf("%s", target.err)
	}
	return nil
}

func (target IssueTarget) invalid(message string) IssueTarget {
	if target.err == "" {
		target.err = message
	}
	return target
}

func (target IssueTarget) append(segment IssueTargetSegment) IssueTarget {
	if target.err != "" {
		return target
	}
	if len(target.segments) >= 64 {
		return target.invalid("issue target supports at most 64 selectors")
	}
	bytes := len(segment.Field) + len(segment.RowKey) + len(segment.BlockType) + len(segment.Locale)
	for _, current := range target.segments {
		bytes += len(current.Field) + len(current.RowKey) + len(current.BlockType) + len(current.Locale)
	}
	if bytes > 4096 {
		return target.invalid("issue target exceeds 4096 bytes")
	}
	target.segments = append(slices.Clone(target.segments), segment)
	return target
}
