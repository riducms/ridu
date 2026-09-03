package schema

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// IsSupportedEndpointMethod reports whether method is part of the custom
// endpoint contract. Config resolution stores supported methods in uppercase.
func IsSupportedEndpointMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "CONNECT", "DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT":
		return true
	default:
		return false
	}
}

// IsValidEndpointPath reports whether path is a deterministic custom endpoint
// pattern. Paths begin with / and may use complete named segments such as
// :id. Query strings, fragments, traversal, empty segments, and partial
// parameters are rejected.
func IsValidEndpointPath(path string) bool {
	if path == "/" {
		return true
	}
	if path == "" || path != strings.TrimSpace(path) || !strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") || strings.Contains(path, "//") || strings.ContainsAny(path, "?#\\") || !utf8.ValidString(path) {
		return false
	}
	seen := make(map[string]struct{})
	for _, segment := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		if strings.HasPrefix(segment, ":") {
			name := strings.TrimPrefix(segment, ":")
			if !validEndpointParameterName(name) {
				return false
			}
			if _, duplicate := seen[name]; duplicate {
				return false
			}
			seen[name] = struct{}{}
			continue
		}
		if strings.ContainsAny(segment, ":*{}%") {
			return false
		}
		for _, character := range segment {
			if unicode.IsControl(character) || unicode.IsSpace(character) {
				return false
			}
		}
	}
	return true
}

func validEndpointParameterName(name string) bool {
	if name == "" {
		return false
	}
	for index, character := range name {
		if character == '_' || unicode.IsLetter(character) || index > 0 && unicode.IsDigit(character) {
			continue
		}
		return false
	}
	return true
}

func endpointPathShape(path string) string {
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for index, segment := range segments {
		if strings.HasPrefix(segment, ":") {
			segments[index] = ":"
		}
	}
	return "/" + strings.Join(segments, "/")
}
