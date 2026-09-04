package secrets

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

	corev1 "k8s.io/api/core/v1"
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
		ProjectCredentialsPublishableKey:    "sb_publishable_fixture",
		ProjectCredentialsSecretKey:         "sb_secret_fixture",
		ProjectCredentialsAnonRoleJWTKey:    signRole(t, key, "anon"),
		ProjectCredentialsServiceRoleJWTKey: signRole(t, key, "service_role"),
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

	for field, mutate := range map[string]func(map[string]string){
		"missing field": func(values map[string]string) { delete(values, ProjectCredentialsSecretKey) },
		"wrong prefix":  func(values map[string]string) { values[ProjectCredentialsSecretKey] = "sb_publishable_other" },
		"wrong role": func(values map[string]string) {
			values[ProjectCredentialsAnonRoleJWTKey] = signRole(t, key, "service_role")
		},
	} {
		t.Run(field, func(t *testing.T) {
			values := cloneStrings(bundle)
			mutate(values)
			_, err := ValidateProjectCredentials(&corev1.Secret{Data: stringData(values)})
			if err == nil {
				t.Fatal("invalid credential bundle was accepted")
			}
			if strings.Contains(err.Error(), "sb_publishable_fixture") || strings.Contains(err.Error(), "sb_secret_fixture") || strings.Contains(err.Error(), "eyJ") {
				t.Fatalf("validation error exposed credential material: %v", err)
			}
		})
	}
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
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256","kid":"fixture"}`))
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"role":"` + role + `","aud":"authenticated","exp":4102444800}`))
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
