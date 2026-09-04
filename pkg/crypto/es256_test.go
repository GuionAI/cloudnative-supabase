package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
)

func TestPublicJWKSAndES256Verification(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	private := JWK{
		Kty: "EC", Alg: ES256, Use: "sig", Crv: "P-256", Kid: "fixture", KeyOps: []string{"sign"},
		X: encodeCoordinate(key.X), Y: encodeCoordinate(key.Y), D: encodeCoordinate(key.D),
	}
	token := signFixtureJWT(t, key, "anon", RequiredRoleAudience)
	if err := VerifyES256JWT(token, private, "anon", RequiredRoleAudience); err != nil {
		t.Fatalf("VerifyES256JWT() error = %v", err)
	}
	public, err := PublicJWK(private)
	if err != nil {
		t.Fatal(err)
	}
	if public.D != "" || public.Kty != "EC" || public.Alg != ES256 {
		t.Fatalf("public JWK leaked private material: %#v", public)
	}
	if err := VerifyES256JWTWithPublicKey(token, public, "anon", RequiredRoleAudience); err != nil {
		t.Fatalf("VerifyES256JWTWithPublicKey() error = %v", err)
	}
	jwks, err := PublicJWKS(mustJSON(t, []JWK{private}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(jwks, `"d"`) || strings.Contains(jwks, `"kty":"oct"`) {
		t.Fatalf("public JWKS contains private/symmetric material: %s", jwks)
	}
}

func TestVerifyES256RejectsHS256(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	private := JWK{
		Kty: "EC", Alg: ES256, Crv: "P-256", Kid: "fixture",
		X: encodeCoordinate(key.X), Y: encodeCoordinate(key.Y), D: encodeCoordinate(key.D),
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","kid":"fixture"}`))
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"role":"anon","aud":"authenticated","exp":4102444800}`))
	token := header + "." + claims + ".invalid"
	if err := VerifyES256JWT(token, private, "anon", RequiredRoleAudience); err == nil {
		t.Fatal("HS256 token was accepted by ES256 verifier")
	}
}

func signFixtureJWT(t *testing.T, key *ecdsa.PrivateKey, role, audience string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256","typ":"JWT","kid":"fixture"}`))
	claimsJSON := []byte(`{"role":"` + role + `","aud":"` + audience + `","exp":4102444800}`)
	claims := base64.RawURLEncoding.EncodeToString(claimsJSON)
	message := header + "." + claims
	digest := sha256.Sum256([]byte(message))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	signature := make([]byte, 64)
	copy(signature[32-len(r.Bytes()):32], r.Bytes())
	copy(signature[64-len(s.Bytes()):], s.Bytes())
	return message + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func encodeCoordinate(value *big.Int) string {
	bytes := make([]byte, 32)
	encoded := value.Bytes()
	copy(bytes[len(bytes)-len(encoded):], encoded)
	return base64.RawURLEncoding.EncodeToString(bytes)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
