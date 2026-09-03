package store

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxDocumentIDBytes keeps canonical IDs representable anywhere Ridu persists
// a document reference, including scheduled publishing and auth-owned tasks.
const MaxDocumentIDBytes = 512

// ValidateDocumentID validates the canonical adapter-neutral document ID.
// IDs are preserved byte-for-byte; only representations that cannot safely
// cross Ridu's JSON and compound-key boundaries are rejected.
func ValidateDocumentID(id string) error {
	switch {
	case id == "":
		return fmt.Errorf("document ID must not be empty")
	case !utf8.ValidString(id):
		return fmt.Errorf("document ID must be valid UTF-8")
	case strings.IndexByte(id, 0) >= 0:
		return fmt.Errorf("document ID must not contain NUL")
	case id == "." || id == "..":
		return fmt.Errorf("document ID must not be a URL dot segment")
	case len(id) > MaxDocumentIDBytes:
		return fmt.Errorf("document ID must be at most %d bytes", MaxDocumentIDBytes)
	default:
		return nil
	}
}
