package query

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// Path segments include authored field names and canonical block discriminators.
// Field names use letters, numbers, and underscores, while block keys are
// lowercase kebab-case and therefore require hyphens here.
const pathSegmentPattern = `^[a-z][A-Za-z0-9_-]*$`

// validPathSegment reports whether segment matches pathSegmentPattern. It is
// hand-written because every string field name in a filter is checked.
func validPathSegment(segment string) bool {
	if segment == "" || segment[0] < 'a' || segment[0] > 'z' {
		return false
	}
	for index := 1; index < len(segment); index++ {
		character := segment[index]
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

const (
	// MaxPathSegments bounds recursive query compilation and schema traversal.
	// Ridu's operation engine supports substantially shallower authored models,
	// leaving this ceiling as a fail-safe rather than a practical restriction.
	MaxPathSegments = 64
	// MaxPathBytes bounds user-supplied filter/sort/population path allocation.
	MaxPathBytes = 4096
)

// Path is an immutable, validated document field path. Its canonical string
// form joins segments with a dot, for example "seo.title". The exact
// single-segment paths "_status" and "_revision" address Ridu's finite
// version metadata; arbitrary underscore-prefixed or nested system paths
// remain invalid.
type Path struct {
	segments []string
}

// FieldPath is a field in a filter or sort: a dot-separated name written in
// your code, such as "title" or "seo.title", or a Path. A malformed name
// panics, like regexp.MustCompile; use ParsePath for names from user input.
type FieldPath interface {
	~string | Path
}

// NewPath validates and constructs a field path from segments computed at run
// time, such as names read from a schema. For a name written in your code, pass
// the string to a filter helper instead.
func NewPath(segments ...string) (Path, error) {
	if len(segments) == 0 {
		return Path{}, fmt.Errorf("query path requires at least one segment")
	}
	if len(segments) > MaxPathSegments {
		return Path{}, fmt.Errorf("query path supports at most %d segments", MaxPathSegments)
	}

	cloned := append([]string(nil), segments...)
	if len(cloned) == 1 && (cloned[0] == "_status" || cloned[0] == "_revision") {
		return Path{segments: cloned}, nil
	}
	totalBytes := len(cloned) - 1
	for index, segment := range cloned {
		if !validPathSegment(segment) {
			return Path{}, fmt.Errorf("query path segment %d %q must match %s", index, segment, pathSegmentPattern)
		}
		totalBytes += len(segment)
	}
	if totalBytes > MaxPathBytes {
		return Path{}, fmt.Errorf("query path exceeds %d bytes", MaxPathBytes)
	}
	return Path{segments: cloned}, nil
}

// Field names a field by its segments, such as Field("seo", "title"). Like
// regexp.MustCompile, it panics when a name is malformed. Filter helpers also
// accept the dot-separated string "seo.title" directly; use Field when you need
// a Path value, such as for ListOptions.Select.
func Field(segments ...string) Path {
	path, err := NewPath(segments...)
	if err != nil {
		panic(err)
	}
	return path
}

// ParsePath validates and constructs a path from its dot-separated form. Use
// it for names that come from user input, where a malformed name is an error.
func ParsePath(value string) (Path, error) {
	if value == "" {
		return Path{}, fmt.Errorf("query path must not be empty")
	}
	return NewPath(strings.Split(value, ".")...)
}

// Segments returns a copy of the path segments.
func (path Path) Segments() []string {
	return append([]string(nil), path.segments...)
}

// Len returns the number of path segments.
func (path Path) Len() int { return len(path.segments) }

// Segment returns segment index without copying the path.
func (path Path) Segment(index int) string { return path.segments[index] }

// AppendSegments appends the path segments to segments.
func (path Path) AppendSegments(segments []string) []string {
	return append(segments, path.segments...)
}

func (path Path) String() string {
	return strings.Join(path.segments, ".")
}

// MarshalJSON encodes a path in its canonical dot-separated form.
func (path Path) MarshalJSON() ([]byte, error) {
	if len(path.segments) == 0 {
		return nil, fmt.Errorf("cannot marshal an empty query path")
	}
	return json.Marshal(path.String())
}

// UnmarshalJSON validates the canonical string before replacing the path.
func (path *Path) UnmarshalJSON(encoded []byte) error {
	var value string
	if err := json.Unmarshal(encoded, &value); err != nil {
		return fmt.Errorf("decode query path: %w", err)
	}
	parsed, err := ParsePath(value)
	if err != nil {
		return err
	}
	*path = parsed
	return nil
}

// pathOf converts a FieldPath, panicking on a malformed name written in code.
func pathOf[P FieldPath](path P) Path {
	if typed, ok := any(path).(Path); ok {
		return typed
	}
	name, ok := any(path).(string)
	if !ok {
		name = reflect.ValueOf(path).String()
	}
	parsed, err := ParsePath(name)
	if err != nil {
		panic(err)
	}
	return parsed
}
