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

package common

import (
	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

const (
	// LabelManagedBy is the label key for identifying the manager
	LabelManagedBy = "app.kubernetes.io/managed-by"

	// LabelInstance is the label key for the instance name
	LabelInstance = "app.kubernetes.io/instance"

	// LabelComponent is the label key for the component
	LabelComponent = "app.kubernetes.io/component"

	// LabelPartOf is the label key for the application name
	LabelPartOf = "app.kubernetes.io/part-of"

	// LabelName is the label key for the name
	LabelName = "app.kubernetes.io/name"

	// ManagerName is the value for LabelManagedBy
	ManagerName = "cloudnative-supabase"

	// DatabaseName is the default database name for Supabase
	DatabaseName = "supabase"

	// ReloaderAnnotation is the annotation key for stakater/reloader auto-reload
	ReloaderAnnotation = "reloader.stakater.com/auto"
)

// ReloaderAnnotations returns the annotations map for enabling stakater/reloader
func ReloaderAnnotations() map[string]string {
	return map[string]string{ReloaderAnnotation: "true"}
}

// CommonLabels returns the common labels for all resources
func CommonLabels(project *supabasev1alpha1.SupabaseProject) map[string]string {
	return map[string]string{
		LabelManagedBy: ManagerName,
		LabelInstance:  project.Name,
		LabelPartOf:    "supabase",
	}
}

// ComponentLabels returns labels for a specific component
func ComponentLabels(project *supabasev1alpha1.SupabaseProject, component string) map[string]string {
	labels := CommonLabels(project)
	labels[LabelComponent] = component
	labels[LabelName] = component
	return labels
}

// SelectorLabels returns selector labels for a specific component
func SelectorLabels(project *supabasev1alpha1.SupabaseProject, component string) map[string]string {
	return map[string]string{
		LabelInstance:  project.Name,
		LabelComponent: component,
	}
}

// MergeLabels merges multiple label maps
func MergeLabels(maps ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}
