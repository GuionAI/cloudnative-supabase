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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// JWK is the subset of a JSON Web Key needed by the operator. Unknown JWK
// members are deliberately ignored when parsing so upstream metadata can be
// retained by callers while the security-critical fields are validated here.
type JWK struct {
	Kty    string   `json:"kty"`
	Alg    string   `json:"alg,omitempty"`
	Use    string   `json:"use,omitempty"`
	Crv    string   `json:"crv,omitempty"`
	X      string   `json:"x,omitempty"`
	Y      string   `json:"y,omitempty"`
	D      string   `json:"d,omitempty"`
	Kid    string   `json:"kid,omitempty"`
	KeyOps []string `json:"key_ops,omitempty"`
	Ext    *bool    `json:"ext,omitempty"`
}

// JWTHeader contains the JOSE fields required for ES256 verification.
type JWTHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ,omitempty"`
	Kid string `json:"kid"`
}

// JWTClaims contains the role claims used by Supabase internal credentials.
// Audience is decoded as either a string or an array by ParseJWTClaims.
type JWTClaims struct {
	Role string `json:"role"`
	Aud  any    `json:"aud"`
	Iss  string `json:"iss,omitempty"`
	Iat  int64  `json:"iat,omitempty"`
	Exp  int64  `json:"exp"`
}

const (
	// ES256 is the only signing algorithm accepted by the modern platform.
	ES256 = "ES256"
	// RequiredRoleAudience is the audience used by Supabase role tokens.
	RequiredRoleAudience = "authenticated"
)

var rawURL = base64.RawURLEncoding

// ParseJWKArray parses a JWK JSON array and requires exactly one key.
func ParseJWKArray(raw string) ([]JWK, error) {
	var keys []JWK
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		return nil, fmt.Errorf("must be valid JSON: %w", err)
	}
	if len(keys) != 1 {
		return nil, fmt.Errorf("must contain exactly one key")
	}
	return keys, nil
}

// ParseSigningJWK validates and returns the sole signing-capable P-256 key.
func ParseSigningJWK(raw string) (JWK, error) {
	keys, err := ParseJWKArray(raw)
	if err != nil {
		return JWK{}, err
	}
	key := keys[0]
	if err := ValidateSigningJWK(key); err != nil {
		return JWK{}, err
	}
	return key, nil
}

// ValidateSigningJWK validates the cryptographic material and signing use of
// a private P-256 ES256 JWK.
func ValidateSigningJWK(key JWK) error {
	if key.Kty != "EC" {
		return errors.New("kty must be EC")
	}
	if key.Crv != "P-256" {
		return errors.New("crv must be P-256")
	}
	if key.Alg != ES256 {
		return errors.New("alg must be ES256")
	}
	if key.Use != "" && key.Use != "sig" {
		return errors.New("use must be sig")
	}
	if key.Kid == "" {
		return errors.New("kid must be non-empty")
	}
	if len(key.KeyOps) > 0 && !contains(key.KeyOps, "sign") {
		return errors.New("key_ops must include sign")
	}
	if key.X == "" || key.Y == "" || key.D == "" {
		return errors.New("x, y, and d are required")
	}
	if _, err := ecdsaPrivateKey(key); err != nil {
		return err
	}
	return nil
}

// PublicJWK returns a verifier-only projection of a private signing key.
func PublicJWK(key JWK) (JWK, error) {
	if err := ValidateSigningJWK(key); err != nil {
		return JWK{}, err
	}
	private, err := ecdsaPrivateKey(key)
	if err != nil {
		return JWK{}, err
	}
	public := JWK{
		Kty:    "EC",
		Alg:    ES256,
		Use:    "sig",
		Crv:    "P-256",
		X:      rawURL.EncodeToString(private.X.Bytes()),
		Y:      rawURL.EncodeToString(private.Y.Bytes()),
		Kid:    key.Kid,
		KeyOps: []string{"verify"},
	}
	// JWK coordinates are fixed-width 32-byte unsigned integers. big.Int.Bytes
	// drops leading zeroes, so pad them before encoding.
	public.X = rawURL.EncodeToString(paddedCoordinate(private.X))
	public.Y = rawURL.EncodeToString(paddedCoordinate(private.Y))
	if key.Ext != nil {
		public.Ext = key.Ext
	}
	return public, nil
}

// PublicJWKS returns the canonical public-only JWKS representation. It never
// contains private material and always emits an object with a keys array.
func PublicJWKS(raw string) (string, error) {
	key, err := ParseSigningJWK(raw)
	if err != nil {
		return "", err
	}
	public, err := PublicJWK(key)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(struct {
		Keys []JWK `json:"keys"`
	}{Keys: []JWK{public}})
	if err != nil {
		return "", fmt.Errorf("marshalling public JWKS: %w", err)
	}
	return string(encoded), nil
}

// VerifyES256JWT validates an ES256 JWT against a public key and expected role
// and audience. It rejects all other algorithms, malformed signatures, and
// expired tokens.
func VerifyES256JWT(token string, key JWK, expectedRole, expectedAudience string) error {
	if err := ValidateSigningJWK(key); err != nil {
		return err
	}
	private, err := ecdsaPrivateKey(key)
	if err != nil {
		return err
	}
	return verifyWithPublicKey(token, &private.PublicKey, key.Kid, expectedRole, expectedAudience)
}

// VerifyES256JWTWithPublicKey verifies a token using a public JWK projection.
func VerifyES256JWTWithPublicKey(token string, key JWK, expectedRole, expectedAudience string) error {
	if key.Kty != "EC" || key.Crv != "P-256" || key.Alg != ES256 || key.Kid == "" {
		return errors.New("public JWK must be a P-256 ES256 key with a kid")
	}
	public, err := ecdsaPublicKey(key)
	if err != nil {
		return err
	}
	return verifyWithPublicKey(token, public, key.Kid, expectedRole, expectedAudience)
}

func verifyWithPublicKey(
	token string,
	public *ecdsa.PublicKey,
	expectedKid, expectedRole, expectedAudience string,
) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errors.New("JWT must contain three segments")
	}
	headerJSON, err := decodeBase64URL(parts[0])
	if err != nil {
		return errors.New("JWT header is not valid base64url")
	}
	var header JWTHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return errors.New("JWT header is not valid JSON")
	}
	if header.Alg != ES256 {
		return errors.New("JWT alg must be ES256")
	}
	if header.Kid != expectedKid {
		return errors.New("JWT kid does not match signing key")
	}
	claimsJSON, err := decodeBase64URL(parts[1])
	if err != nil {
		return errors.New("JWT claims are not valid base64url")
	}
	var claims JWTClaims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return errors.New("JWT claims are not valid JSON")
	}
	if claims.Role != expectedRole {
		return errors.New("JWT role does not match expected role")
	}
	if !audienceMatches(claims.Aud, expectedAudience) {
		return errors.New("JWT audience does not match expected audience")
	}
	if claims.Exp <= time.Now().Unix() {
		return errors.New("JWT is expired")
	}
	signature, err := decodeBase64URL(parts[2])
	if err != nil || len(signature) != 64 {
		return errors.New("JWT signature is not a valid ES256 signature")
	}
	hash := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	if !ecdsa.Verify(public, hash[:], r, s) {
		return errors.New("JWT signature verification failed")
	}
	return nil
}

func audienceMatches(value any, expected string) bool {
	switch aud := value.(type) {
	case string:
		return aud == expected
	case []any:
		for _, item := range aud {
			if s, ok := item.(string); ok && s == expected {
				return true
			}
		}
	}
	return false
}

func ecdsaPrivateKey(key JWK) (*ecdsa.PrivateKey, error) {
	x, err := decodeCoordinate(key.X)
	if err != nil {
		return nil, errors.New("x is not a valid P-256 coordinate")
	}
	y, err := decodeCoordinate(key.Y)
	if err != nil {
		return nil, errors.New("y is not a valid P-256 coordinate")
	}
	d, err := decodeCoordinate(key.D)
	if err != nil {
		return nil, errors.New("d is not a valid P-256 private scalar")
	}
	curve := elliptic.P256()
	if !curve.IsOnCurve(x, y) {
		return nil, errors.New("public point is not on P-256")
	}
	if d.Sign() <= 0 || d.Cmp(curve.Params().N) >= 0 {
		return nil, errors.New("private scalar is outside P-256 range")
	}
	derivedX, derivedY := curve.ScalarBaseMult(d.Bytes())
	if derivedX.Cmp(x) != 0 || derivedY.Cmp(y) != 0 {
		return nil, errors.New("public coordinates do not match private scalar")
	}
	return &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y}, D: d}, nil
}

func ecdsaPublicKey(key JWK) (*ecdsa.PublicKey, error) {
	x, err := decodeCoordinate(key.X)
	if err != nil {
		return nil, errors.New("x is not a valid P-256 coordinate")
	}
	y, err := decodeCoordinate(key.Y)
	if err != nil {
		return nil, errors.New("y is not a valid P-256 coordinate")
	}
	curve := elliptic.P256()
	if !curve.IsOnCurve(x, y) {
		return nil, errors.New("public point is not on P-256")
	}
	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
}

func decodeCoordinate(value string) (*big.Int, error) {
	if value == "" {
		return nil, errors.New("empty coordinate")
	}
	decoded, err := decodeBase64URL(value)
	if err != nil || len(decoded) != 32 {
		return nil, errors.New("coordinate must be 32-byte base64url")
	}
	return new(big.Int).SetBytes(decoded), nil
}

func decodeBase64URL(value string) ([]byte, error) {
	decoded, err := rawURL.DecodeString(value)
	if err == nil {
		return decoded, nil
	}
	return base64.URLEncoding.DecodeString(value)
}

func paddedCoordinate(value *big.Int) []byte {
	result := make([]byte, 32)
	bytes := value.Bytes()
	copy(result[len(result)-len(bytes):], bytes)
	return result
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
