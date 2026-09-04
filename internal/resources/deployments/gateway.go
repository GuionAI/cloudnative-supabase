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
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/defaults"
	secretresources "github.com/GuionAI/cloudnative-supabase/internal/resources/secrets"
)

const (
	// GatewayComponentName is the managed Envoy gateway component label.
	GatewayComponentName = "gateway"
	// GatewayPort is the public HTTP listener port.
	GatewayPort int32 = 8000
	// GatewayAdminPort is the Envoy admin/readiness port.
	GatewayAdminPort int32 = 9901
	// GatewayHealthPath is a harmless direct-response route on the public
	// listener. It lets kubelet probe Envoy without exposing the loopback-only
	// admin interface (which contains rendered credential material).
	GatewayHealthPath = "/_internal/health"
)

// GatewayDeploymentName returns the gateway deployment identity.
func GatewayDeploymentName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-api-gw"
}

// DefaultGatewayResources returns conservative resources for Envoy.
func DefaultGatewayResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("128Mi"),
			corev1.ResourceCPU:    resource.MustParse("100m"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("256Mi"),
			corev1.ResourceCPU:    resource.MustParse("500m"),
		},
	}
}

// NormalizeGatewayResources applies the gateway defaults when no resources
// were requested.
func NormalizeGatewayResources(resources corev1.ResourceRequirements) corev1.ResourceRequirements {
	if len(resources.Requests) == 0 && len(resources.Limits) == 0 {
		return DefaultGatewayResources()
	}
	return resources
}

// BuildGatewayDeployment creates Envoy and wires only opaque project keys and
// their corresponding internal role JWTs into the container.
func BuildGatewayDeployment(project *supabasev1alpha1.SupabaseProject) *appsv1.Deployment {
	spec := project.Spec.Gateway
	imageTag := defaults.EnvoyTag
	if spec.ImageTag != "" {
		imageTag = spec.ImageTag
	}
	replicas := NormalizeReplicas(spec.Replicas)
	container := corev1.Container{
		Name:            GatewayComponentName,
		Image:           fmt.Sprintf("%s:%s", defaults.EnvoyImage, imageTag),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/bin/sh", "-ec"},
		Args:            []string{"cp /config/envoy.yaml /config/cds.yaml /config/lds.template.yaml /config/docker-entrypoint.sh /etc/envoy/ && exec /bin/sh /config/docker-entrypoint.sh"},
		Env: []corev1.EnvVar{
			secretEnv("SUPABASE_PUBLISHABLE_KEY", project.Spec.ProjectCredentialsSecret, secretresources.ProjectCredentialsPublishableKey),
			secretEnv("SUPABASE_SECRET_KEY", project.Spec.ProjectCredentialsSecret, secretresources.ProjectCredentialsSecretKey),
			secretEnv("SUPABASE_ANON_ROLE_JWT", project.Spec.ProjectCredentialsSecret, secretresources.ProjectCredentialsAnonRoleJWTKey),
			secretEnv("SUPABASE_SERVICE_ROLE_JWT", project.Spec.ProjectCredentialsSecret, secretresources.ProjectCredentialsServiceRoleJWTKey),
		},
		Ports: []corev1.ContainerPort{
			{Name: "http", ContainerPort: GatewayPort, Protocol: corev1.ProtocolTCP},
		},
		LivenessProbe: BuildHTTPProbe(ProbeConfig{
			Path: GatewayHealthPath, Port: GatewayPort, InitialDelaySeconds: 10, PeriodSeconds: 10, TimeoutSeconds: 3,
		}),
		ReadinessProbe: BuildHTTPProbe(ProbeConfig{
			Path: GatewayHealthPath, Port: GatewayPort, InitialDelaySeconds: 5, PeriodSeconds: 5, TimeoutSeconds: 3,
		}),
		Resources: NormalizeGatewayResources(spec.Resources),
		VolumeMounts: []corev1.VolumeMount{
			{Name: "envoy-assets", MountPath: "/config", ReadOnly: true},
			{Name: "envoy-runtime", MountPath: "/etc/envoy"},
		},
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: GatewayDeploymentName(project), Namespace: project.Namespace,
			Labels: common.ComponentLabels(project, GatewayComponentName), Annotations: common.ReloaderAnnotations(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: common.SelectorLabels(project, GatewayComponentName)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: common.ComponentLabels(project, GatewayComponentName), Annotations: common.ReloaderAnnotations()},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{container},
					Volumes: []corev1.Volume{
						{Name: "envoy-assets", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: common.EnvoyConfigMapName(project)}}}},
						{Name: "envoy-runtime", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					},
				},
			},
		},
	}
	AddImagePullSecrets(&deployment.Spec.Template.Spec, project)
	return deployment
}

func secretEnv(name, secretName, key string) corev1.EnvVar {
	return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: key,
	}}}
}
