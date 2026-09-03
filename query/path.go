package query

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Path segments include authored field names and canonical block discriminators.
// Field names use letters, numbers, and underscores, while block keys are
// lowercase kebab-case and therefore require hyphens here.
var pathSegmentPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9_-]*$`)

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

// NewPath validates and constructs a field path from individual segments.
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
		if !pathSegmentPattern.MatchString(segment) {
			return Path{}, fmt.Errorf("query path segment %d %q must match %s", index, segment, pathSegmentPattern)
		}
		totalBytes += len(segment)
	}
	if totalBytes > MaxPathBytes {
		return Path{}, fmt.Errorf("query path exceeds %d bytes", MaxPathBytes)
	}
	return Path{segments: cloned}, nil
}

// ParsePath validates and constructs a path from its dot-separated form.
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
