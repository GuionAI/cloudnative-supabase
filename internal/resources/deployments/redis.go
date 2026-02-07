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
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/defaults"
)

const (
	RedisComponentName = "sequin-redis"
	RedisPort          int32 = 6379
)

// SequinRedisStatefulSetName returns the bundled Redis StatefulSet name
func SequinRedisStatefulSetName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-sequin-redis"
}

// SequinRedisServiceName returns the bundled Redis service name
func SequinRedisServiceName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-sequin-redis"
}

// DefaultRedisResources returns default resource requirements for bundled Redis
func DefaultRedisResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("128Mi"),
			corev1.ResourceCPU:    resource.MustParse("50m"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("256Mi"),
			corev1.ResourceCPU:    resource.MustParse("200m"),
		},
	}
}

// BuildSequinRedisStatefulSet creates a minimal single-replica Redis StatefulSet for Sequin.
// Matches the flicknote-deploy Redis chart: AOF persistence, non-root, TCP+CLI probes.
func BuildSequinRedisStatefulSet(project *supabasev1alpha1.SupabaseProject) *appsv1.StatefulSet {
	spec := project.Spec.Sequin
	name := SequinRedisStatefulSetName(project)
	image := fmt.Sprintf("%s:%s", defaults.RedisImage, defaults.RedisTag)

	resources := normalizeRedisResources(spec.Redis.Resources)

	storageSize := spec.Redis.Storage.Size
	if storageSize == "" {
		storageSize = "2Gi"
	}

	var replicas int32 = 1

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, RedisComponentName),
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: name,
			Replicas:    &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: common.SelectorLabels(project, RedisComponentName),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: common.ComponentLabels(project, RedisComponentName),
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true),
						RunAsUser:    ptr.To(int64(999)),
						RunAsGroup:   ptr.To(int64(1000)),
						FSGroup:      ptr.To(int64(1000)),
					},
					Containers: []corev1.Container{
						{
							Name:  "redis",
							Image: image,
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
							Command: []string{"redis-server", "--appendonly", "yes"},
							Ports: []corev1.ContainerPort{
								{
									Name:          "redis",
									ContainerPort: RedisPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Resources: resources,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									MountPath: "/data",
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.FromString("redis"),
									},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       10,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"redis-cli", "ping"},
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
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
	if spec.Redis.Storage.StorageClass != "" {
		sc := spec.Redis.Storage.StorageClass
		sts.Spec.VolumeClaimTemplates[0].Spec.StorageClassName = &sc
	}

	AddImagePullSecrets(&sts.Spec.Template.Spec, project)
	return sts
}

// BuildSequinRedisService creates the ClusterIP service for bundled Redis
func BuildSequinRedisService(project *supabasev1alpha1.SupabaseProject) *corev1.Service {
	name := SequinRedisServiceName(project)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, RedisComponentName),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: common.SelectorLabels(project, RedisComponentName),
			Ports: []corev1.ServicePort{
				{
					Name:       "redis",
					Port:       RedisPort,
					TargetPort: intstr.FromString("redis"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

func normalizeRedisResources(resources corev1.ResourceRequirements) corev1.ResourceRequirements {
	if len(resources.Requests) == 0 && len(resources.Limits) == 0 {
		return DefaultRedisResources()
	}
	return resources
}
