package proxy

import (
	"path"
	"strings"
)

var defaultStaticExtensions = map[string]struct{}{
	".css":         {},
	".js":          {},
	".png":         {},
	".jpg":         {},
	".jpeg":        {},
	".gif":         {},
	".svg":         {},
	".ico":         {},
	".woff":        {},
	".woff2":       {},
	".ttf":         {},
	".eot":         {},
	".webp":        {},
	".map":         {},
	".webmanifest": {},
	".mp4":         {},
	".webm":        {},
}

// PathMatcher determines whether incoming request paths should bypass queueing.
type PathMatcher struct {
	exactPaths map[string]struct{}
	prefixes   []string
	extensions map[string]struct{}
}

// NewPathMatcher builds a new path whitelist matcher with default static assets and custom paths.
func NewPathMatcher(customBypass []string) *PathMatcher {
	pm := &PathMatcher{
		exactPaths: map[string]struct{}{
			"/favicon.ico": {},
			"/robots.txt":  {},
		},
		prefixes:   []string{},
		extensions: make(map[string]struct{}, len(defaultStaticExtensions)),
	}

	for ext := range defaultStaticExtensions {
		pm.extensions[ext] = struct{}{}
	}

	for _, rule := range customBypass {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			continue
		}

		if strings.HasPrefix(rule, ".") {
			// Extension rule e.g. ".pdf"
			pm.extensions[strings.ToLower(rule)] = struct{}{}
		} else if strings.HasSuffix(rule, "*") {
			// Prefix wildcard rule e.g. "/api/webhook/*"
			prefix := strings.TrimSuffix(rule, "*")
			pm.prefixes = append(pm.prefixes, prefix)
		} else {
			// Exact path rule e.g. "/healthz"
			pm.exactPaths[rule] = struct{}{}
		}
	}

	return pm
}

// ShouldBypass returns true if the request path matches static assets or whitelist rules.
func (pm *PathMatcher) ShouldBypass(rawPath string) bool {
	cleanPath := path.Clean(rawPath)

	// Check exact matches
	if _, ok := pm.exactPaths[cleanPath]; ok {
		return true
	}

	// Check prefix matches
	for _, prefix := range pm.prefixes {
		if strings.HasPrefix(cleanPath, prefix) {
			return true
		}
	}

	// Check file extension
	ext := strings.ToLower(path.Ext(cleanPath))
	if ext != "" {
		if _, ok := pm.extensions[ext]; ok {
			return true
		}
	}

	return false
}
