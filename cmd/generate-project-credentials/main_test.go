package main

import (
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	projectsecrets "github.com/GuionAI/cloudnative-supabase/internal/resources/secrets"
)

func TestGeneratedCredentialBundlePassesOperatorValidation(t *testing.T) {
	bundle, err := generateCredentialBundle(time.Now(), rand.Reader)
	if err != nil {
		t.Fatalf("generateCredentialBundle() error = %v", err)
	}

	projection, err := projectsecrets.ValidateProjectCredentials(bundle.secret())
	if err != nil {
		t.Fatalf("generated bundle failed operator validation: %v", err)
	}
	if projection.SigningKeyID == "" || projection.PublicJWKS == "" {
		t.Fatal("generated bundle did not produce signing metadata")
	}
}

func TestCredentialBundleJSONContract(t *testing.T) {
	bundle, err := generateCredentialBundle(time.Now(), rand.Reader)
	if err != nil {
		t.Fatalf("generateCredentialBundle() error = %v", err)
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var values map[string]string
	if err := json.Unmarshal(encoded, &values); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(values) != len(projectsecrets.RequiredProjectCredentialKeys) {
		t.Fatalf("JSON field count = %d, want %d", len(values), len(projectsecrets.RequiredProjectCredentialKeys))
	}
	for _, key := range projectsecrets.RequiredProjectCredentialKeys {
		if values[key] == "" {
			t.Fatalf("JSON field %q is missing or empty", key)
		}
	}
}
