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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/cnpg"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/defaults"
)

const (
	SequinComponentName = "sequin"
	SequinHTTPPort      = 7376
	SequinMetricsPort   = 4000
)

// SequinDeploymentName returns the Sequin deployment name
func SequinDeploymentName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-sequin"
}

// ResolveImage resolves an ImageSpec to a full image string with defaults
func ResolveImage(spec supabasev1alpha1.ImageSpec, defaultImage, defaultTag string) string {
	repo := defaultImage
	if spec.Repository != "" {
		repo = spec.Repository
	}
	tag := defaultTag
	if spec.Tag != "" {
		tag = spec.Tag
	}
	image := fmt.Sprintf("%s:%s", repo, tag)
	if spec.Registry != "" {
		image = fmt.Sprintf("%s/%s", spec.Registry, image)
	}
	return image
}

// ResolvePullPolicy resolves pull policy from ImageSpec with IfNotPresent default
func ResolvePullPolicy(spec supabasev1alpha1.ImageSpec) corev1.PullPolicy {
	if spec.PullPolicy != "" {
		return spec.PullPolicy
	}
	return corev1.PullIfNotPresent
}

// DefaultSequinResources returns default resource requirements for Sequin
func DefaultSequinResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("256Mi"),
			corev1.ResourceCPU:    resource.MustParse("100m"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("512Mi"),
			corev1.ResourceCPU:    resource.MustParse("500m"),
		},
	}
}

// NormalizeSequinResources returns provided resources if set, otherwise returns defaults
func NormalizeSequinResources(resources corev1.ResourceRequirements) corev1.ResourceRequirements {
	if len(resources.Requests) == 0 && len(resources.Limits) == 0 {
		return DefaultSequinResources()
	}
	return resources
}

// BuildSequinDeployment creates the Sequin deployment
func BuildSequinDeployment(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) *appsv1.Deployment {
	spec := project.Spec.Sequin
	name := SequinDeploymentName(project)
	dbHost := cnpg.ClusterRWServiceName(project)

	image := ResolveImage(spec.Image, defaults.SequinImage, defaults.SequinTag)
	pullPolicy := ResolvePullPolicy(spec.Image)
	replicas := NormalizeReplicas(spec.Replicas)
	resources := NormalizeSequinResources(spec.Resources)

	env := buildSequinEnv(project, secretNames, dbHost)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   project.Namespace,
			Labels:      common.ComponentLabels(project, SequinComponentName),
			Annotations: common.ReloaderAnnotations(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: common.SelectorLabels(project, SequinComponentName),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      common.ComponentLabels(project, SequinComponentName),
					Annotations: common.ReloaderAnnotations(),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            SequinComponentName,
							Image:           image,
							ImagePullPolicy: pullPolicy,
							Env:             env,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: SequinHTTPPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "metrics",
									ContainerPort: SequinMetricsPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							LivenessProbe: BuildHTTPProbe(ProbeConfig{
								Path:                "/health",
								Port:                SequinHTTPPort,
								InitialDelaySeconds: 30,
								PeriodSeconds:       10,
								TimeoutSeconds:      5,
							}),
							ReadinessProbe: BuildHTTPProbe(ProbeConfig{
								Path:                "/health",
								Port:                SequinHTTPPort,
								InitialDelaySeconds: 10,
								PeriodSeconds:       5,
								TimeoutSeconds:      3,
							}),
							Resources: resources,
						},
					},
				},
			},
		},
	}

	AddImagePullSecrets(&deployment.Spec.Template.Spec, project)

	return deployment
}

// buildSequinEnv builds environment variables for the Sequin deployment
func buildSequinEnv(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus, dbHost string) []corev1.EnvVar {
	spec := project.Spec.Sequin

	// Build Redis URL
	redisURL := "redis://localhost:6379"
	if spec.Redis.External != nil {
		port := spec.Redis.External.Port
		if port == 0 {
			port = 6379
		}
		redisURL = fmt.Sprintf("redis://%s:%d", spec.Redis.External.Host, port)
	}

	env := []corev1.EnvVar{
		// Sequin database connection (sequin's own database)
		{Name: "PG_HOSTNAME", Value: dbHost},
		{Name: "PG_PORT", Value: "5432"},
		{Name: "PG_DATABASE", Value: "sequin"},
		{
			Name: "PG_USERNAME",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.SequinPassword,
					},
					Key: "username",
				},
			},
		},
		{
			Name: "PG_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.SequinPassword,
					},
					Key: "password",
				},
			},
		},

		// Redis
		{Name: "REDIS_URL", Value: redisURL},

		// Sequin configuration
		{Name: "SEQUIN_ENV", Value: "prod"},
		{Name: "PHX_HOST", Value: SequinDeploymentName(project)},
		{Name: "PORT", Value: fmt.Sprintf("%d", SequinHTTPPort)},

		// Secret key base
		{
			Name: "SECRET_KEY_BASE",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.Sequin,
					},
					Key: "secretKeyBase",
				},
			},
		},

		// Vault key
		{
			Name: "VAULT_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.Sequin,
					},
					Key: "vaultKey",
				},
			},
		},
	}

	return env
}
