package httpapi

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	corsAllowedMethods = "POST, OPTIONS"
	corsAllowedHeaders = "Content-Type, Authorization"
)

func corsMiddleware(originPatterns []string) gin.HandlerFunc {
	patterns := append([]string(nil), originPatterns...)

	return func(c *gin.Context) {
		// WebSocket upgrades have their own origin validation in the room hub.
		if c.Request.Method != http.MethodPost && c.Request.Method != http.MethodOptions {
			c.Next()
			return
		}

		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}
		if !originAllowed(origin, patterns) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		if c.Request.Method == http.MethodOptions {
			if c.GetHeader("Access-Control-Request-Method") != http.MethodPost || !allowsSupportedHeaders(c.GetHeader("Access-Control-Request-Headers")) {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}

			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Methods", corsAllowedMethods)
			c.Header("Access-Control-Allow-Headers", corsAllowedHeaders)
			c.Header("Vary", "Origin")
			c.Status(http.StatusNoContent)
			c.Abort()
			return
		}

		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Vary", "Origin")
		c.Next()
	}
}

func allowsSupportedHeaders(value string) bool {
	if value == "" {
		return true
	}
	for _, header := range strings.Split(value, ",") {
		switch strings.ToLower(strings.TrimSpace(header)) {
		case "authorization", "content-type":
		default:
			return false
		}
	}
	return true
}

func originAllowed(origin string, patterns []string) bool {
	parsedOrigin, ok := parseOrigin(origin)
	if !ok {
		return false
	}
	for _, pattern := range patterns {
		if originMatchesPattern(parsedOrigin, strings.TrimSpace(pattern)) {
			return true
		}
	}
	return false
}

func originMatchesPattern(origin *url.URL, pattern string) bool {
	if pattern == "" || pattern == "*" {
		return false
	}
	if strings.HasSuffix(pattern, ":*") {
		base, ok := parseOrigin(strings.TrimSuffix(pattern, ":*"))
		return ok && base.Port() == "" && origin.Port() != "" && sameSchemeAndHost(origin, base)
	}

	allowedOrigin, ok := parseOrigin(pattern)
	return ok && sameOrigin(origin, allowedOrigin)
}

func parseOrigin(value string) (*url.URL, bool) {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, false
	}
	return parsed, true
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func sameSchemeAndHost(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Hostname(), right.Hostname())
}
