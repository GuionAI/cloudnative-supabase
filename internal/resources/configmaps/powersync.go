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

package configmaps

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
)

const (
	PowersyncConfigComponentName    = "powersync-config"
	PowersyncSyncRulesComponentName = "powersync-sync-rules"
)

// PowersyncConfigMapName returns the Powersync config ConfigMap name
func PowersyncConfigMapName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-powersync-config"
}

// PowersyncSyncRulesConfigMapName returns the sync rules ConfigMap name
func PowersyncSyncRulesConfigMapName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-powersync-sync-rules"
}

// BuildPowersyncConfigMap creates the PowerSync config.yaml ConfigMap.
// PowerSync's !env tag resolves the database URIs at runtime without putting
// credentials in the ConfigMap.
func BuildPowersyncConfigMap(project *supabasev1alpha1.SupabaseProject) *corev1.ConfigMap {
	configYAML := fmt.Sprintf(`storage:
  type: postgresql
  uri: !env PS_POWERSYNC_STORAGE_URI
replication:
  connections:
    - type: postgresql
      uri: !env PS_POWERSYNC_REPLICATION_URI
      tag: default
dev:
  demo_auth: false
client_auth:
  supabase: false
  jwks_uri: %q
  audience:
    - authenticated
migrations:
  disable_auto_migration: false
port: 8080
sync_rules:
  path: /powersync/sync_rules/sync_rules.yaml
  exit_on_error: true
telemetry:
  disable_telemetry_sharing: false
`, common.AuthJWKSURL(project.Spec.Auth.ExternalURL))

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PowersyncConfigMapName(project),
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, PowersyncConfigComponentName),
		},
		Data: map[string]string{
			"config.yaml": configYAML,
		},
	}
}

// BuildPowersyncSyncRulesConfigMap creates the sync rules ConfigMap.
// Returns nil if an external ConfigMapRef is specified (the deployment references it directly).
func BuildPowersyncSyncRulesConfigMap(project *supabasev1alpha1.SupabaseProject) *corev1.ConfigMap {
	spec := project.Spec.Powersync

	// If using external ConfigMap reference, don't create our own
	if spec.SyncRules.ConfigMapRef != "" {
		return nil
	}

	// An empty sync config would either fail startup or accidentally broaden access
	// if a permissive default were used. Admission validation also rejects this case.
	syncRules := spec.SyncRules.Inline
	if syncRules == "" {
		return nil
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PowersyncSyncRulesConfigMapName(project),
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, PowersyncSyncRulesComponentName),
		},
		Data: map[string]string{
			"sync_rules.yaml": syncRules,
		},
	}
}

// SyncRulesConfigMapName returns the actual ConfigMap name for sync rules
// (either operator-generated or user-provided external)
func SyncRulesConfigMapName(project *supabasev1alpha1.SupabaseProject) string {
	if project.Spec.Powersync.SyncRules.ConfigMapRef != "" {
		return project.Spec.Powersync.SyncRules.ConfigMapRef
	}
	return PowersyncSyncRulesConfigMapName(project)
}
