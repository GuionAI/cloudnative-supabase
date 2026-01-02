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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
	"github.com/GuionAI/cloudnative-supabase/pkg/crypto"
)

// GenerateSecrets generates all required secrets for a SupabaseProject
func GenerateSecrets(project *supabasev1alpha1.SupabaseProject) ([]*corev1.Secret, supabasev1alpha1.SecretNamesStatus, error) {
	var secrets []*corev1.Secret
	var secretNames supabasev1alpha1.SecretNamesStatus

	// Generate JWT secret
	jwtSecret, jwtSecretName, err := generateJWTSecret(project)
	if err != nil {
		return nil, secretNames, err
	}
	if jwtSecret != nil {
		secrets = append(secrets, jwtSecret)
	}
	secretNames.JWT = jwtSecretName

	// Generate database role secrets
	supabaseAdminSecret, supabaseAdminName, err := generateRoleSecret(project, "supabase-admin", "supabase_admin")
	if err != nil {
		return nil, secretNames, err
	}
	secrets = append(secrets, supabaseAdminSecret)
	secretNames.SupabaseAdmin = supabaseAdminName

	authenticatorSecret, authenticatorName, err := generateRoleSecret(project, "authenticator", "authenticator")
	if err != nil {
		return nil, secretNames, err
	}
	secrets = append(secrets, authenticatorSecret)
	secretNames.Authenticator = authenticatorName

	authAdminSecret, authAdminName, err := generateRoleSecret(project, "auth-admin", "supabase_auth_admin")
	if err != nil {
		return nil, secretNames, err
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
		return nil, "", err
	}

	// Determine expiration
	expSeconds := 3600
	if project.Spec.JWT != nil && project.Spec.JWT.ExpirationSeconds > 0 {
		expSeconds = project.Spec.JWT.ExpirationSeconds
	}

	// Generate tokens
	anonKey, err := crypto.CreateAnonKey(jwtSecret, expSeconds)
	if err != nil {
		return nil, "", err
	}

	serviceKey, err := crypto.CreateServiceKey(jwtSecret, expSeconds)
	if err != nil {
		return nil, "", err
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
		return nil, "", err
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
