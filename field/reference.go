package field

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/riducms/ridu/schema"
)

// ReferenceScope identifies the object from which a field reference is resolved.
type ReferenceScope string

const (
	SiblingScope ReferenceScope = "sibling"
	RootScope    ReferenceScope = "root"
)

// Reference is an immutable field selector. Its dot-separated path names stored
// fields through non-repeated groups or named tabs, excluding presentation
// wrappers and repeated row selectors. Each use resolves in its own field scope.
type Reference struct {
	scope ReferenceScope
	path  string
}

// Sibling selects a field path in the enclosing object or repeated row.
func Sibling(path string) Reference { return Reference{scope: SiblingScope, path: path} }

// Root selects a field path from the resource's document root.
func Root(path string) Reference { return Reference{scope: RootScope, path: path} }

// Scope returns the reference's starting object scope.
func (r Reference) Scope() ReferenceScope { return r.scope }

// Path returns the authored dot-separated path relative to the starting scope.
func (r Reference) Path() string { return r.path }

// IsZero reports an unspecified reference.
func (r Reference) IsZero() bool { return r.scope == "" && r.path == "" }

// Err checks path structure. Configuration resolution checks target existence
// and rejects paths that cross repeated fields or end at incompatible values.
func (r Reference) Err() error {
	if r.scope != SiblingScope && r.scope != RootScope {
		return fmt.Errorf("reference scope %q must be sibling or root", r.scope)
	}
	segments := strings.Split(r.path, ".")
	if len(segments) > 64 || len(r.path) > 4096 {
		return fmt.Errorf("reference path must contain at most 64 field names and 4096 bytes")
	}
	for _, segment := range segments {
		if !schema.IsValidFieldName(segment) {
			return fmt.Errorf("reference path %q must contain authored field names separated by dots, without repeated row selectors", r.path)
		}
	}
	return nil
}

// MarshalJSON retains the authored scope and path.
func (r Reference) MarshalJSON() ([]byte, error) {
	if err := r.Err(); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Scope ReferenceScope `json:"scope"`
		Path  string         `json:"path"`
	}{Scope: r.scope, Path: r.path})
}
