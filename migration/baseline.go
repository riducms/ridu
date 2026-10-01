package migration

import "errors"

// BaselineReplacementOptions controls an explicit rewrite of recorded history.
// PreviousDirectory must contain the immutable artifacts the database records;
// when empty, the replacement directory is also used as the previous history.
type BaselineReplacementOptions struct {
	PreviousDirectory string
	// AllowProduction permits rewriting the history of a database that is
	// exactly at its recorded head, where binaries built from that history may
	// still pass readiness. Stop every application process and writer first.
	AllowProduction bool
}

// ErrReplacementNeedsConfirmation reports a baseline replacement that would
// rewrite the history of a database exactly at its recorded head. Repeat the
// replacement with AllowProduction once its application processes are stopped.
var ErrReplacementNeedsConfirmation = errors.New("this database is exactly at its recorded " +
	"migration head, so an application binary built from the old history may be serving it; " +
	"after the replacement those binaries fail readiness until they are rebuilt from the new " +
	"history; stop every application process and writer first")
