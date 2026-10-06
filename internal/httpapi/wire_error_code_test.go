package httpapi

import (
	"net/http"
	"testing"

	"github.com/riducms/ridu/protocol"
)

// A code the protocol doesn't name falls back to the category of its status,
// so a client never reads bad_request beside a 403, 404, 409 or 5xx.
func TestWireErrorCodeNeverContradictsItsStatus(t *testing.T) {
	for _, test := range []struct {
		code   string
		status int
		want   protocol.ErrorCode
	}{
		{"bad_query", http.StatusBadRequest, protocol.ErrorBadQuery},
		{"publish_required", http.StatusConflict, protocol.ErrorPublishRequired},
		{"unknown_collection", http.StatusNotFound, protocol.ErrorNotFound},
		{"origin_denied", http.StatusForbidden, protocol.ErrorAccess},
		{"transaction_aborted", http.StatusConflict, protocol.ErrorConflict},
		{"body_too_large", http.StatusRequestEntityTooLarge, protocol.ErrorBadRequest},
		{"unsupported_media_type", http.StatusUnsupportedMediaType, protocol.ErrorBadRequest},
		{"storage_unavailable", http.StatusServiceUnavailable, protocol.ErrorInternal},
	} {
		if got := wireErrorCode(test.code, test.status); got != test.want {
			t.Errorf("wireErrorCode(%q, %d) = %q, want %q", test.code, test.status, got, test.want)
		}
	}
}
