package store

import "github.com/riducms/ridu/schema"

// SchemaRecoveryError reports stored content that cannot be interpreted safely
// under the active schema. Adapters must preserve the stored content and supply
// only schema paths and actionable diagnostics, never the unknown payload.
type SchemaRecoveryError struct {
	Issues []schema.Issue
}

// Error returns a payload-independent diagnostic. Inspect Issues for paths.
func (*SchemaRecoveryError) Error() string {
	return "stored content requires schema recovery"
}
