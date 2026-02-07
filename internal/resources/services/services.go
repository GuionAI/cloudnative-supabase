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

package services

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
)

// BuildService creates a ClusterIP service for a component
func BuildService(project *supabasev1alpha1.SupabaseProject, name, component string, port int32) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, component),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: common.SelectorLabels(project, component),
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       port,
					TargetPort: intstr.FromInt(int(port)),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// BuildAuthService creates the service for GoTrue auth
func BuildAuthService(project *supabasev1alpha1.SupabaseProject) *corev1.Service {
	return BuildService(project, project.Name+"-auth", "auth", 9999)
}

// BuildRestService creates the service for PostgREST
func BuildRestService(project *supabasev1alpha1.SupabaseProject) *corev1.Service {
	return BuildService(project, project.Name+"-rest", "rest", 3000)
}

// BuildStudioService creates the service for Studio
func BuildStudioService(project *supabasev1alpha1.SupabaseProject) *corev1.Service {
	return BuildService(project, project.Name+"-studio", "studio", 3000)
}

// BuildMetaService creates the service for postgres-meta
func BuildMetaService(project *supabasev1alpha1.SupabaseProject) *corev1.Service {
	return BuildService(project, project.Name+"-meta", "meta", 8080)
}

// BuildSequinService creates the service for Sequin with HTTP and metrics ports
func BuildSequinService(project *supabasev1alpha1.SupabaseProject) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      project.Name + "-sequin",
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, "sequin"),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: common.SelectorLabels(project, "sequin"),
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       7376,
					TargetPort: intstr.FromInt(7376),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "metrics",
					Port:       4000,
					TargetPort: intstr.FromInt(4000),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// BuildPowersyncAPIService creates the service for Powersync API with HTTP and metrics ports
func BuildPowersyncAPIService(project *supabasev1alpha1.SupabaseProject) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      project.Name + "-powersync-api",
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, "powersync-api"),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: common.SelectorLabels(project, "powersync-api"),
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "metrics",
					Port:       9464,
					TargetPort: intstr.FromInt(9464),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// BuildKongService creates the service for Kong
func BuildKongService(project *supabasev1alpha1.SupabaseProject) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      project.Name + "-kong",
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, "kong"),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: common.SelectorLabels(project, "kong"),
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       8000,
					TargetPort: intstr.FromInt(8000),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "https",
					Port:       8443,
					TargetPort: intstr.FromInt(8443),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}
