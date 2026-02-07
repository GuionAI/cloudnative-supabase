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

package cnpg

import (
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/configmaps"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/defaults"
)

const (
	// OwnerRole is the database owner role
	OwnerRole = "supabase_admin"
)

// ClusterName returns the CNPG cluster name for a project
func ClusterName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-pg"
}

// ClusterRWServiceName returns the read-write service name
func ClusterRWServiceName(project *supabasev1alpha1.SupabaseProject) string {
	return ClusterName(project) + "-rw"
}

// BuildCluster creates a CNPG Cluster resource for the project
func BuildCluster(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) *cnpgv1.Cluster {
	name := ClusterName(project)
	spec := project.Spec.Database

	// Determine image
	image := fmt.Sprintf("%s:%s", defaults.PostgresImage, defaults.PostgresTag)
	if spec.Image != "" {
		image = spec.Image
	}

	cluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, "postgresql"),
		},
		Spec: cnpgv1.ClusterSpec{
			Instances:             int(spec.Instances),
			ImageName:             image,
			EnableSuperuserAccess: ptr.To(spec.EnableSuperuserAccess),

			Bootstrap: buildBootstrapConfiguration(project, secretNames),

			PostgresConfiguration: cnpgv1.PostgresConfiguration{
				AdditionalLibraries: []string{
					"pg_stat_statements",
					"pgaudit",
					"auto_explain",
				},
				Parameters: mergeParameters(defaultParameters(), spec.Parameters),
				PgHBA: []string{
					// Local connections
					"local all all trust",
					// K3s pod CIDR
					"host all all 10.42.0.0/16 scram-sha-256",
					// K3s service CIDR
					"host all all 10.43.0.0/16 scram-sha-256",
					// Allow all private networks with password
					"host all all 10.0.0.0/8 scram-sha-256",
					"host all all 172.16.0.0/12 scram-sha-256",
					"host all all 192.168.0.0/16 scram-sha-256",
				},
			},

			StorageConfiguration: spec.Storage,

			Managed: &cnpgv1.ManagedConfiguration{
				Roles: buildAllRoles(project, secretNames),
			},
		},
	}

	// Add resources if specified
	if spec.Resources.Requests != nil || spec.Resources.Limits != nil {
		cluster.Spec.Resources = spec.Resources
	}

	// Enable pod anti-affinity for HA when multiple instances
	// Uses soft anti-affinity (preferred) so pods still schedule on single-node clusters
	if spec.Instances > 1 {
		cluster.Spec.Affinity = cnpgv1.AffinityConfiguration{
			EnablePodAntiAffinity: ptr.To(true),
		}
	}

	// Add backup configuration if enabled
	if spec.Backup != nil && spec.Backup.Enabled {
		cluster.Spec.Plugins = []cnpgv1.PluginConfiguration{
			{
				Name:          BarmanCloudPluginName,
				IsWALArchiver: ptr.To(true),
				Parameters: map[string]string{
					"barmanObjectName": ObjectStoreName(project),
				},
			},
		}
	}

	// Add external clusters configuration for recovery
	if spec.Recovery != nil && spec.Recovery.Enabled {
		cluster.Spec.ExternalClusters = []cnpgv1.ExternalCluster{
			{
				Name: RecoverySourceName(project),
				PluginConfiguration: &cnpgv1.PluginConfiguration{
					Name: BarmanCloudPluginName,
					Parameters: map[string]string{
						"barmanObjectName": RecoveryObjectStoreName(project),
						"serverName":       spec.Recovery.ServerName,
					},
				},
			},
		}
	}

	return cluster
}

// RecoverySourceName returns the external cluster source name for recovery
func RecoverySourceName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-recovery-source"
}

// buildBootstrapConfiguration creates the bootstrap configuration based on recovery mode
func buildBootstrapConfiguration(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) *cnpgv1.BootstrapConfiguration {
	spec := project.Spec.Database

	// Use recovery bootstrap if recovery is enabled
	if spec.Recovery != nil && spec.Recovery.Enabled {
		recovery := &cnpgv1.BootstrapRecovery{
			Source:   RecoverySourceName(project),
			Database: common.DatabaseName,
			Owner:    OwnerRole,
			Secret: &cnpgv1.LocalObjectReference{
				Name: secretNames.SupabaseAdmin,
			},
		}

		// Set recovery target if targetTime is specified
		if spec.Recovery.TargetTime != "" {
			recovery.RecoveryTarget = &cnpgv1.RecoveryTarget{
				TargetTime: spec.Recovery.TargetTime,
			}
		}

		return &cnpgv1.BootstrapConfiguration{
			Recovery: recovery,
		}
	}

	// Default: use initdb bootstrap
	return &cnpgv1.BootstrapConfiguration{
		InitDB: &cnpgv1.BootstrapInitDB{
			Database: common.DatabaseName,
			Owner:    OwnerRole,
			Secret: &cnpgv1.LocalObjectReference{
				Name: secretNames.SupabaseAdmin,
			},
			PostInitApplicationSQLRefs: &cnpgv1.SQLRefs{
				ConfigMapRefs: []cnpgv1.ConfigMapKeySelector{
					{
						LocalObjectReference: cnpgv1.LocalObjectReference{
							Name: configmaps.InitSQLConfigMapName(project),
						},
						Key: "init.sql",
					},
				},
			},
		},
	}
}

// buildAllRoles combines base Supabase roles with optional CDC/search roles
func buildAllRoles(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) []cnpgv1.RoleConfiguration {
	roles := buildRoles(&project.Spec.Database, secretNames)
	if project.Spec.Sequin != nil && secretNames.SequinPassword != "" {
		roles = append(roles, BuildSequinRoles(secretNames)...)
	}
	if project.Spec.Powersync != nil && secretNames.PowersyncStoragePassword != "" {
		roles = append(roles, BuildPowersyncRoles(secretNames)...)
	}
	return roles
}

// BuildSequinRoles returns additional CNPG roles required for Sequin CDC
func BuildSequinRoles(secretNames *supabasev1alpha1.SecretNamesStatus) []cnpgv1.RoleConfiguration {
	return []cnpgv1.RoleConfiguration{
		{
			Name:   "sequin",
			Ensure: cnpgv1.EnsurePresent,
			Login:  true,
			PasswordSecret: &cnpgv1.LocalObjectReference{
				Name: secretNames.SequinPassword,
			},
			Comment: "Sequin database owner role",
		},
		{
			Name:        "sequin_replication",
			Ensure:      cnpgv1.EnsurePresent,
			Login:       true,
			Replication: true,
			BypassRLS:   true,
			PasswordSecret: &cnpgv1.LocalObjectReference{
				Name: secretNames.SequinReplicationPassword,
			},
			Comment: "Sequin CDC replication role",
		},
	}
}

// BuildPowersyncRoles returns additional CNPG roles required for Powersync
func BuildPowersyncRoles(secretNames *supabasev1alpha1.SecretNamesStatus) []cnpgv1.RoleConfiguration {
	return []cnpgv1.RoleConfiguration{
		{
			Name:   "powersync_storage",
			Ensure: cnpgv1.EnsurePresent,
			Login:  true,
			PasswordSecret: &cnpgv1.LocalObjectReference{
				Name: secretNames.PowersyncStoragePassword,
			},
			Comment: "Powersync storage role",
		},
	}
}

// buildRoles creates the managed roles for Supabase
func buildRoles(spec *supabasev1alpha1.DatabaseSpec, secretNames *supabasev1alpha1.SecretNamesStatus) []cnpgv1.RoleConfiguration {
	roles := []cnpgv1.RoleConfiguration{
		// Group role for API permissions
		{
			Name:    "api_access_role",
			Ensure:  cnpgv1.EnsurePresent,
			Comment: "Group role for API access permissions",
		},
		// Non-login API roles (inherit from api_access_role)
		{
			Name:    "anon",
			Ensure:  cnpgv1.EnsurePresent,
			Inherit: ptr.To(true),
			InRoles: []string{"api_access_role"},
			Comment: "Anonymous role for unauthenticated API access",
		},
		{
			Name:    "authenticated",
			Ensure:  cnpgv1.EnsurePresent,
			Inherit: ptr.To(true),
			InRoles: []string{"api_access_role"},
			Comment: "Role for authenticated API users",
		},
		{
			Name:      "service_role",
			Ensure:    cnpgv1.EnsurePresent,
			Inherit:   ptr.To(true),
			BypassRLS: true,
			InRoles:   []string{"api_access_role"},
			Comment:   "Service role with RLS bypass for backend operations",
		},
		// Login roles with passwords from secrets
		{
			Name:        "supabase_admin",
			Ensure:      cnpgv1.EnsurePresent,
			Login:       true,
			CreateDB:    true,
			CreateRole:  true,
			BypassRLS:   true,
			Replication: true,
			PasswordSecret: &cnpgv1.LocalObjectReference{
				Name: secretNames.SupabaseAdmin,
			},
			InRoles: []string{"pg_read_all_data", "pg_write_all_data"},
			Comment: "Database owner and admin role",
		},
		{
			Name:    "authenticator",
			Ensure:  cnpgv1.EnsurePresent,
			Login:   true,
			Inherit: ptr.To(false),
			PasswordSecret: &cnpgv1.LocalObjectReference{
				Name: secretNames.Authenticator,
			},
			InRoles: []string{"anon", "authenticated", "service_role"},
			Comment: "PostgREST role switcher",
		},
		{
			Name:       "supabase_auth_admin",
			Ensure:     cnpgv1.EnsurePresent,
			Login:      true,
			Inherit:    ptr.To(false),
			CreateRole: true,
			PasswordSecret: &cnpgv1.LocalObjectReference{
				Name: secretNames.AuthAdmin,
			},
			Comment: "GoTrue auth service admin role",
		},
	}

	// Add additional custom roles (directly from CNPG RoleConfiguration)
	roles = append(roles, spec.AdditionalRoles...)

	return roles
}

// defaultParameters returns the default PostgreSQL parameters
func defaultParameters() map[string]string {
	return map[string]string{
		// Enable logical replication for CDC
		"wal_level":             "logical",
		"max_replication_slots": "10",
		"max_wal_senders":       "10",
		"wal_sender_timeout":    "60s",
		"wal_receiver_timeout":  "60s",
		// Performance
		"shared_buffers":   "256MB",
		"log_min_messages": "fatal",
		// pgaudit settings (matching Supabase Cloud)
		"pgaudit.log":                "function, ddl, role",
		"pgaudit.log_catalog":        "on",
		"pgaudit.log_client":         "off",
		"pgaudit.log_level":          "log",
		"pgaudit.log_parameter":      "off",
		"pgaudit.log_relation":       "off",
		"pgaudit.log_rows":           "off",
		"pgaudit.log_statement":      "on",
		"pgaudit.log_statement_once": "off",
		// auto_explain settings (log slow queries)
		"auto_explain.log_min_duration":      "1s",
		"auto_explain.log_analyze":           "on",
		"auto_explain.log_buffers":           "on",
		"auto_explain.log_timing":            "on",
		"auto_explain.log_nested_statements": "on",
	}
}

// mergeParameters merges default parameters with user-provided parameters
func mergeParameters(base, custom map[string]string) map[string]string {
	result := make(map[string]string)
	for k, v := range base {
		result[k] = v
	}
	for k, v := range custom {
		result[k] = v
	}
	return result
}
