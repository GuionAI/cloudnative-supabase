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
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/cnpg"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/defaults"
)

const (
	// RestComponentName is the name of the rest component
	RestComponentName = "rest"

	// RestPort is the port PostgREST listens on
	RestPort = 3000
)

// RestDeploymentName returns the rest deployment name
func RestDeploymentName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-rest"
}

// BuildRestDeployment creates the PostgREST deployment
func BuildRestDeployment(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) *appsv1.Deployment {
	spec := &project.Spec.Rest
	name := RestDeploymentName(project)
	dbHost := cnpg.ClusterRWServiceName(project)

	// Determine image tag
	imageTag := defaults.RestTag
	if spec.ImageTag != "" {
		imageTag = spec.ImageTag
	}

	// Determine schemas
	schemas := []string{"public"}
	if len(spec.Schemas) > 0 {
		schemas = spec.Schemas
	}

	replicas := NormalizeReplicas(spec.Replicas)

	env := []corev1.EnvVar{
		// Database configuration
		{Name: "DB_HOST", Value: dbHost},
		{Name: "DB_PORT", Value: "5432"},
		{Name: "DB_DRIVER", Value: "postgres"},
		{Name: "DB_SSL", Value: "disable"},
		{Name: "DB_NAME", Value: common.DatabaseName},

		// PostgREST configuration
		{Name: "PGRST_DB_SCHEMAS", Value: strings.Join(schemas, ",")},
		{Name: "PGRST_DB_ANON_ROLE", Value: "anon"},
		{Name: "PGRST_DB_USE_LEGACY_GUCS", Value: "false"},
		{Name: "PGRST_APP_SETTINGS_JWT_EXP", Value: fmt.Sprintf("%d", common.GetAccessTokenExpiration(project))},

		// Database credentials
		{
			Name: "DB_USER",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.Authenticator,
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
						Name: secretNames.Authenticator,
					},
					Key: "password",
				},
			},
		},

		// JWT secret
		{
			Name: "PGRST_JWT_SECRET",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.JWT,
					},
					Key: "secret",
				},
			},
		},

		// Database URI (constructed from env vars)
		{
			Name:  "PGRST_DB_URI",
			Value: "$(DB_DRIVER)://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=$(DB_SSL)",
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   project.Namespace,
			Labels:      common.ComponentLabels(project, RestComponentName),
			Annotations: common.ReloaderAnnotations(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: common.SelectorLabels(project, RestComponentName),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      common.ComponentLabels(project, RestComponentName),
					Annotations: common.ReloaderAnnotations(),
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						buildRestDBInitContainer(secretNames, dbHost),
					},
					Containers: []corev1.Container{
						{
							Name:            RestComponentName,
							Image:           fmt.Sprintf("%s:%s", defaults.RestImage, imageTag),
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env:             env,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: RestPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							LivenessProbe:  BuildLivenessProbe("/", RestPort),
							ReadinessProbe: BuildReadinessProbe("/", RestPort),
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

// buildRestDBInitContainer creates the init container that waits for the database
func buildRestDBInitContainer(secretNames *supabasev1alpha1.SecretNamesStatus, dbHost string) corev1.Container {
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
							Name: secretNames.Authenticator,
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
