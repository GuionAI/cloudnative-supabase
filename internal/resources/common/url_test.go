package common

import "testing"

func TestAuthJWKSURLNormalizesExternalURL(t *testing.T) {
	tests := map[string]string{
		"https://auth.example.com/":            "https://auth.example.com/auth/v1/.well-known/jwks.json",
		"https://auth.example.com/auth/v1/":    "https://auth.example.com/auth/v1/.well-known/jwks.json",
		"https://auth.example.com/auth/v1?x=1": "https://auth.example.com/auth/v1/.well-known/jwks.json",
	}
	for input, want := range tests {
		if got := AuthJWKSURL(input); got != want {
			t.Errorf("AuthJWKSURL(%q) = %q, want %q", input, got, want)
		}
	}
}
