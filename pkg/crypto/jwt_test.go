/*
Copyright 2026 GuionAI.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package crypto

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGenerateJWTSecret(t *testing.T) {
	secret, err := GenerateJWTSecret()
	if err != nil {
		t.Fatalf("GenerateJWTSecret() error = %v", err)
	}

	// Should be 64 hex characters (32 bytes)
	if len(secret) != 64 {
		t.Errorf("GenerateJWTSecret() length = %d, want 64", len(secret))
	}

	// Should only contain hex characters
	for _, c := range secret {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("GenerateJWTSecret() contains non-hex character: %c", c)
		}
	}
}

func TestCreateJWT(t *testing.T) {
	secret := "test-secret-key-32-bytes-long!!"

	tests := []struct {
		name       string
		role       string
		expSeconds int
		wantRole   string
	}{
		{
			name:       "anon role with default expiration",
			role:       "anon",
			expSeconds: 0,
			wantRole:   "anon",
		},
		{
			name:       "service_role with default expiration",
			role:       "service_role",
			expSeconds: 0,
			wantRole:   "service_role",
		},
		{
			name:       "authenticated with custom expiration",
			role:       "authenticated",
			expSeconds: 3600,
			wantRole:   "authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := CreateJWT(tt.role, secret, tt.expSeconds)
			if err != nil {
				t.Fatalf("CreateJWT() error = %v", err)
			}

			// JWT should have 3 parts
			parts := strings.Split(token, ".")
			if len(parts) != 3 {
				t.Fatalf("CreateJWT() token has %d parts, want 3", len(parts))
			}

			// Decode and verify header
			headerJSON, err := base64URLDecode(parts[0])
			if err != nil {
				t.Fatalf("Failed to decode header: %v", err)
			}
			var header JWTHeader
			if err := json.Unmarshal(headerJSON, &header); err != nil {
				t.Fatalf("Failed to unmarshal header: %v", err)
			}
			if header.Alg != "HS256" {
				t.Errorf("header.Alg = %s, want HS256", header.Alg)
			}
			if header.Typ != "JWT" {
				t.Errorf("header.Typ = %s, want JWT", header.Typ)
			}

			// Decode and verify claims
			claimsJSON, err := base64URLDecode(parts[1])
			if err != nil {
				t.Fatalf("Failed to decode claims: %v", err)
			}
			var claims JWTClaims
			if err := json.Unmarshal(claimsJSON, &claims); err != nil {
				t.Fatalf("Failed to unmarshal claims: %v", err)
			}
			if claims.Role != tt.wantRole {
				t.Errorf("claims.Role = %s, want %s", claims.Role, tt.wantRole)
			}
			if claims.Iss != DefaultJWTIssuer {
				t.Errorf("claims.Iss = %s, want %s", claims.Iss, DefaultJWTIssuer)
			}
			if claims.Exp <= claims.Iat {
				t.Errorf("claims.Exp (%d) should be greater than claims.Iat (%d)", claims.Exp, claims.Iat)
			}

			// Verify expiration
			now := time.Now().Unix()
			if tt.expSeconds == 0 {
				// Should be approximately 5 years from now
				expectedExp := now + DefaultTokenExpiration
				if claims.Exp < expectedExp-10 || claims.Exp > expectedExp+10 {
					t.Errorf("claims.Exp = %d, want ~%d", claims.Exp, expectedExp)
				}
			} else {
				expectedExp := now + int64(tt.expSeconds)
				if claims.Exp < expectedExp-10 || claims.Exp > expectedExp+10 {
					t.Errorf("claims.Exp = %d, want ~%d", claims.Exp, expectedExp)
				}
			}
		})
	}
}

func TestCreateAnonKey(t *testing.T) {
	secret := "test-secret"
	token, err := CreateAnonKey(secret, 0)
	if err != nil {
		t.Fatalf("CreateAnonKey() error = %v", err)
	}

	parts := strings.Split(token, ".")
	claimsJSON, _ := base64URLDecode(parts[1])
	var claims JWTClaims
	json.Unmarshal(claimsJSON, &claims)

	if claims.Role != "anon" {
		t.Errorf("CreateAnonKey() role = %s, want anon", claims.Role)
	}
}

func TestCreateServiceKey(t *testing.T) {
	secret := "test-secret"
	token, err := CreateServiceKey(secret, 0)
	if err != nil {
		t.Fatalf("CreateServiceKey() error = %v", err)
	}

	parts := strings.Split(token, ".")
	claimsJSON, _ := base64URLDecode(parts[1])
	var claims JWTClaims
	json.Unmarshal(claimsJSON, &claims)

	if claims.Role != "service_role" {
		t.Errorf("CreateServiceKey() role = %s, want service_role", claims.Role)
	}
}

// base64URLDecode decodes base64url encoded data
func base64URLDecode(s string) ([]byte, error) {
	// Add padding if needed
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	// Convert from base64url to base64
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	return base64.StdEncoding.DecodeString(s)
}
