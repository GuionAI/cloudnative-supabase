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
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/defaults"
)

const (
	// KongComponentName is the name of the kong component
	KongComponentName = "kong"

	// KongProxyPort is the HTTP proxy port
	KongProxyPort = 8000

	// KongProxySSLPort is the HTTPS proxy port
	KongProxySSLPort = 8443

	// KongAdminPort is the admin API port
	KongAdminPort = 8001
)

// KongDeploymentName returns the kong deployment name
func KongDeploymentName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-kong"
}

// BuildKongDeployment creates the Kong API gateway deployment
func BuildKongDeployment(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) *appsv1.Deployment {
	spec := &project.Spec.Kong
	name := KongDeploymentName(project)

	// Determine image tag
	imageTag := defaults.KongTag
	if spec.ImageTag != "" {
		imageTag = spec.ImageTag
	}

	replicas := NormalizeReplicas(spec.Replicas)

	env := []corev1.EnvVar{
		// Kong configuration
		{Name: "KONG_DATABASE", Value: "off"},
		{Name: "KONG_DECLARATIVE_CONFIG", Value: "/kong/config/kong.yml"},
		{Name: "KONG_DNS_ORDER", Value: "LAST,A,CNAME"},
		{Name: "KONG_NGINX_WORKER_PROCESSES", Value: "1"},
		{Name: "KONG_PLUGINS", Value: "request-transformer,cors,key-auth,acl,basic-auth"},
		{Name: "KONG_NGINX_PROXY_PROXY_BUFFER_SIZE", Value: "160k"},
		{Name: "KONG_NGINX_PROXY_PROXY_BUFFERS", Value: "64 160k"},

		// Proxy configuration
		{Name: "KONG_PROXY_LISTEN", Value: fmt.Sprintf("0.0.0.0:%d, 0.0.0.0:%d ssl", KongProxyPort, KongProxySSLPort)},
		{Name: "KONG_ADMIN_LISTEN", Value: fmt.Sprintf("0.0.0.0:%d", KongAdminPort)},

		// Logging
		{Name: "KONG_PROXY_ACCESS_LOG", Value: "/dev/stdout"},
		{Name: "KONG_PROXY_ERROR_LOG", Value: "/dev/stderr"},
		{Name: "KONG_ADMIN_ACCESS_LOG", Value: "/dev/stdout"},
		{Name: "KONG_ADMIN_ERROR_LOG", Value: "/dev/stderr"},

		// JWT secrets for validation
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
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   project.Namespace,
			Labels:      common.ComponentLabels(project, KongComponentName),
			Annotations: common.ReloaderAnnotations(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: common.SelectorLabels(project, KongComponentName),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      common.ComponentLabels(project, KongComponentName),
					Annotations: common.ReloaderAnnotations(),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            KongComponentName,
							Image:           fmt.Sprintf("%s:%s", defaults.KongImage, imageTag),
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env:             env,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: KongProxyPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "https",
									ContainerPort: KongProxySSLPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "admin",
									ContainerPort: KongAdminPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							LivenessProbe: BuildHTTPProbe(ProbeConfig{
								Path:                "/status",
								Port:                KongAdminPort,
								InitialDelaySeconds: 15,
								PeriodSeconds:       10,
								TimeoutSeconds:      5,
							}),
							ReadinessProbe: BuildReadinessProbe("/status", KongAdminPort),
							Resources:      NormalizeKongResources(spec.Resources),
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "kong-config",
									MountPath: "/kong/config",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "kong-config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: common.KongConfigMapName(project),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	AddImagePullSecrets(&deployment.Spec.Template.Spec, project)

	return deployment
}
