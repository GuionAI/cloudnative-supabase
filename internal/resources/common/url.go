package common

import (
	"fmt"
	"net/url"
	"strings"
)

// NormalizeExternalURL returns an absolute URL with a stable, slash-free path
// representation. It is used for GoTrue's issuer and for derived public URLs.
func NormalizeExternalURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimRight(strings.TrimSpace(raw), "/")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	// An issuer and its JWKS endpoint are origin/path identifiers, not query
	// resources. Drop fragments and queries so equivalent external URLs derive
	// the same verifier endpoint.
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/")
}

// AuthJWKSURL derives the public Auth JWKS endpoint from the configured Auth
// external URL. If the URL already includes /auth/v1, it is preserved.
func AuthJWKSURL(externalURL string) string {
	base := NormalizeExternalURL(externalURL)
	if strings.HasSuffix(base, "/auth/v1") {
		return base + "/.well-known/jwks.json"
	}
	return fmt.Sprintf("%s/auth/v1/.well-known/jwks.json", base)
}
