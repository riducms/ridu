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

// Placement IDs are persisted field identities; their spelling must not change.
func TestPlacementFieldIDIsStable(t *testing.T) {
	for _, test := range []struct {
		path []string
		want StableID
	}{
		{[]string{"title"}, "pages-title"},
		{[]string{"seo", "metaTitle"}, "pages-seo-meta-title"},
		{[]string{"SEOTitle"}, "pages-s-e-o-title"},
		{[]string{"HTML5Parser"}, "pages-h-t-m-l5-parser"},
		{[]string{"snake_case_name"}, "pages-snake-case-name"},
		{[]string{"__lead", "trail__"}, "pages-lead-trail-"},
		{[]string{"a--b", "a_-_b"}, "pages-a-b-a-b"},
		{[]string{"_Upper"}, "pages-upper"},
		{[]string{"Ärger", "fooÄr"}, "pages-ärger-foo-är"},
		{[]string{"x\xffy"}, "pages-x�y"},
		{[]string{"", "a"}, "pages--a"},
		{[]string{"layout", "content-3", "links", "url"}, "pages-layout-content-3-links-url"},
		{nil, "pages"},
	} {
		if got := PlacementFieldID("pages", test.path); got != test.want {
			t.Errorf("PlacementFieldID(%q) = %q, want %q", test.path, got, test.want)
		}
	}
}
