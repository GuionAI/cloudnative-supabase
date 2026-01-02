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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// JWTHeader represents the JWT header
type JWTHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

// JWTClaims represents the JWT claims for Supabase tokens
type JWTClaims struct {
	Role string `json:"role"`
	Iss  string `json:"iss"`
	Iat  int64  `json:"iat"`
	Exp  int64  `json:"exp"`
}

// DefaultJWTIssuer is the default issuer for Supabase JWTs
const DefaultJWTIssuer = "supabase"

// DefaultTokenExpiration is 5 years in seconds (for anon/service_role keys)
const DefaultTokenExpiration = 157680000

// GenerateJWTSecret generates a 32-byte random secret as hex string
func GenerateJWTSecret() (string, error) {
	return GenerateHex(32)
}

// CreateJWT creates an HS256 JWT token for Supabase
// role: the Supabase role (anon, service_role, authenticated)
// secret: the JWT signing secret
// expSeconds: token expiration in seconds (0 = default 5 years)
func CreateJWT(role, secret string, expSeconds int) (string, error) {
	now := time.Now().Unix()

	exp := now + int64(expSeconds)
	if expSeconds == 0 {
		// Default to 5 years for anon/service_role keys
		exp = now + DefaultTokenExpiration
	}

	// Create header
	header := JWTHeader{
		Alg: "HS256",
		Typ: "JWT",
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	headerEncoded := base64URLEncode(headerJSON)

	// Create claims
	claims := JWTClaims{
		Role: role,
		Iss:  DefaultJWTIssuer,
		Iat:  now,
		Exp:  exp,
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payloadEncoded := base64URLEncode(claimsJSON)

	// Create signature
	signatureInput := headerEncoded + "." + payloadEncoded
	signature := signHS256(signatureInput, secret)

	return signatureInput + "." + signature, nil
}

// CreateAnonKey creates a JWT token for the anon role
// expSeconds: 0 = default 5 years
func CreateAnonKey(secret string, expSeconds int) (string, error) {
	return CreateJWT("anon", secret, expSeconds)
}

// CreateServiceKey creates a JWT token for the service_role
// expSeconds: 0 = default 5 years
func CreateServiceKey(secret string, expSeconds int) (string, error) {
	return CreateJWT("service_role", secret, expSeconds)
}

// base64URLEncode encodes data using base64url encoding without padding
func base64URLEncode(data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	// Convert to base64url format
	encoded = strings.ReplaceAll(encoded, "+", "-")
	encoded = strings.ReplaceAll(encoded, "/", "_")
	// Remove padding
	encoded = strings.TrimRight(encoded, "=")
	return encoded
}

// signHS256 creates an HMAC-SHA256 signature
func signHS256(data, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(data))
	return base64URLEncode(h.Sum(nil))
}
