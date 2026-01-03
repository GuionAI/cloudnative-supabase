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
)

const (
	// MetaComponentName is the name of the meta component
	MetaComponentName = "meta"

	// MetaPort is the port postgres-meta listens on
	MetaPort = 8080
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
	imageTag := defaults.MetaTag
	if spec.ImageTag != "" {
		imageTag = spec.ImageTag
	}

	replicas := NormalizeReplicas(spec.Replicas)

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
			Name:        name,
			Namespace:   project.Namespace,
			Labels:      common.ComponentLabels(project, MetaComponentName),
			Annotations: common.ReloaderAnnotations(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: common.SelectorLabels(project, MetaComponentName),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      common.ComponentLabels(project, MetaComponentName),
					Annotations: common.ReloaderAnnotations(),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            MetaComponentName,
							Image:           fmt.Sprintf("%s:%s", defaults.MetaImage, imageTag),
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env:             env,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: MetaPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							LivenessProbe:  BuildLivenessProbe("/health", MetaPort),
							ReadinessProbe: BuildReadinessProbe("/health", MetaPort),
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
