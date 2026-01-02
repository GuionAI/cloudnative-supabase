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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/configmaps"
)

const (
	// DefaultPostgresImage is the default PostgreSQL image for CNPG
	DefaultPostgresImage = "ghcr.io/cloudnative-pg/postgresql:17"

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
	image := DefaultPostgresImage
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

			Bootstrap: &cnpgv1.BootstrapConfiguration{
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
			},

			// Environment variables for JWT substitution in init SQL
			Env: []corev1.EnvVar{
				{
					Name: "JWT_SECRET",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: secretNames.JWT,
							},
							Key: "secret",
						},
					},
				},
				{
					Name:  "JWT_EXP",
					Value: fmt.Sprintf("%d", common.GetJWTExpiration(project)),
				},
			},

			PostgresConfiguration: cnpgv1.PostgresConfiguration{
				AdditionalLibraries: []string{
					"pg_stat_statements",
					"pgaudit",
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

			StorageConfiguration: cnpgv1.StorageConfiguration{
				Size:         spec.Storage.Size,
				StorageClass: ptrStringOrNil(spec.Storage.StorageClass),
			},

			Managed: &cnpgv1.ManagedConfiguration{
				Roles: buildRoles(project, secretNames),
			},
		},
	}

	// Add resources if specified
	if spec.Resources.Requests != nil || spec.Resources.Limits != nil {
		cluster.Spec.Resources = spec.Resources
	}

	// Add backup configuration if enabled
	if spec.Backup != nil && spec.Backup.Enabled {
		cluster.Spec.Plugins = []cnpgv1.PluginConfiguration{
			{
				Name:          "barman-cloud.cloudnative-pg.io",
				IsWALArchiver: ptr.To(true),
				Parameters: map[string]string{
					"barmanObjectName": project.Name + "-backup",
				},
			},
		}
	}

	return cluster
}

// buildRoles creates the managed roles for Supabase
func buildRoles(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) []cnpgv1.RoleConfiguration {
	return []cnpgv1.RoleConfiguration{
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
	}
}

// mergeParameters merges default parameters with user-provided parameters
func mergeParameters(defaults, custom map[string]string) map[string]string {
	result := make(map[string]string)
	for k, v := range defaults {
		result[k] = v
	}
	for k, v := range custom {
		result[k] = v
	}
	return result
}


// ptrStringOrNil returns a pointer to the string if non-empty, nil otherwise
func ptrStringOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
