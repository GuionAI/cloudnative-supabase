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

package deployments

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/cnpg"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/defaults"
	secretresources "github.com/GuionAI/cloudnative-supabase/internal/resources/secrets"
)

const (
	// AuthComponentName is the name of the auth component
	AuthComponentName = "auth"

	// AuthPort is the port GoTrue listens on
	AuthPort = 9999
)

// AuthDeploymentName returns the auth deployment name
func AuthDeploymentName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-auth"
}

// AuthServiceName returns the auth service name
func AuthServiceName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-auth"
}

// BuildAuthDeployment creates the GoTrue auth deployment
func BuildAuthDeployment(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) *appsv1.Deployment {
	spec := project.Spec.Auth
	name := AuthDeploymentName(project)
	dbHost := cnpg.ClusterRWServiceName(project)

	// Determine image tag
	imageTag := defaults.AuthTag
	if spec.ImageTag != "" {
		imageTag = spec.ImageTag
	}

	// Build environment variables
	env := buildAuthEnv(project, secretNames, dbHost)

	// Build envFrom for auth providers
	var envFrom []corev1.EnvFromSource
	if spec.Providers != nil && spec.Providers.SecretRef != "" {
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: spec.Providers.SecretRef,
				},
			},
		})
	}

	replicas := NormalizeReplicas(spec.Replicas)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   project.Namespace,
			Labels:      common.ComponentLabels(project, AuthComponentName),
			Annotations: common.ReloaderAnnotations(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: common.SelectorLabels(project, AuthComponentName),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      common.ComponentLabels(project, AuthComponentName),
					Annotations: common.ReloaderAnnotations(),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            AuthComponentName,
							Image:           fmt.Sprintf("%s:%s", defaults.AuthImage, imageTag),
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env:             env,
							EnvFrom:         envFrom,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: AuthPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							LivenessProbe:  BuildLivenessProbe("/health", AuthPort),
							ReadinessProbe: BuildReadinessProbe("/health", AuthPort),
							Resources:      spec.Resources,
						},
					},
				},
			},
		},
	}

	AddImagePullSecrets(&deployment.Spec.Template.Spec, project)

	return deployment
}

// buildAuthEnv builds the environment variables for GoTrue
func buildAuthEnv(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus, dbHost string) []corev1.EnvVar {
	spec := project.Spec.Auth
	fallbackSecretName := secretNames.GoTrueFallback
	if fallbackSecretName == "" {
		fallbackSecretName = secretresources.GoTrueFallbackSecretName(project)
	}

	env := BuildDatabaseEnv(dbHost)
	env = append(env,
		// API configuration
		corev1.EnvVar{Name: "GOTRUE_API_HOST", Value: "0.0.0.0"},
		corev1.EnvVar{Name: "GOTRUE_API_PORT", Value: fmt.Sprintf("%d", AuthPort)},
		corev1.EnvVar{Name: "API_EXTERNAL_URL", Value: spec.ExternalURL},
		corev1.EnvVar{Name: "GOTRUE_SITE_URL", Value: spec.SiteURL},
		corev1.EnvVar{Name: "GOTRUE_URI_ALLOW_LIST", Value: "*"},

		// JWT configuration
		corev1.EnvVar{Name: "GOTRUE_JWT_DEFAULT_GROUP_NAME", Value: "authenticated"},
		corev1.EnvVar{Name: "GOTRUE_JWT_ADMIN_ROLES", Value: "service_role"},
		corev1.EnvVar{Name: "GOTRUE_JWT_AUD", Value: "authenticated"},
		corev1.EnvVar{Name: "GOTRUE_JWT_EXP", Value: fmt.Sprintf("%d", common.GetAccessTokenExpiration(project))},
		corev1.EnvVar{Name: "GOTRUE_JWT_ISSUER", Value: common.NormalizeExternalURL(spec.ExternalURL)},
		corev1.EnvVar{Name: "GOTRUE_JWT_VALID_METHODS", Value: "ES256"},
		corev1.EnvVar{
			Name: "GOTRUE_JWT_KEYS",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: project.Spec.ProjectCredentialsSecret},
				Key:                  secretresources.ProjectCredentialsSigningKeysKey,
			}},
		},

		// Signup configuration
		corev1.EnvVar{Name: "GOTRUE_DISABLE_SIGNUP", Value: fmt.Sprintf("%t", spec.DisableSignup)},
		corev1.EnvVar{Name: "GOTRUE_EXTERNAL_ANONYMOUS_USERS_ENABLED", Value: fmt.Sprintf("%t", spec.EnableAnonymousUsers)},

		// Email configuration
		corev1.EnvVar{Name: "GOTRUE_EXTERNAL_EMAIL_ENABLED", Value: "true"},
		corev1.EnvVar{Name: "GOTRUE_MAILER_AUTOCONFIRM", Value: fmt.Sprintf("%t", spec.AutoConfirmEmail)},
		corev1.EnvVar{Name: "GOTRUE_MAILER_OTP_EXP", Value: "3600"},

		// Database credentials from secret
		corev1.EnvVar{
			Name: "DB_USER",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.AuthAdmin,
					},
					Key: "username",
				},
			},
		},
		corev1.EnvVar{
			Name: "DB_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.AuthAdmin,
					},
					Key: "password",
				},
			},
		},
		corev1.EnvVar{
			Name: "DB_PASSWORD_ENC",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.AuthAdmin,
					},
					Key: "password",
				},
			},
		},

		// GoTrue still requires an independent fallback secret. It is never
		// shared with verifiers or any other workload.
		corev1.EnvVar{
			Name: "GOTRUE_JWT_SECRET",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: fallbackSecretName,
					},
					Key: secretresources.GoTrueFallbackSecretKey,
				},
			},
		},

		// Database URL (constructed from other env vars)
		corev1.EnvVar{
			Name:  "GOTRUE_DB_DATABASE_URL",
			Value: "$(DB_DRIVER)://$(DB_USER):$(DB_PASSWORD_ENC)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?search_path=auth&sslmode=$(DB_SSL)",
		},
		corev1.EnvVar{Name: "GOTRUE_DB_DRIVER", Value: "$(DB_DRIVER)"},
	)

	// Add OAuth provider configuration
	if spec.Providers != nil {
		if spec.Providers.Google != nil && spec.Providers.Google.Enabled {
			env = append(env, corev1.EnvVar{Name: "GOTRUE_EXTERNAL_GOOGLE_ENABLED", Value: "true"})
			if spec.Providers.Google.SkipNonceCheck {
				env = append(env, corev1.EnvVar{Name: "GOTRUE_EXTERNAL_GOOGLE_SKIP_NONCE_CHECK", Value: "true"})
			}
		}
		if spec.Providers.Apple != nil && spec.Providers.Apple.Enabled {
			env = append(env, corev1.EnvVar{Name: "GOTRUE_EXTERNAL_APPLE_ENABLED", Value: "true"})
		}
	}

	// Add email hook configuration
	if spec.EmailHook != nil && spec.EmailHook.Enabled {
		emailHookSecretName := secretNames.EmailHook
		if emailHookSecretName == "" {
			emailHookSecretName = secretresources.EmailHookSecretName(project)
		}
		env = append(env,
			corev1.EnvVar{Name: "GOTRUE_HOOK_SEND_EMAIL_ENABLED", Value: "true"},
			corev1.EnvVar{Name: "GOTRUE_HOOK_SEND_EMAIL_URI", Value: spec.EmailHook.URI},
			corev1.EnvVar{
				Name: "GOTRUE_HOOK_SEND_EMAIL_SECRETS",
				ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: emailHookSecretName},
					Key:                  "secret",
				}},
			},
		)
	}

	return applyGoTrueEnv(env, spec.GoTrueEnv)
}

func applyGoTrueEnv(env []corev1.EnvVar, configured []supabasev1alpha1.GoTrueEnvVar) []corev1.EnvVar {
	indexes := make(map[string]int, len(env))
	for index, variable := range env {
		indexes[variable.Name] = index
	}
	for _, configuredVariable := range configured {
		if IsOperatorOwnedGoTrueEnv(configuredVariable.Name) {
			// Security-critical JWT settings are owned by the operator even when
			// a builder is used directly outside the controller admission path.
			continue
		}
		variable := corev1.EnvVar{Name: configuredVariable.Name}
		if configuredVariable.Value != nil {
			variable.Value = *configuredVariable.Value
		} else if valueFrom := configuredVariable.ValueFrom; valueFrom != nil {
			variable.ValueFrom = &corev1.EnvVarSource{
				SecretKeyRef:    goTrueEnvSecretKeyRef(valueFrom.SecretKeyRef),
				ConfigMapKeyRef: goTrueEnvConfigMapKeyRef(valueFrom.ConfigMapKeyRef),
			}
		}
		if index, exists := indexes[variable.Name]; exists {
			env[index] = variable
			continue
		}
		indexes[variable.Name] = len(env)
		env = append(env, variable)
	}
	return env
}

// operatorOwnedGoTrueEnvNames is the set of settings that define the ES256
// authentication contract. Provider-specific GOTRUE_ settings remain
// user-configurable.
var operatorOwnedGoTrueEnvNames = map[string]struct{}{
	"GOTRUE_JWT_KEYS":               {},
	"GOTRUE_JWT_SECRET":             {},
	"GOTRUE_JWT_ALG":                {},
	"GOTRUE_JWT_KEY_ID":             {},
	"GOTRUE_JWT_ISSUER":             {},
	"GOTRUE_JWT_AUD":                {},
	"GOTRUE_JWT_EXP":                {},
	"GOTRUE_JWT_VALID_METHODS":      {},
	"GOTRUE_JWT_ALLOWED_ALGS":       {},
	"GOTRUE_JWT_ADMIN_ROLES":        {},
	"GOTRUE_JWT_ADMIN_GROUP_NAME":   {},
	"GOTRUE_JWT_DEFAULT_GROUP_NAME": {},
	"GOTRUE_JWT_ROLE_CLAIM":         {},
}

// IsOperatorOwnedGoTrueEnv reports whether a custom environment variable
// would weaken or replace an operator-owned authentication setting.
func IsOperatorOwnedGoTrueEnv(name string) bool {
	_, ok := operatorOwnedGoTrueEnvNames[name]
	return ok
}

func goTrueEnvSecretKeyRef(ref *supabasev1alpha1.GoTrueEnvKeySelector) *corev1.SecretKeySelector {
	if ref == nil {
		return nil
	}
	return &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: ref.Name},
		Key:                  ref.Key,
		Optional:             ref.Optional,
	}
}

func goTrueEnvConfigMapKeyRef(ref *supabasev1alpha1.GoTrueEnvKeySelector) *corev1.ConfigMapKeySelector {
	if ref == nil {
		return nil
	}
	return &corev1.ConfigMapKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: ref.Name},
		Key:                  ref.Key,
		Optional:             ref.Optional,
	}
}
