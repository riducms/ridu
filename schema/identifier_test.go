package schema

import "testing"

func TestAdminPluginRoutesAreLiteralAndCannotShadowFrameworkRoutes(t *testing.T) {
	for _, value := range []string{"report", "reports/today", "MY_reports/Today"} {
		if !IsValidAdminPluginRoute(value) {
			t.Fatalf("rejected literal route %q", value)
		}
	}
	for _, value := range []string{"", "/reports", "reports/", "reports//today", "reports/../today", "reports?x", "reports/:id", "account/security", "ACCOUNT/security", "Collections/posts", "globals/site", "login", "create-first-user", "forgot-password", "reset-password", "request-verification", "verify-email"} {
		if IsValidAdminPluginRoute(value) {
			t.Fatalf("accepted ambiguous or reserved route %q", value)
		}
	}
}
