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

package secrets

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
	"github.com/GuionAI/cloudnative-supabase/pkg/crypto"
)

const (
	// EmailHookSecretKey is the fixed key containing the Standard Webhooks secret.
	EmailHookSecretKey = "secret"
	// ProjectCredentialsSigningKeysKey is the external bundle key consumed by GoTrue.
	ProjectCredentialsSigningKeysKey = "signingKeys"
	// ProjectCredentialsPublishableKey is the public opaque client key.
	ProjectCredentialsPublishableKey = "publishableKey"
	// ProjectCredentialsSecretKey is the private opaque backend key.
	ProjectCredentialsSecretKey = "secretKey"
	// ProjectCredentialsAnonRoleJWTKey is the internal anon role token.
	ProjectCredentialsAnonRoleJWTKey = "anonRoleJwt"
	// ProjectCredentialsServiceRoleJWTKey is the internal service role token.
	ProjectCredentialsServiceRoleJWTKey = "serviceRoleJwt"
	// GoTrueFallbackSecretKey is the key used by the GoTrue-only fallback Secret.
	GoTrueFallbackSecretKey = "secret"
	// GoTrueFallbackSecretNameSuffix is appended to a project name for the
	// create-once fallback Secret.
	GoTrueFallbackSecretNameSuffix = "-gotrue-jwt-secret"
)

const emailHookSecretBytes = 32

// ProjectCredentials is the validated, transient projection of the external
// credential bundle. Secret values are never written to project status.
type ProjectCredentials struct {
	SigningKeys     string
	PublishableKey  string
	SecretKey       string
	AnonRoleJWT     string
	ServiceRoleJWT  string
	PublicJWKS      string
	SigningKeyID    string
	PodTemplateHash string
}

// RequiredProjectCredentialKeys is the complete external Secret contract.
var RequiredProjectCredentialKeys = []string{
	ProjectCredentialsSigningKeysKey,
	ProjectCredentialsPublishableKey,
	ProjectCredentialsSecretKey,
	ProjectCredentialsAnonRoleJWTKey,
	ProjectCredentialsServiceRoleJWTKey,
}

// GoTrueFallbackSecretName returns the create-once implementation Secret name.
func GoTrueFallbackSecretName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + GoTrueFallbackSecretNameSuffix
}

// ValidateProjectCredentials validates the complete externally managed
// credential bundle and returns its public projection. Errors identify only a
// field and safe validation reason; credential contents are never included.
func ValidateProjectCredentials(secret *corev1.Secret) (*ProjectCredentials, error) {
	if secret == nil {
		return nil, fieldError("secret", "object is nil")
	}
	allowed := make(map[string]struct{}, len(RequiredProjectCredentialKeys))
	for _, key := range RequiredProjectCredentialKeys {
		allowed[key] = struct{}{}
	}
	for key := range secret.Data {
		if _, ok := allowed[key]; !ok {
			return nil, fieldError(key, "unexpected field")
		}
	}
	for key := range secret.StringData {
		if _, ok := allowed[key]; !ok {
			return nil, fieldError(key, "unexpected field")
		}
		if _, ok := secret.Data[key]; ok {
			return nil, fieldError(key, "value is provided more than once")
		}
	}
	values := make(map[string]string, len(RequiredProjectCredentialKeys))
	for _, key := range RequiredProjectCredentialKeys {
		value, ok := secretValue(secret, key)
		if !ok || value == "" {
			return nil, fieldError(key, "value is required")
		}
		values[key] = value
	}

	signingKey, err := crypto.ParseSigningJWK(values[ProjectCredentialsSigningKeysKey])
	if err != nil {
		return nil, fieldError(ProjectCredentialsSigningKeysKey, err.Error())
	}
	publicJWKS, err := crypto.PublicJWKS(values[ProjectCredentialsSigningKeysKey])
	if err != nil {
		return nil, fieldError(ProjectCredentialsSigningKeysKey, "public projection failed")
	}

	if err := validateOpaqueKey(values[ProjectCredentialsPublishableKey], "sb_publishable_"); err != nil {
		return nil, fieldError(ProjectCredentialsPublishableKey, err.Error())
	}
	if err := validateOpaqueKey(values[ProjectCredentialsSecretKey], "sb_secret_"); err != nil {
		return nil, fieldError(ProjectCredentialsSecretKey, err.Error())
	}
	if values[ProjectCredentialsPublishableKey] == values[ProjectCredentialsSecretKey] {
		return nil, fieldError(ProjectCredentialsSecretKey, "must be distinct from publishableKey")
	}
	if err := crypto.VerifyES256JWT(values[ProjectCredentialsAnonRoleJWTKey], signingKey, "anon", crypto.RequiredRoleAudience); err != nil {
		return nil, fieldError(ProjectCredentialsAnonRoleJWTKey, err.Error())
	}
	if err := crypto.VerifyES256JWT(values[ProjectCredentialsServiceRoleJWTKey], signingKey, "service_role", crypto.RequiredRoleAudience); err != nil {
		return nil, fieldError(ProjectCredentialsServiceRoleJWTKey, err.Error())
	}

	hash := sha256.New()
	for _, key := range RequiredProjectCredentialKeys {
		// Length-prefix each value so concatenation cannot create collisions.
		_, _ = fmt.Fprintf(hash, "%s:%d:", key, len(values[key]))
		_, _ = hash.Write([]byte(values[key]))
	}
	return &ProjectCredentials{
		SigningKeys:     values[ProjectCredentialsSigningKeysKey],
		PublishableKey:  values[ProjectCredentialsPublishableKey],
		SecretKey:       values[ProjectCredentialsSecretKey],
		AnonRoleJWT:     values[ProjectCredentialsAnonRoleJWTKey],
		ServiceRoleJWT:  values[ProjectCredentialsServiceRoleJWTKey],
		PublicJWKS:      publicJWKS,
		SigningKeyID:    signingKey.Kid,
		PodTemplateHash: fmt.Sprintf("%x", hash.Sum(nil)),
	}, nil
}

func secretValue(secret *corev1.Secret, key string) (string, bool) {
	if value, ok := secret.Data[key]; ok {
		return string(value), true
	}
	if value, ok := secret.StringData[key]; ok {
		return value, true
	}
	return "", false
}

func fieldError(field, reason string) error {
	return fmt.Errorf("project credential field %q: %s", field, reason)
}

func validateOpaqueKey(value, prefix string) error {
	if !strings.HasPrefix(value, prefix) || len(value) == len(prefix) {
		return fmt.Errorf("must start with %s and include a non-empty suffix", prefix)
	}
	for _, r := range value[len(prefix):] {
		isLetter := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
		isDigit := r >= '0' && r <= '9'
		if !isLetter && !isDigit && !strings.ContainsRune("._-", r) {
			return fmt.Errorf("contains an invalid character")
		}
	}
	return nil
}

// GenerateGoTrueFallbackSecret creates the independent random fallback value.
// It is intentionally not included in the project credential bundle.
func GenerateGoTrueFallbackSecret(project *supabasev1alpha1.SupabaseProject) (*corev1.Secret, string, error) {
	name := GoTrueFallbackSecretName(project)
	value, err := crypto.GenerateHex(32)
	if err != nil {
		return nil, "", fmt.Errorf("generating GoTrue fallback secret: %w", err)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, "gotrue-secret"),
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			GoTrueFallbackSecretKey: value,
		},
	}, name, nil
}

// GenerateSecrets generates database implementation role secrets. Public
// project identity is always supplied by ProjectCredentialsSecret instead.
func GenerateSecrets(project *supabasev1alpha1.SupabaseProject) ([]*corev1.Secret, supabasev1alpha1.SecretNamesStatus, error) {
	generated := make([]*corev1.Secret, 0, 4)
	var names supabasev1alpha1.SecretNamesStatus

	fallback, fallbackName, err := GenerateGoTrueFallbackSecret(project)
	if err != nil {
		return nil, names, err
	}
	generated = append(generated, fallback)
	names.GoTrueFallback = fallbackName

	for _, role := range []struct {
		suffix   string
		username string
		set      *string
	}{
		{suffix: "supabase-admin", username: "supabase_admin", set: &names.SupabaseAdmin},
		{suffix: "authenticator", username: "authenticator", set: &names.Authenticator},
		{suffix: "auth-admin", username: "supabase_auth_admin", set: &names.AuthAdmin},
	} {
		secret, name, err := generateRoleSecret(project, role.suffix, role.username)
		if err != nil {
			return nil, names, err
		}
		generated = append(generated, secret)
		*role.set = name
	}
	return generated, names, nil
}

func generateRoleSecret(project *supabasev1alpha1.SupabaseProject, suffix, username string) (*corev1.Secret, string, error) {
	name := project.Name + "-" + suffix + "-password"
	password, err := crypto.GeneratePassword()
	if err != nil {
		return nil, "", fmt.Errorf("generating %s password: %w", username, err)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, suffix),
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"username": username,
			"password": password,
		},
	}, name, nil
}

// GenerateEmailHookSecret creates the per-project Standard Webhooks signing secret.
func GenerateEmailHookSecret(project *supabasev1alpha1.SupabaseProject) (*corev1.Secret, string, error) {
	name := EmailHookSecretName(project)
	value, err := crypto.GenerateWebhookSecret()
	if err != nil {
		return nil, "", fmt.Errorf("generating webhook secret: %w", err)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, "email-hook"),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{EmailHookSecretKey: []byte(value)},
	}, name, nil
}

// EmailHookSecretName returns the fixed per-project email-hook Secret name.
func EmailHookSecretName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-email-hook"
}

// ValidateEmailHookSecret validates the fixed email-hook Secret contract.
func ValidateEmailHookSecret(secret *corev1.Secret) error {
	if secret == nil {
		return fmt.Errorf("email hook Secret is nil")
	}
	value, ok := secretValue(secret, EmailHookSecretKey)
	if !ok || !strings.HasPrefix(value, "v1,whsec_") {
		return fmt.Errorf("email hook Secret must contain a Standard Webhooks value")
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "v1,whsec_"))
	if err != nil || len(payload) != emailHookSecretBytes {
		return fmt.Errorf("email hook Secret contains an invalid Standard Webhooks value")
	}
	return nil
}

// ValidateRoleSecret validates a generated role Secret contract.
func ValidateRoleSecret(secret *corev1.Secret, secretName string) error {
	if secret == nil {
		return fmt.Errorf("role Secret %s is nil", secretName)
	}
	for _, key := range []string{"username", "password"} {
		if _, ok := secretValue(secret, key); !ok {
			return fmt.Errorf("role Secret %s is missing required key %q", secretName, key)
		}
	}
	return nil
}

// GeneratePowersyncSecrets generates PowerSync database role Secrets.
func GeneratePowersyncSecrets(project *supabasev1alpha1.SupabaseProject) ([]*corev1.Secret, error) {
	storage, _, err := generateRoleSecret(project, "powersync-storage", "powersync_storage")
	if err != nil {
		return nil, err
	}
	replication, _, err := generateRoleSecret(project, "powersync-replication", "powersync_replication")
	if err != nil {
		return nil, err
	}
	return []*corev1.Secret{storage, replication}, nil
}

// PowersyncSecretNames returns expected PowerSync implementation Secret names.
func PowersyncSecretNames(project *supabasev1alpha1.SupabaseProject) (string, string) {
	return project.Name + "-powersync-storage-password", project.Name + "-powersync-replication-password"
}
