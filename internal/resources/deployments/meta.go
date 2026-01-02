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
	// MetaComponentName is the name of the meta component
	MetaComponentName = "meta"

	// MetaPort is the port postgres-meta listens on
	MetaPort = 8080

	// DefaultMetaImage is the default postgres-meta image
	DefaultMetaImage = "supabase/postgres-meta"

	// DefaultMetaTag is the default postgres-meta image tag
	DefaultMetaTag = "v0.84.2"
)

// MetaDeploymentName returns the meta deployment name
func MetaDeploymentName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-meta"
}

// BuildMetaDeployment creates the postgres-meta deployment
func BuildMetaDeployment(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) *appsv1.Deployment {
	spec := &project.Spec.Meta
	name := MetaDeploymentName(project)
	dbHost := cnpg.ClusterRWServiceName(project)

	// Determine image tag
	imageTag := DefaultMetaTag
	if spec.ImageTag != "" {
		imageTag = spec.ImageTag
	}

	replicas := spec.Replicas
	if replicas == 0 {
		replicas = 1
	}

	env := []corev1.EnvVar{
		// Database configuration
		{Name: "PG_META_DB_HOST", Value: dbHost},
		{Name: "PG_META_DB_PORT", Value: "5432"},
		{Name: "PG_META_DB_NAME", Value: common.DatabaseName},
		{Name: "PG_META_DB_SSL_MODE", Value: "disable"},

		// Server configuration
		{Name: "PG_META_PORT", Value: fmt.Sprintf("%d", MetaPort)},

		// Database credentials
		{
			Name: "PG_META_DB_USER",
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
			Name: "PG_META_DB_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.SupabaseAdmin,
					},
					Key: "password",
				},
			},
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, MetaComponentName),
			Annotations: map[string]string{
				"reloader.stakater.com/auto": "true",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: common.SelectorLabels(project, MetaComponentName),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: common.ComponentLabels(project, MetaComponentName),
					Annotations: map[string]string{
						"reloader.stakater.com/auto": "true",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            MetaComponentName,
							Image:           fmt.Sprintf("%s:%s", DefaultMetaImage, imageTag),
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env:             env,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: MetaPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(MetaPort),
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
										Port: intstr.FromInt(MetaPort),
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
