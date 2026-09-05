package secrets

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

const (
	fixturePublishableKey = "sb_publishable_A1_b2-C3d4_E5f6-G7h8I9_oWzQ-j5j"
	fixtureSecretKey      = "sb_secret_Z9_y8-X7w6_V5u4-T3s2_R_6LqoZ8QA"
)

func TestValidateProjectCredentials(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	private := map[string]any{
		"kty": "EC", "alg": "ES256", "use": "sig", "crv": "P-256", "kid": "fixture", "key_ops": []string{"sign"},
		"x": coordinate(key.X), "y": coordinate(key.Y), "d": coordinate(key.D),
	}
	bundle := map[string]string{
		ProjectCredentialsSigningKeysKey:    mustMarshal(t, []any{private}),
		ProjectCredentialsPublishableKey:    fixturePublishableKey,
		ProjectCredentialsSecretKey:         fixtureSecretKey,
		ProjectCredentialsAnonRoleJWTKey:    signRole(t, key, "anon"),
		ProjectCredentialsServiceRoleJWTKey: signRole(t, key, "service_role"),
	}
	if len(bundle[ProjectCredentialsPublishableKey]) != 46 || len(bundle[ProjectCredentialsSecretKey]) != 41 {
		t.Fatalf("canonical fixture lengths = %d and %d, want 46 and 41", len(bundle[ProjectCredentialsPublishableKey]), len(bundle[ProjectCredentialsSecretKey]))
	}
	projection, err := ValidateProjectCredentials(&corev1.Secret{Data: stringData(bundle)})
	if err != nil {
		t.Fatalf("ValidateProjectCredentials() error = %v", err)
	}
	if projection.SigningKeyID != "fixture" || projection.PodTemplateHash == "" {
		t.Fatalf("projection metadata = %#v", projection)
	}
	if strings.Contains(projection.PublicJWKS, `"d"`) || strings.Contains(projection.PublicJWKS, `"oct"`) {
		t.Fatalf("public projection contains private material: %s", projection.PublicJWKS)
	}

	mutations := map[string]func(map[string]string){
		"malformed JSON": func(values map[string]string) { values[ProjectCredentialsSigningKeysKey] = "{" },
		"missing private material": func(values map[string]string) {
			values[ProjectCredentialsSigningKeysKey] = `[{"kty":"EC","alg":"ES256","use":"sig","crv":"P-256","kid":"fixture","key_ops":["sign"]}]`
		},
		"invalid private material": func(values map[string]string) {
			values[ProjectCredentialsSigningKeysKey] = strings.Replace(values[ProjectCredentialsSigningKeysKey], private["d"].(string), "not-base64", 1)
		},
		"mismatched kid": func(values map[string]string) {
			values[ProjectCredentialsAnonRoleJWTKey] = signRoleWithClaims(t, key, "anon", "authenticated", "other", 4102444800)
		},
		"bad signature": func(values map[string]string) {
			token := values[ProjectCredentialsAnonRoleJWTKey]
			parts := strings.Split(token, ".")
			signature := []byte(parts[2])
			replacement := byte('A')
			if signature[0] == replacement {
				replacement = 'B'
			}
			signature[0] = replacement
			parts[2] = string(signature)
			values[ProjectCredentialsAnonRoleJWTKey] = strings.Join(parts, ".")
		},
		"wrong role": func(values map[string]string) {
			values[ProjectCredentialsAnonRoleJWTKey] = signRoleWithClaims(t, key, "service_role", "authenticated", "fixture", 4102444800)
		},
		"wrong audience": func(values map[string]string) {
			values[ProjectCredentialsAnonRoleJWTKey] = signRoleWithClaims(t, key, "anon", "other", "fixture", 4102444800)
		},
		"expired": func(values map[string]string) {
			values[ProjectCredentialsAnonRoleJWTKey] = signRoleWithClaims(t, key, "anon", "authenticated", "fixture", 1)
		},
		"wrong secret prefix": func(values map[string]string) {
			values[ProjectCredentialsSecretKey] = fixturePublishableKey
		},
		"wrong publishable prefix": func(values map[string]string) {
			values[ProjectCredentialsPublishableKey] = "jwt_publishable_A1_b2-C3d4_E5f6-G7h8I9_oWzQ-j5j"
		},
		"short random segment": func(values map[string]string) {
			values[ProjectCredentialsPublishableKey] = canonicalOpaqueFixture("sb_publishable_", "short")
		},
		"long random segment": func(values map[string]string) {
			values[ProjectCredentialsPublishableKey] = canonicalOpaqueFixture("sb_publishable_", "ABCDEFGHIJKLMNOPQRSTUVW")
		},
		"short checksum": func(values map[string]string) {
			values[ProjectCredentialsPublishableKey] = "sb_publishable_A1_b2-C3d4_E5f6-G7h8I9_oWzQ-j5"
		},
		"long checksum": func(values map[string]string) {
			values[ProjectCredentialsPublishableKey] = "sb_publishable_A1_b2-C3d4_E5f6-G7h8I9_oWzQ-j5jA"
		},
		"invalid random character": func(values map[string]string) {
			values[ProjectCredentialsPublishableKey] = canonicalOpaqueFixture("sb_publishable_", "A1_b2-C3d4_E5f6-G7h8I!")
		},
		"invalid checksum character": func(values map[string]string) {
			values[ProjectCredentialsPublishableKey] = "sb_publishable_A1_b2-C3d4_E5f6-G7h8I9_oWzQ-j!j"
		},
		"misplaced fixed-boundary separator": func(values map[string]string) {
			key := []byte(fixturePublishableKey)
			key[len("sb_publishable_")+5] = '_'
			key[len("sb_publishable_")+22] = 'A'
			values[ProjectCredentialsPublishableKey] = string(key)
		},
		"checksum mismatch": func(values map[string]string) {
			key := []byte(fixturePublishableKey)
			key[len(key)-1] = 'A'
			values[ProjectCredentialsPublishableKey] = string(key)
		},
	}
	for _, field := range RequiredProjectCredentialKeys {
		mutations["missing "+field] = func(values map[string]string) { delete(values, field) }
	}
	for field, mutate := range mutations {
		t.Run(field, func(t *testing.T) {
			values := cloneStrings(bundle)
			mutate(values)
			_, err := ValidateProjectCredentials(&corev1.Secret{Data: stringData(values)})
			if err == nil {
				t.Fatal("invalid credential bundle was accepted")
			}
			if strings.Contains(err.Error(), fixturePublishableKey) || strings.Contains(err.Error(), fixtureSecretKey) || strings.Contains(err.Error(), "eyJ") {
				t.Fatalf("validation error exposed credential material: %v", err)
			}
		})
	}
}

func canonicalOpaqueFixture(prefix, random string) string {
	digest := sha256.Sum256([]byte("supabase-self-hosted|" + prefix + random))
	checksum := base64.RawURLEncoding.EncodeToString(digest[:])[:8]
	return prefix + random + "_" + checksum
}

func stringData(values map[string]string) map[string][]byte {
	result := make(map[string][]byte, len(values))
	for key, value := range values {
		result[key] = []byte(value)
	}
	return result
}

func cloneStrings(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func coordinate(value *big.Int) string {
	bytes := make([]byte, 32)
	encoded := value.Bytes()
	copy(bytes[len(bytes)-len(encoded):], encoded)
	return base64.RawURLEncoding.EncodeToString(bytes)
}

func signRole(t *testing.T, key *ecdsa.PrivateKey, role string) string {
	t.Helper()
	return signRoleWithClaims(t, key, role, "authenticated", "fixture", 4102444800)
}

func signRoleWithClaims(t *testing.T, key *ecdsa.PrivateKey, role, audience, kid string, expiration int64) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"alg":"ES256","kid":"%s"}`, kid)))
	claims := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"role":"%s","aud":"%s","exp":%d}`, role, audience, expiration)))
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

func mustMarshal(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
