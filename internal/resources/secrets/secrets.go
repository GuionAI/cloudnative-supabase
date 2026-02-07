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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
	"github.com/GuionAI/cloudnative-supabase/pkg/crypto"
)

// RequiredJWTKeys are the required keys in a JWT secret
var RequiredJWTKeys = []string{"secret", "anonKey", "serviceKey"}

// RequiredRoleKeys are the required keys in a database role secret
var RequiredRoleKeys = []string{"username", "password"}

// GenerateSecrets generates all required secrets for a SupabaseProject
func GenerateSecrets(project *supabasev1alpha1.SupabaseProject) ([]*corev1.Secret, supabasev1alpha1.SecretNamesStatus, error) {
	var secrets []*corev1.Secret
	var secretNames supabasev1alpha1.SecretNamesStatus

	// Generate JWT secret
	jwtSecret, jwtSecretName, err := generateJWTSecret(project)
	if err != nil {
		return nil, secretNames, fmt.Errorf("failed to generate JWT secret: %w", err)
	}
	if jwtSecret != nil {
		secrets = append(secrets, jwtSecret)
	}
	secretNames.JWT = jwtSecretName

	// Generate database role secrets
	supabaseAdminSecret, supabaseAdminName, err := generateRoleSecret(project, "supabase-admin", "supabase_admin")
	if err != nil {
		return nil, secretNames, fmt.Errorf("failed to generate supabase-admin secret: %w", err)
	}
	secrets = append(secrets, supabaseAdminSecret)
	secretNames.SupabaseAdmin = supabaseAdminName

	authenticatorSecret, authenticatorName, err := generateRoleSecret(project, "authenticator", "authenticator")
	if err != nil {
		return nil, secretNames, fmt.Errorf("failed to generate authenticator secret: %w", err)
	}
	secrets = append(secrets, authenticatorSecret)
	secretNames.Authenticator = authenticatorName

	authAdminSecret, authAdminName, err := generateRoleSecret(project, "auth-admin", "supabase_auth_admin")
	if err != nil {
		return nil, secretNames, fmt.Errorf("failed to generate auth-admin secret: %w", err)
	}
	secrets = append(secrets, authAdminSecret)
	secretNames.AuthAdmin = authAdminName

	return secrets, secretNames, nil
}

// generateJWTSecret creates the JWT secret with secret key and pre-generated tokens
func generateJWTSecret(project *supabasev1alpha1.SupabaseProject) (*corev1.Secret, string, error) {
	secretName := project.Name + "-jwt"

	// Use provided secret if specified
	if project.Spec.JWT != nil && project.Spec.JWT.SecretRef != "" {
		return nil, project.Spec.JWT.SecretRef, nil
	}

	// Generate new JWT secret
	jwtSecret, err := crypto.GenerateHex(32)
	if err != nil {
		return nil, "", fmt.Errorf("generating secret key: %w", err)
	}

	// Generate API keys (anon/service_role) with 5-year expiration.
	// Note: JWT.ExpirationSeconds controls access token expiration in GoTrue,
	// NOT the API keys. API keys are embedded in client apps and must be long-lived.
	anonKey, err := crypto.CreateAnonKey(jwtSecret, crypto.DefaultTokenExpiration)
	if err != nil {
		return nil, "", fmt.Errorf("generating anon key: %w", err)
	}

	serviceKey, err := crypto.CreateServiceKey(jwtSecret, crypto.DefaultTokenExpiration)
	if err != nil {
		return nil, "", fmt.Errorf("generating service key: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, "jwt"),
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"secret":     jwtSecret,
			"anonKey":    anonKey,
			"serviceKey": serviceKey,
		},
	}

	return secret, secretName, nil
}

// generateRoleSecret creates a secret for a database role with username/password
func generateRoleSecret(project *supabasev1alpha1.SupabaseProject, nameSuffix, username string) (*corev1.Secret, string, error) {
	secretName := project.Name + "-" + nameSuffix + "-password"

	password, err := crypto.GeneratePassword()
	if err != nil {
		return nil, "", fmt.Errorf("generating password: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, nameSuffix),
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"username": username,
			"password": password,
		},
	}

	return secret, secretName, nil
}

// ValidateJWTSecret validates that a secret contains all required JWT keys
func ValidateJWTSecret(secret *corev1.Secret) error {
	for _, key := range RequiredJWTKeys {
		if _, ok := secret.Data[key]; !ok {
			return fmt.Errorf("JWT secret %s/%s missing required key: %s", secret.Namespace, secret.Name, key)
		}
	}
	return nil
}

// ValidateRoleSecret validates that a secret contains all required role keys
func ValidateRoleSecret(secret *corev1.Secret, secretName string) error {
	for _, key := range RequiredRoleKeys {
		if _, ok := secret.Data[key]; !ok {
			return fmt.Errorf("role secret %s/%s missing required key: %s", secret.Namespace, secretName, key)
		}
	}
	return nil
}

// GetSecretNamesFromSpec extracts secret names from user-specified secrets configuration
func GetSecretNamesFromSpec(spec *supabasev1alpha1.SecretsSpec) supabasev1alpha1.SecretNamesStatus {
	return supabasev1alpha1.SecretNamesStatus{
		JWT:           spec.JWT,
		SupabaseAdmin: spec.SupabaseAdmin,
		Authenticator: spec.Authenticator,
		AuthAdmin:     spec.AuthAdmin,
	}
}

// GenerateSequinSecrets generates all Sequin-related secrets
func GenerateSequinSecrets(project *supabasev1alpha1.SupabaseProject) ([]*corev1.Secret, error) {
	var secrets []*corev1.Secret

	// Sequin application secret (secretKeyBase, vaultKey, apiToken)
	appSecret, err := generateSequinAppSecret(project)
	if err != nil {
		return nil, fmt.Errorf("failed to generate Sequin app secret: %w", err)
	}
	secrets = append(secrets, appSecret)

	// Sequin database role password
	sequinPassword, _, err := generateRoleSecret(project, "sequin", "sequin")
	if err != nil {
		return nil, fmt.Errorf("failed to generate sequin password: %w", err)
	}
	secrets = append(secrets, sequinPassword)

	// Sequin replication role password
	replicationPassword, _, err := generateRoleSecret(project, "sequin-replication", "sequin_replication")
	if err != nil {
		return nil, fmt.Errorf("failed to generate sequin-replication password: %w", err)
	}
	secrets = append(secrets, replicationPassword)

	return secrets, nil
}

// SequinSecretNames returns the expected secret names for Sequin
func SequinSecretNames(project *supabasev1alpha1.SupabaseProject) (sequin, sequinPassword, sequinReplicationPassword string) {
	return project.Name + "-sequin",
		project.Name + "-sequin-password",
		project.Name + "-sequin-replication-password"
}

// GeneratePowersyncSecrets generates Powersync-related secrets
func GeneratePowersyncSecrets(project *supabasev1alpha1.SupabaseProject) ([]*corev1.Secret, error) {
	var secrets []*corev1.Secret

	// Powersync storage role password
	storagePassword, _, err := generateRoleSecret(project, "powersync-storage", "powersync_storage")
	if err != nil {
		return nil, fmt.Errorf("failed to generate powersync-storage password: %w", err)
	}
	secrets = append(secrets, storagePassword)

	return secrets, nil
}

// PowersyncSecretNames returns the expected secret names for Powersync
func PowersyncSecretNames(project *supabasev1alpha1.SupabaseProject) (powersyncStoragePassword string) {
	return project.Name + "-powersync-storage-password"
}

// GenerateMeilisearchSecrets generates Meilisearch-related secrets
func GenerateMeilisearchSecrets(project *supabasev1alpha1.SupabaseProject) ([]*corev1.Secret, error) {
	// Use existing secret if specified
	if project.Spec.Meilisearch.MasterKeySecretRef != "" {
		return nil, nil
	}

	secretName := MeilisearchSecretName(project)

	masterKey, err := crypto.GenerateHex(32)
	if err != nil {
		return nil, fmt.Errorf("generating meilisearch master key: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, "meilisearch"),
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"masterKey": masterKey,
		},
	}

	return []*corev1.Secret{secret}, nil
}

// MeilisearchSecretName returns the expected secret name for Meilisearch master key
func MeilisearchSecretName(project *supabasev1alpha1.SupabaseProject) string {
	if project.Spec.Meilisearch != nil && project.Spec.Meilisearch.MasterKeySecretRef != "" {
		return project.Spec.Meilisearch.MasterKeySecretRef
	}
	return project.Name + "-meilisearch-master-key"
}

// generateSequinAppSecret creates the Sequin application secret
func generateSequinAppSecret(project *supabasev1alpha1.SupabaseProject) (*corev1.Secret, error) {
	secretName := project.Name + "-sequin"

	// SECRET_KEY_BASE: 64 bytes hex (128 chars)
	secretKeyBase, err := crypto.GenerateHex(64)
	if err != nil {
		return nil, fmt.Errorf("generating secretKeyBase: %w", err)
	}

	// VAULT_KEY: 32 bytes base64
	vaultKey, err := crypto.GenerateBase64(32)
	if err != nil {
		return nil, fmt.Errorf("generating vaultKey: %w", err)
	}

	// API token: 32 bytes hex
	apiToken, err := crypto.GenerateHex(32)
	if err != nil {
		return nil, fmt.Errorf("generating apiToken: %w", err)
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, "sequin"),
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"secretKeyBase": secretKeyBase,
			"vaultKey":      vaultKey,
			"apiToken":      apiToken,
		},
	}, nil
}
