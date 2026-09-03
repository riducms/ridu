package graphql

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestTokenAcceptsSupportedSchemesCaseInsensitively(t *testing.T) {
	for _, authorization := range []string{
		"Bearer token-value",
		"bEaReR    token-value",
		"JWT token-value",
		"jWt\t token-value",
		"Session token-value",
		"sEsSiOn \t token-value",
	} {
		t.Run(authorization, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/api/graphql", nil)
			request.Header.Set("Authorization", authorization)
			if token := requestToken(request); token != "token-value" {
				t.Fatalf("token = %q", token)
			}
		})
	}
}

func TestRequestTokenRejectsMalformedAuthorizationAndKeepsCookieFallback(t *testing.T) {
	for _, authorization := range []string{"Bearer", "Bearer one two", "Basic token-value"} {
		t.Run(authorization, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/api/graphql", nil)
			request.Header.Set("Authorization", authorization)
			request.AddCookie(&http.Cookie{Name: "ridu_session", Value: "cookie-token"})
			if token := requestToken(request); token != "cookie-token" {
				t.Fatalf("fallback token = %q", token)
			}
		})
	}
}
