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
	PowersyncAPIComponentName               = "powersync-api"
	PowersyncReplicationComponentName       = "powersync-replication"
	PowersyncCompactComponentName           = "powersync-compact"
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
			corev1.ResourceMemory: resource.MustParse("180Mi"),
			corev1.ResourceCPU:    resource.MustParse("100m"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("360Mi"),
			corev1.ResourceCPU:    resource.MustParse("1"),
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
			corev1.ResourceMemory: resource.MustParse("512Mi"),
			corev1.ResourceCPU:    resource.MustParse("1"),
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
		nodeOptions = "--max-old-space-size=150"
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
							Args:            []string{"start", "-r", "api"},
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
							LivenessProbe:  powersyncFileProbe("/app/.probes/poll", 5, 10, 30),
							ReadinessProbe: powersyncFileProbe("/app/.probes/ready", 5, 10, 30),
							StartupProbe:   powersyncFileProbe("/app/.probes/startup", 200, 1, 1),
							Lifecycle:      powersyncLifecycle(),
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
		nodeOptions = "--max-old-space-size=230"
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
							Args:            []string{"start", "-r", "sync"},
							Env:             env,
							Ports: []corev1.ContainerPort{
								{
									Name:          "metrics",
									ContainerPort: PowersyncMetricsPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							LivenessProbe:  powersyncFileProbe("/app/.probes/poll", 5, 10, 30),
							ReadinessProbe: powersyncFileProbe("/app/.probes/ready", 5, 10, 30),
							StartupProbe:   powersyncFileProbe("/app/.probes/startup", 200, 1, 1),
							Lifecycle:      powersyncLifecycle(),
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

	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, PowersyncCompactComponentName),
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   schedule,
			ConcurrencyPolicy:          batchv1.ForbidConcurrent,
			SuccessfulJobsHistoryLimit: int32Ptr(3),
			FailedJobsHistoryLimit:     int32Ptr(1),
			StartingDeadlineSeconds:    int64Ptr(300),
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					BackoffLimit:            int32Ptr(2),
					TTLSecondsAfterFinished: int32Ptr(3600),
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: common.ComponentLabels(project, PowersyncCompactComponentName),
						},
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyNever,
							Containers: []corev1.Container{
								{
									Name:            "powersync-compact",
									Image:           image,
									ImagePullPolicy: pullPolicy,
									Args:            []string{"compact"},
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
	AddImagePullSecrets(&cronJob.Spec.JobTemplate.Spec.Template.Spec, project)
	return cronJob
}

func powersyncFileProbe(path string, failureThreshold, periodSeconds, timeoutSeconds int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{Command: []string{"cat", path}},
		},
		FailureThreshold:    failureThreshold,
		InitialDelaySeconds: 5,
		PeriodSeconds:       periodSeconds,
		TimeoutSeconds:      timeoutSeconds,
	}
}

func powersyncLifecycle() *corev1.Lifecycle {
	return &corev1.Lifecycle{
		PreStop: &corev1.LifecycleHandler{
			Exec: &corev1.ExecAction{Command: []string{"sh", "-c", "sleep 5"}},
		},
	}
}

// buildPowersyncEnv builds environment variables shared by all Powersync containers
func buildPowersyncEnv(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus, nodeOptions string) []corev1.EnvVar {
	dbHost := cnpg.ClusterRWServiceName(project)

	return []corev1.EnvVar{
		{Name: "POWERSYNC_CONFIG_PATH", Value: "/powersync/config/config.json"},
		{Name: "NODE_OPTIONS", Value: nodeOptions},
		{Name: "LOG_FORMAT", Value: "json"},
		{Name: "METRICS_PORT", Value: "9464"},
		{Name: "MICRO_ENVIRONMENT_NAME", Value: "production"},
		{Name: "MICRO_PROBE_TYPE", Value: "fs"},
		{Name: "MICRO_SERVICE_NAME", Value: "powersync"},
		// Storage password (powersync_storage role — internal sync state tables)
		{
			Name: "PS_STORAGE_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.PowersyncStoragePassword,
					},
					Key: "password",
				},
			},
		},
		// Replication password (powersync_replication role — CDC/WAL reading)
		{
			Name: "PS_REPLICATION_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.PowersyncReplicationPassword,
					},
					Key: "password",
				},
			},
		},
		// PowerSync resolves {{ env.VAR }} in config.json
		{
			Name:  "PS_POWERSYNC_STORAGE_URI",
			Value: fmt.Sprintf("postgresql://powersync_storage:$(PS_STORAGE_PASSWORD)@%s:5432/supabase?sslmode=disable", dbHost),
		},
		{
			Name:  "PS_POWERSYNC_REPLICATION_URI",
			Value: fmt.Sprintf("postgresql://powersync_replication:$(PS_REPLICATION_PASSWORD)@%s:5432/supabase?sslmode=disable", dbHost),
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

func normalizePowersyncResources(resources corev1.ResourceRequirements, fallback corev1.ResourceRequirements) corev1.ResourceRequirements {
	if len(resources.Requests) == 0 && len(resources.Limits) == 0 {
		return fallback
	}
	return resources
}

func int32Ptr(value int32) *int32 {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}
