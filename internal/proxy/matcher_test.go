package proxy

import "testing"

func TestPathMatcher(t *testing.T) {
	customRules := []string{
		".pdf",
		"/api/webhooks/*",
		"/healthz",
	}
	pm := NewPathMatcher(customRules)

	tests := []struct {
		path     string
		expected bool
	}{
		// Default static assets
		{"/static/main.css", true},
		{"/js/bundle.js", true},
		{"/images/logo.png", true},
		{"/favicon.ico", true},
		{"/robots.txt", true},
		{"/fonts/inter.woff2", true},

		// Custom rules
		{"/docs/whitepaper.pdf", true},
		{"/api/webhooks/stripe", true},
		{"/api/webhooks/github/push", true},
		{"/healthz", true},

		// Pages that MUST be protected
		{"/", false},
		{"/checkout", false},
		{"/api/v1/tickets", false},
		{"/concert/booking", false},
		{"/healthz/details", false}, // exact match "/healthz" shouldn't match subpath
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			actual := pm.ShouldBypass(tc.path)
			if actual != tc.expected {
				t.Fatalf("path %s: expected bypass=%v, got %v", tc.path, tc.expected, actual)
			}
		})
	}
}
