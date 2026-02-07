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

// powersyncConfig represents the PowerSync service config.json structure
type powersyncConfig struct {
	Storage     powersyncStorage     `json:"storage"`
	Replication powersyncReplication `json:"replication"`
	ClientAuth  powersyncClientAuth  `json:"client_auth"`
	SyncRules   powersyncSyncRules   `json:"sync_rules"`
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
	Supabase          bool     `json:"supabase"`
	SupabaseJWTSecret string   `json:"supabase_jwt_secret"`
	Audience          []string `json:"audience"`
}

type powersyncSyncRules struct {
	Path string `json:"path"`
}

// BuildPowersyncConfigMap creates the PowerSync config.json ConfigMap.
// Database credentials are injected via environment variables that PowerSync resolves at runtime.
// The config uses connection strings with env var placeholders.
// dbHost is the database hostname (e.g., from cnpg.ClusterRWServiceName) - passed as parameter to avoid import cycle.
func BuildPowersyncConfigMap(project *supabasev1alpha1.SupabaseProject, dbHost string) *corev1.ConfigMap {

	// PowerSync config uses connection URIs with credentials from env vars
	// Environment variables PS_STORAGE_URI, PS_REPLICATION_URI, PS_JWT_SECRET
	// are set on the deployment from K8s secrets
	config := powersyncConfig{
		Storage: powersyncStorage{
			Type: "postgresql",
			// Will be overridden by PS_POWERSYNC_STORAGE_URI env var
			URI: fmt.Sprintf("postgresql://powersync_storage@%s:5432/supabase?sslmode=disable", dbHost),
		},
		Replication: powersyncReplication{
			Connections: []powersyncConnection{
				{
					Type: "postgresql",
					// Will be overridden by PS_POWERSYNC_REPLICATION_URI env var
					URI: fmt.Sprintf("postgresql://powersync_storage@%s:5432/supabase?sslmode=disable", dbHost),
					Tag: "default",
				},
			},
		},
		ClientAuth: powersyncClientAuth{
			Supabase:          true,
			SupabaseJWTSecret: "{{ env.PS_JWT_SECRET }}",
			Audience:          []string{"authenticated"},
		},
		SyncRules: powersyncSyncRules{
			Path: "/powersync/sync_rules/sync_rules.yaml",
		},
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

	// Use inline sync rules or default
	syncRules := spec.SyncRules.Inline
	if syncRules == "" {
		syncRules = defaultSyncRules()
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

func defaultSyncRules() string {
	return `bucket_definitions:
  global:
    data:
      - SELECT * FROM public.*
`
}
