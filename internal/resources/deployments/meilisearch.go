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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/defaults"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/secrets"
)

const (
	MeilisearchComponentName = "meilisearch"
	MeilisearchHTTPPort      int32 = 7700
)

// MeilisearchStatefulSetName returns the Meilisearch StatefulSet name
func MeilisearchStatefulSetName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-meilisearch"
}

// DefaultMeilisearchResources returns default resource requirements for Meilisearch
func DefaultMeilisearchResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("512Mi"),
			corev1.ResourceCPU:    resource.MustParse("250m"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("2Gi"),
			corev1.ResourceCPU:    resource.MustParse("500m"),
		},
	}
}

// BuildMeilisearchStatefulSet creates the Meilisearch StatefulSet with persistent storage
func BuildMeilisearchStatefulSet(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) *appsv1.StatefulSet {
	spec := project.Spec.Meilisearch
	name := MeilisearchStatefulSetName(project)
	image := ResolveImage(spec.Image, defaults.MeilisearchImage, defaults.MeilisearchTag)
	pullPolicy := ResolvePullPolicy(spec.Image)
	replicas := NormalizeReplicas(spec.Replicas)
	resources := normalizeMeilisearchResources(spec.Resources)

	// Storage configuration
	storageSize := spec.Persistence.Size
	if storageSize == "" {
		storageSize = "10Gi"
	}

	masterKeySecretName := secrets.MeilisearchSecretName(project)

	env := []corev1.EnvVar{
		{Name: "MEILI_ENV", Value: "production"},
		{Name: "MEILI_NO_ANALYTICS", Value: "true"},
		{Name: "MEILI_EXPERIMENTAL_LOGS_MODE", Value: "json"},
		{Name: "MEILI_DB_PATH", Value: "/meili_data/data.ms"},
		{Name: "MEILI_HTTP_ADDR", Value: "0.0.0.0:7700"},
		{
			Name: "MEILI_MASTER_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: masterKeySecretName,
					},
					Key: "masterKey",
				},
			},
		},
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, MeilisearchComponentName),
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: name,
			Replicas:    &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: common.SelectorLabels(project, MeilisearchComponentName),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: common.ComponentLabels(project, MeilisearchComponentName),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            MeilisearchComponentName,
							Image:           image,
							ImagePullPolicy: pullPolicy,
							Env:             env,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: MeilisearchHTTPPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							LivenessProbe:  BuildLivenessProbe("/health", MeilisearchHTTPPort),
							ReadinessProbe: BuildReadinessProbe("/health", MeilisearchHTTPPort),
							Resources:      resources,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									MountPath: "/meili_data",
								},
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "data",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse(storageSize),
							},
						},
					},
				},
			},
		},
	}

	// Set storage class if specified
	if spec.Persistence.StorageClass != "" {
		sc := spec.Persistence.StorageClass
		sts.Spec.VolumeClaimTemplates[0].Spec.StorageClassName = &sc
	}

	AddImagePullSecrets(&sts.Spec.Template.Spec, project)
	return sts
}

func normalizeMeilisearchResources(resources corev1.ResourceRequirements) corev1.ResourceRequirements {
	if len(resources.Requests) == 0 && len(resources.Limits) == 0 {
		return DefaultMeilisearchResources()
	}
	return resources
}
