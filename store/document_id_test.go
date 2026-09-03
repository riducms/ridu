package store_test

import (
	"strings"
	"testing"

	"github.com/riducms/ridu/store"
)

func TestValidateDocumentIDPreservesPayloadCompatibleStrings(t *testing.T) {
	for _, id := range []string{"1", "-42", "018f6b2a-9786-7c2e-bb2f-50d9f14c47a1", "article/custom key", strings.Repeat("x", store.MaxDocumentIDBytes)} {
		if err := store.ValidateDocumentID(id); err != nil {
			t.Fatalf("ValidateDocumentID(%q): %v", id, err)
		}
	}
}

func TestValidateDocumentIDRejectsUnsafeRepresentations(t *testing.T) {
	for _, id := range []string{"", ".", "..", "bad\x00id", string([]byte{0xff}), strings.Repeat("x", store.MaxDocumentIDBytes+1), strings.Repeat("界", 171)} {
		if err := store.ValidateDocumentID(id); err == nil {
			t.Fatalf("ValidateDocumentID(%q) succeeded", id)
		}
	}
}
