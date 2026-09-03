package store

import "strings"

// CanonicalAuthIdentity returns the provider-neutral storage and lookup key
// for an authentication identity. Auth identity equality is exact equality of
// this value; adapters must not substitute database collations or case-folding
// rules.
func CanonicalAuthIdentity(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
