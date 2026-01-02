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
	// StudioComponentName is the name of the studio component
	StudioComponentName = "studio"

	// StudioPort is the port Studio listens on
	StudioPort = 3000

	// DefaultStudioImage is the default Studio image
	DefaultStudioImage = "supabase/studio"

	// DefaultStudioTag is the default Studio image tag
	DefaultStudioTag = "2025.12.17-sha-43f4f7f"
)

// StudioDeploymentName returns the studio deployment name
func StudioDeploymentName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-studio"
}

// BuildStudioDeployment creates the Supabase Studio deployment
func BuildStudioDeployment(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) *appsv1.Deployment {
	spec := &project.Spec.Studio
	name := StudioDeploymentName(project)
	dbHost := cnpg.ClusterRWServiceName(project)

	// Determine image tag
	imageTag := DefaultStudioTag
	if spec.ImageTag != "" {
		imageTag = spec.ImageTag
	}

	// Determine org and project names
	orgName := "Default Organization"
	if spec.OrganizationName != "" {
		orgName = spec.OrganizationName
	}
	projName := "Default Project"
	if spec.ProjectName != "" {
		projName = spec.ProjectName
	}

	replicas := spec.Replicas
	if replicas == 0 {
		replicas = 1
	}

	// Build internal service URLs
	kongService := project.Name + "-kong"
	metaService := project.Name + "-meta"

	env := []corev1.EnvVar{
		// Server configuration
		{Name: "HOSTNAME", Value: "::"},
		{Name: "STUDIO_PORT", Value: fmt.Sprintf("%d", StudioPort)},

		// Database configuration
		{Name: "POSTGRES_HOST", Value: dbHost},
		{Name: "POSTGRES_PORT", Value: "5432"},
		{Name: "POSTGRES_DB", Value: common.DatabaseName},

		// Organization configuration
		{Name: "STUDIO_DEFAULT_ORGANIZATION", Value: orgName},
		{Name: "STUDIO_DEFAULT_PROJECT", Value: projName},

		// Internal service URLs
		{Name: "SUPABASE_URL", Value: fmt.Sprintf("http://%s:8000", kongService)},
		{Name: "STUDIO_PG_META_URL", Value: fmt.Sprintf("http://%s:8080", metaService)},

		// Public URL for auth redirects
		{Name: "SUPABASE_PUBLIC_URL", Value: spec.PublicURL},

		// Database credentials
		{
			Name: "POSTGRES_USER",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.SupabaseAdmin,
					},
					Key: "username",
				},
			},
		},
		{
			Name: "POSTGRES_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.SupabaseAdmin,
					},
					Key: "password",
				},
			},
		},

		// JWT keys
		{
			Name: "SUPABASE_ANON_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.JWT,
					},
					Key: "anonKey",
				},
			},
		},
		{
			Name: "SUPABASE_SERVICE_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.JWT,
					},
					Key: "serviceKey",
				},
			},
		},
		{
			Name: "AUTH_JWT_SECRET",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.JWT,
					},
					Key: "secret",
				},
			},
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, StudioComponentName),
			Annotations: map[string]string{
				"reloader.stakater.com/auto": "true",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: common.SelectorLabels(project, StudioComponentName),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: common.ComponentLabels(project, StudioComponentName),
					Annotations: map[string]string{
						"reloader.stakater.com/auto": "true",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            StudioComponentName,
							Image:           fmt.Sprintf("%s:%s", DefaultStudioImage, imageTag),
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env:             env,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: StudioPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/api/profile",
										Port: intstr.FromInt(StudioPort),
									},
								},
								InitialDelaySeconds: 15,
								PeriodSeconds:       10,
								TimeoutSeconds:      5,
								FailureThreshold:    3,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/api/profile",
										Port: intstr.FromInt(StudioPort),
									},
								},
								InitialDelaySeconds: 10,
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
