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
	"encoding/json"

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

// powersyncConfig represents the PowerSync service config.json structure
type powersyncConfig struct {
	Storage     powersyncStorage     `json:"storage"`
	Replication powersyncReplication `json:"replication"`
	Dev         powersyncDev         `json:"dev"`
	ClientAuth  powersyncClientAuth  `json:"client_auth"`
	Migrations  powersyncMigrations  `json:"migrations"`
	Port        int                  `json:"port"`
	SyncRules   powersyncSyncRules   `json:"sync_rules"`
	Telemetry   powersyncTelemetry   `json:"telemetry"`
}

type powersyncStorage struct {
	Type string `json:"type"`
	URI  string `json:"uri"`
}

type powersyncReplication struct {
	Connections []powersyncConnection `json:"connections"`
}

type powersyncConnection struct {
	Type string `json:"type"`
	URI  string `json:"uri"`
	Tag  string `json:"tag"`
}

type powersyncClientAuth struct {
	// Supabase is kept false so PowerSync does not enable its legacy HMAC
	// integration. JWTs are verified through the public JWKS URI instead.
	Supabase bool     `json:"supabase"`
	JWKSURI  string   `json:"jwks_uri"`
	Audience []string `json:"audience"`
}

type powersyncDev struct {
	DemoAuth bool `json:"demo_auth"`
}

type powersyncMigrations struct {
	DisableAutoMigration bool `json:"disable_auto_migration"`
}

type powersyncSyncRules struct {
	Path        string `json:"path"`
	ExitOnError bool   `json:"exit_on_error"`
}

type powersyncTelemetry struct {
	DisableTelemetrySharing bool `json:"disable_telemetry_sharing"`
}

// BuildPowersyncConfigMap creates the PowerSync config.json ConfigMap.
// Database credentials are injected via environment variable templates that
// PowerSync resolves at runtime.
func BuildPowersyncConfigMap(project *supabasev1alpha1.SupabaseProject) *corev1.ConfigMap {
	config := powersyncConfig{
		Storage: powersyncStorage{
			Type: "postgresql",
			URI:  "{{ env.PS_POWERSYNC_STORAGE_URI }}",
		},
		Replication: powersyncReplication{
			Connections: []powersyncConnection{
				{
					Type: "postgresql",
					URI:  "{{ env.PS_POWERSYNC_REPLICATION_URI }}",
					Tag:  "default",
				},
			},
		},
		Dev: powersyncDev{DemoAuth: false},
		ClientAuth: powersyncClientAuth{
			Supabase: false,
			JWKSURI:  common.AuthJWKSURL(project.Spec.Auth.ExternalURL),
			Audience: []string{"authenticated"},
		},
		Migrations: powersyncMigrations{DisableAutoMigration: false},
		Port:       8080,
		SyncRules: powersyncSyncRules{
			Path:        "/powersync/sync_rules/sync_rules.yaml",
			ExitOnError: true,
		},
		Telemetry: powersyncTelemetry{DisableTelemetrySharing: false},
	}

	configJSON, _ := json.MarshalIndent(config, "", "  ")

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PowersyncConfigMapName(project),
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, PowersyncConfigComponentName),
		},
		Data: map[string]string{
			"config.json": string(configJSON),
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
