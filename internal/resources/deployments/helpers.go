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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
)

// ProbeConfig holds configuration for building HTTP probes.
// All time-related fields are in seconds.
type ProbeConfig struct {
	// Path is the HTTP endpoint to probe (e.g., "/health", "/ready")
	Path string
	// Port is the container port to probe (must be > 0)
	Port int32
	// InitialDelaySeconds is how long to wait before first probe (typically 5-30s)
	InitialDelaySeconds int32
	// PeriodSeconds is how often to perform the probe (typically 5-15s)
	PeriodSeconds int32
	// TimeoutSeconds is how long to wait for probe response (typically 1-5s)
	TimeoutSeconds int32
}

// DefaultLivenessConfig returns default liveness probe settings
func DefaultLivenessConfig(path string, port int32) ProbeConfig {
	return ProbeConfig{
		Path:                path,
		Port:                port,
		InitialDelaySeconds: 10,
		PeriodSeconds:       10,
		TimeoutSeconds:      5,
	}
}

// DefaultReadinessConfig returns default readiness probe settings
func DefaultReadinessConfig(path string, port int32) ProbeConfig {
	return ProbeConfig{
		Path:                path,
		Port:                port,
		InitialDelaySeconds: 5,
		PeriodSeconds:       5,
		TimeoutSeconds:      3,
	}
}

// BuildHTTPProbe creates an HTTP probe with the given configuration
func BuildHTTPProbe(config ProbeConfig) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: config.Path,
				Port: intstr.FromInt32(config.Port),
			},
		},
		InitialDelaySeconds: config.InitialDelaySeconds,
		PeriodSeconds:       config.PeriodSeconds,
		TimeoutSeconds:      config.TimeoutSeconds,
		FailureThreshold:    3,
	}
}

// BuildLivenessProbe creates a liveness probe with default settings
func BuildLivenessProbe(path string, port int32) *corev1.Probe {
	return BuildHTTPProbe(DefaultLivenessConfig(path, port))
}

// BuildReadinessProbe creates a readiness probe with default settings
func BuildReadinessProbe(path string, port int32) *corev1.Probe {
	return BuildHTTPProbe(DefaultReadinessConfig(path, port))
}

// NormalizeReplicas returns 1 if replicas is 0, otherwise returns replicas
func NormalizeReplicas(replicas int32) int32 {
	if replicas == 0 {
		return 1
	}
	return replicas
}

// AddImagePullSecrets adds image pull secrets to the pod spec if configured
func AddImagePullSecrets(podSpec *corev1.PodSpec, project *supabasev1alpha1.SupabaseProject) {
	if len(project.Spec.ImagePullSecrets) > 0 {
		podSpec.ImagePullSecrets = project.Spec.ImagePullSecrets
	}
}

// BuildDatabaseEnv creates the common database environment variables used by Auth and Rest
func BuildDatabaseEnv(dbHost string) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "DB_HOST", Value: dbHost},
		{Name: "DB_PORT", Value: "5432"},
		{Name: "DB_DRIVER", Value: "postgres"},
		{Name: "DB_SSL", Value: "disable"},
		{Name: "DB_NAME", Value: common.DatabaseName},
	}
}

// DefaultKongResources returns default resource requirements for Kong.
// Kong handles all API traffic and needs more resources than other services.
func DefaultKongResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("512Mi"),
			corev1.ResourceCPU:    resource.MustParse("50m"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("1Gi"),
			corev1.ResourceCPU:    resource.MustParse("500m"),
		},
	}
}

// NormalizeKongResources returns the provided resources if set, otherwise returns defaults
func NormalizeKongResources(resources corev1.ResourceRequirements) corev1.ResourceRequirements {
	if len(resources.Requests) == 0 && len(resources.Limits) == 0 {
		return DefaultKongResources()
	}
	return resources
}
