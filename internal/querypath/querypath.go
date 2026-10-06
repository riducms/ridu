// Package querypath holds the caller query-path rules that the operation
// engine and the store adapters share.
package querypath

import (
	"fmt"

	"github.com/riducms/ridu/query"
)

// UnsupportedError reports a caller's filter path that a store adapter cannot
// compile. The operation engine rejects it as a bad query naming Path.
// Adapters wrap only caller filters: a path in a collection access rule is
// trusted configuration, so its failure remains an ordinary store failure.
type UnsupportedError struct {
	Path  query.Path
	Cause error
}

func (err *UnsupportedError) Error() string {
	return fmt.Sprintf("query path %q is not supported by this store: %v", err.Path.String(), err.Cause)
}

func (err *UnsupportedError) Unwrap() error { return err.Cause }

// Unsupported wraps an adapter's path-resolution failure for a caller filter.
func Unsupported(path query.Path, cause error) error {
	if cause == nil {
		return nil
	}
	return &UnsupportedError{Path: path, Cause: cause}
}
