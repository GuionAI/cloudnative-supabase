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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/cnpg"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/configmaps"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/defaults"
)

const (
	PowersyncAPIComponentName         = "powersync-api"
	PowersyncReplicationComponentName = "powersync-replication"
	PowersyncCompactComponentName     = "powersync-compact"
	PowersyncHTTPPort                 int32 = 8080
	PowersyncMetricsPort              int32 = 9464
)

// PowersyncAPIDeploymentName returns the Powersync API deployment name
func PowersyncAPIDeploymentName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-powersync-api"
}

// PowersyncReplicationDeploymentName returns the Powersync replication deployment name
func PowersyncReplicationDeploymentName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-powersync-replication"
}

// PowersyncCompactCronJobName returns the Powersync compact CronJob name
func PowersyncCompactCronJobName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-powersync-compact"
}

// DefaultPowersyncAPIResources returns default resource requirements for Powersync API
func DefaultPowersyncAPIResources() corev1.ResourceRequirements {
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

// DefaultPowersyncReplicationResources returns default resource requirements for Powersync replication
func DefaultPowersyncReplicationResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("256Mi"),
			corev1.ResourceCPU:    resource.MustParse("100m"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("768Mi"),
			corev1.ResourceCPU:    resource.MustParse("500m"),
		},
	}
}

// BuildPowersyncAPIDeployment creates the Powersync API deployment (client-facing)
func BuildPowersyncAPIDeployment(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) *appsv1.Deployment {
	spec := project.Spec.Powersync
	name := PowersyncAPIDeploymentName(project)
	image := ResolveImage(spec.Image, defaults.PowersyncImage, defaults.PowersyncTag)
	pullPolicy := ResolvePullPolicy(spec.Image)
	replicas := NormalizeReplicas(spec.API.Replicas)
	resources := normalizePowersyncResources(spec.API.Resources, DefaultPowersyncAPIResources())

	nodeOptions := spec.API.NodeOptions
	if nodeOptions == "" {
		nodeOptions = "--max-old-space-size=330"
	}

	env := buildPowersyncEnv(project, secretNames, nodeOptions)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   project.Namespace,
			Labels:      common.ComponentLabels(project, PowersyncAPIComponentName),
			Annotations: common.ReloaderAnnotations(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: common.SelectorLabels(project, PowersyncAPIComponentName),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      common.ComponentLabels(project, PowersyncAPIComponentName),
					Annotations: common.ReloaderAnnotations(),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "powersync-api",
							Image:           image,
							ImagePullPolicy: pullPolicy,
							Command:         []string{"node", "entry-api.js"},
							Env:             env,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: PowersyncHTTPPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "metrics",
									ContainerPort: PowersyncMetricsPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							LivenessProbe:  BuildLivenessProbe("/api/status", PowersyncHTTPPort),
							ReadinessProbe: BuildReadinessProbe("/api/status", PowersyncHTTPPort),
							Resources:      resources,
							VolumeMounts:   powersyncVolumeMounts(),
						},
					},
					Volumes: powersyncVolumes(project),
				},
			},
		},
	}

	AddImagePullSecrets(&deployment.Spec.Template.Spec, project)
	return deployment
}

// BuildPowersyncReplicationDeployment creates the Powersync replication deployment (CDC processing)
func BuildPowersyncReplicationDeployment(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) *appsv1.Deployment {
	spec := project.Spec.Powersync
	name := PowersyncReplicationDeploymentName(project)
	image := ResolveImage(spec.Image, defaults.PowersyncImage, defaults.PowersyncTag)
	pullPolicy := ResolvePullPolicy(spec.Image)
	var replicas int32 = 1 // Replication is always single instance
	resources := normalizePowersyncResources(spec.Replication.Resources, DefaultPowersyncReplicationResources())

	nodeOptions := spec.Replication.NodeOptions
	if nodeOptions == "" {
		nodeOptions = "--max-old-space-size=482"
	}

	env := buildPowersyncEnv(project, secretNames, nodeOptions)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   project.Namespace,
			Labels:      common.ComponentLabels(project, PowersyncReplicationComponentName),
			Annotations: common.ReloaderAnnotations(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: common.SelectorLabels(project, PowersyncReplicationComponentName),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      common.ComponentLabels(project, PowersyncReplicationComponentName),
					Annotations: common.ReloaderAnnotations(),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "powersync-replication",
							Image:           image,
							ImagePullPolicy: pullPolicy,
							Command:         []string{"node", "entry-replication.js"},
							Env:             env,
							Ports: []corev1.ContainerPort{
								{
									Name:          "metrics",
									ContainerPort: PowersyncMetricsPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Resources:    resources,
							VolumeMounts: powersyncVolumeMounts(),
						},
					},
					Volumes: powersyncVolumes(project),
				},
			},
		},
	}

	AddImagePullSecrets(&deployment.Spec.Template.Spec, project)
	return deployment
}

// BuildPowersyncCompactCronJob creates the Powersync compact CronJob
func BuildPowersyncCompactCronJob(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) *batchv1.CronJob {
	spec := project.Spec.Powersync

	if !spec.Compact.Enabled {
		return nil
	}

	name := PowersyncCompactCronJobName(project)
	image := ResolveImage(spec.Image, defaults.PowersyncImage, defaults.PowersyncTag)
	pullPolicy := ResolvePullPolicy(spec.Image)
	resources := normalizePowersyncResources(spec.Compact.Resources, DefaultPowersyncAPIResources())

	schedule := spec.Compact.Schedule
	if schedule == "" {
		schedule = "0 3 * * *"
	}

	env := buildPowersyncEnv(project, secretNames, "--max-old-space-size=330")

	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, PowersyncCompactComponentName),
		},
		Spec: batchv1.CronJobSpec{
			Schedule: schedule,
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: common.ComponentLabels(project, PowersyncCompactComponentName),
						},
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyOnFailure,
							Containers: []corev1.Container{
								{
									Name:            "powersync-compact",
									Image:           image,
									ImagePullPolicy: pullPolicy,
									Command:         []string{"node", "entry-compact.js"},
									Env:             env,
									Resources:       resources,
									VolumeMounts:    powersyncVolumeMounts(),
								},
							},
							Volumes: powersyncVolumes(project),
						},
					},
				},
			},
		},
	}
}

// buildPowersyncEnv builds environment variables shared by all Powersync containers
func buildPowersyncEnv(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus, nodeOptions string) []corev1.EnvVar {
	dbHost := cnpg.ClusterRWServiceName(project)

	return []corev1.EnvVar{
		{Name: "POWERSYNC_CONFIG_PATH", Value: "/powersync/config/config.json"},
		{Name: "NODE_OPTIONS", Value: nodeOptions},
		// Database password from secret for connection string construction
		{
			Name: "PS_PG_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.PowersyncStoragePassword,
					},
					Key: "password",
				},
			},
		},
		// PowerSync resolves {{ env.VAR }} in config.json
		{
			Name:  "PS_POWERSYNC_STORAGE_URI",
			Value: fmt.Sprintf("postgresql://powersync_storage:$(PS_PG_PASSWORD)@%s:5432/supabase?sslmode=disable", dbHost),
		},
		{
			Name:  "PS_POWERSYNC_REPLICATION_URI",
			Value: fmt.Sprintf("postgresql://powersync_storage:$(PS_PG_PASSWORD)@%s:5432/supabase?sslmode=disable", dbHost),
		},
		// JWT secret for client authentication
		{
			Name: "PS_JWT_SECRET",
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
}

// powersyncVolumeMounts returns the shared volume mounts for Powersync containers
func powersyncVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			Name:      "config",
			MountPath: "/powersync/config",
			ReadOnly:  true,
		},
		{
			Name:      "sync-rules",
			MountPath: "/powersync/sync_rules",
			ReadOnly:  true,
		},
	}
}

// powersyncVolumes returns the shared volumes for Powersync pods
func powersyncVolumes(project *supabasev1alpha1.SupabaseProject) []corev1.Volume {
	return []corev1.Volume{
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configmaps.PowersyncConfigMapName(project),
					},
				},
			},
		},
		{
			Name: "sync-rules",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configmaps.SyncRulesConfigMapName(project),
					},
				},
			},
		},
	}
}

func normalizePowersyncResources(resources corev1.ResourceRequirements, defaults corev1.ResourceRequirements) corev1.ResourceRequirements {
	if len(resources.Requests) == 0 && len(resources.Limits) == 0 {
		return defaults
	}
	return resources
}
