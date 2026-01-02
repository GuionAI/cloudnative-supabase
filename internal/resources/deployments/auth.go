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
	"k8s.io/apimachinery/pkg/util/intstr"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/cnpg"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
)

const (
	// AuthComponentName is the name of the auth component
	AuthComponentName = "auth"

	// AuthPort is the port GoTrue listens on
	AuthPort = 9999

	// DefaultAuthImage is the default GoTrue image
	DefaultAuthImage = "supabase/gotrue"

	// DefaultAuthTag is the default GoTrue image tag
	DefaultAuthTag = "v2.184.0"
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
	imageTag := DefaultAuthTag
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

	replicas := spec.Replicas
	if replicas == 0 {
		replicas = 1
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, AuthComponentName),
			Annotations: map[string]string{
				"reloader.stakater.com/auto": "true",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: common.SelectorLabels(project, AuthComponentName),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: common.ComponentLabels(project, AuthComponentName),
					Annotations: map[string]string{
						"reloader.stakater.com/auto": "true",
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						buildDBInitContainer(project, secretNames, dbHost),
					},
					Containers: []corev1.Container{
						{
							Name:            AuthComponentName,
							Image:           fmt.Sprintf("%s:%s", DefaultAuthImage, imageTag),
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
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(AuthPort),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       10,
								TimeoutSeconds:      5,
								FailureThreshold:    3,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(AuthPort),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
								TimeoutSeconds:      3,
								FailureThreshold:    3,
							},
							Resources: spec.Resources,
						},
					},
				},
			},
		},
	}

	// Add image pull secrets if specified
	if len(project.Spec.ImagePullSecrets) > 0 {
		deployment.Spec.Template.Spec.ImagePullSecrets = project.Spec.ImagePullSecrets
	}

	return deployment
}

// buildAuthEnv builds the environment variables for GoTrue
func buildAuthEnv(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus, dbHost string) []corev1.EnvVar {
	spec := project.Spec.Auth

	env := []corev1.EnvVar{
		// Database configuration
		{Name: "DB_HOST", Value: dbHost},
		{Name: "DB_PORT", Value: "5432"},
		{Name: "DB_DRIVER", Value: "postgres"},
		{Name: "DB_SSL", Value: "disable"},
		{Name: "DB_NAME", Value: cnpg.DatabaseName},

		// API configuration
		{Name: "GOTRUE_API_HOST", Value: "0.0.0.0"},
		{Name: "GOTRUE_API_PORT", Value: fmt.Sprintf("%d", AuthPort)},
		{Name: "API_EXTERNAL_URL", Value: spec.ExternalURL},
		{Name: "GOTRUE_SITE_URL", Value: spec.SiteURL},
		{Name: "GOTRUE_URI_ALLOW_LIST", Value: "*"},

		// JWT configuration
		{Name: "GOTRUE_JWT_DEFAULT_GROUP_NAME", Value: "authenticated"},
		{Name: "GOTRUE_JWT_ADMIN_ROLES", Value: "service_role"},
		{Name: "GOTRUE_JWT_AUD", Value: "authenticated"},
		{Name: "GOTRUE_JWT_EXP", Value: fmt.Sprintf("%d", getJWTExp(project))},

		// Signup configuration
		{Name: "GOTRUE_DISABLE_SIGNUP", Value: fmt.Sprintf("%t", spec.DisableSignup)},

		// Email configuration
		{Name: "GOTRUE_EXTERNAL_EMAIL_ENABLED", Value: "true"},
		{Name: "GOTRUE_MAILER_AUTOCONFIRM", Value: fmt.Sprintf("%t", spec.AutoConfirmEmail)},
		{Name: "GOTRUE_MAILER_OTP_EXP", Value: "3600"},

		// Database credentials from secret
		{
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
		{
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
		{
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

		// JWT secret
		{
			Name: "GOTRUE_JWT_SECRET",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.JWT,
					},
					Key: "secret",
				},
			},
		},

		// Database URL (constructed from other env vars)
		{
			Name:  "GOTRUE_DB_DATABASE_URL",
			Value: "$(DB_DRIVER)://$(DB_USER):$(DB_PASSWORD_ENC)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?search_path=auth&sslmode=$(DB_SSL)",
		},
		{Name: "GOTRUE_DB_DRIVER", Value: "$(DB_DRIVER)"},
	}

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
		env = append(env,
			corev1.EnvVar{Name: "GOTRUE_HOOK_SEND_EMAIL_ENABLED", Value: "true"},
			corev1.EnvVar{Name: "GOTRUE_HOOK_SEND_EMAIL_URI", Value: spec.EmailHook.URI},
		)
	}

	return env
}

// buildDBInitContainer creates the init container that waits for the database
func buildDBInitContainer(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus, dbHost string) corev1.Container {
	return corev1.Container{
		Name:            "init-db",
		Image:           "postgres:15-alpine",
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env: []corev1.EnvVar{
			{Name: "DB_HOST", Value: dbHost},
			{Name: "DB_PORT", Value: "5432"},
			{
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
		},
		Command: []string{"/bin/sh", "-c"},
		Args: []string{
			`until pg_isready -h $(DB_HOST) -p $(DB_PORT) -U $(DB_USER); do
echo "Waiting for database to start..."
sleep 2
done
echo "Database is ready"`,
		},
	}
}

// getJWTExp returns JWT expiration seconds
func getJWTExp(project *supabasev1alpha1.SupabaseProject) int {
	if project.Spec.JWT != nil && project.Spec.JWT.ExpirationSeconds > 0 {
		return project.Spec.JWT.ExpirationSeconds
	}
	return 3600
}
