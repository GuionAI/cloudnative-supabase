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

package jobs

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/cnpg"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/defaults"
)

const (
	CDCComponentName = "cdc-permissions"
)

// CDCConfigMapName returns the name of the CDC migrations ConfigMap
func CDCConfigMapName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-cdc-migrations"
}

// CDCJobName returns the name of the CDC permissions Job
func CDCJobName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-cdc-permissions"
}

// BuildCDCMigrationsConfigMap creates the ConfigMap containing CDC setup scripts
func BuildCDCMigrationsConfigMap(project *supabasev1alpha1.SupabaseProject) *corev1.ConfigMap {
	setupScript := buildCDCSetupScript(project)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      CDCConfigMapName(project),
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, CDCComponentName),
		},
		Data: map[string]string{
			"setup.sh": setupScript,
		},
	}
}

// buildCDCSetupScript generates the CDC setup shell script based on enabled services
func buildCDCSetupScript(project *supabasev1alpha1.SupabaseProject) string {
	script := `#!/bin/sh
set -e

echo "=== CDC Permissions Setup ==="
`

	// Sequin-specific grants
	if project.Spec.Sequin != nil {
		script += `
# Create sequin database if it doesn't exist
echo "Checking if sequin database exists..."
DB_EXISTS=$(psql "$PGCONNSTR" -tAc "SELECT 1 FROM pg_database WHERE datname='sequin'" 2>/dev/null || echo "0")
if [ "$DB_EXISTS" != "1" ]; then
  echo "Creating sequin database..."
  psql "$PGCONNSTR" -c "CREATE DATABASE sequin OWNER sequin"
  echo "Sequin database created"
else
  echo "Sequin database already exists"
fi

# Apply Sequin CDC grants
echo "Applying Sequin CDC grants..."
psql "$PGCONNSTR" <<'EOSQL'
-- Grant CDC role (sequin_replication) read access to public schema
GRANT USAGE ON SCHEMA public TO sequin_replication;

-- Grant sequin CREATE ON DATABASE for its migrations
GRANT CREATE ON DATABASE supabase TO sequin;

-- Grant SELECT on future tables created by supabase_admin in public schema
ALTER DEFAULT PRIVILEGES FOR ROLE supabase_admin IN SCHEMA public GRANT SELECT ON TABLES TO sequin_replication;

-- Grant SELECT on existing tables in public schema
GRANT SELECT ON ALL TABLES IN SCHEMA public TO sequin_replication;
EOSQL
`
	}

	// Powersync-specific grants
	if project.Spec.Powersync != nil {
		script += `
# Apply Powersync grants
echo "Applying Powersync grants..."
psql "$PGCONNSTR" <<'EOSQL'
-- Grant powersync_storage role access to create its schema
GRANT CREATE ON DATABASE supabase TO powersync_storage;

-- Grant usage on public schema for replication reads
GRANT USAGE ON SCHEMA public TO powersync_storage;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO powersync_storage;
ALTER DEFAULT PRIVILEGES FOR ROLE supabase_admin IN SCHEMA public GRANT SELECT ON TABLES TO powersync_storage;
EOSQL
`
	}

	script += `
echo "=== CDC Permissions Setup Complete ==="
`
	return script
}

// BuildCDCPermissionsJob creates the Job that applies CDC permissions after database is ready
func BuildCDCPermissionsJob(project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) *batchv1.Job {
	name := CDCJobName(project)
	dbHost := cnpg.ClusterRWServiceName(project)
	// Use the same postgres image as the CNPG cluster for psql compatibility
	pgImage := fmt.Sprintf("%s:%s", defaults.PostgresImage, defaults.PostgresTag)

	var backoffLimit int32 = 3

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, CDCComponentName),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: common.ComponentLabels(project, CDCComponentName),
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					InitContainers: []corev1.Container{
						{
							Name:    "wait-for-db",
							Image:   pgImage,
							Command: []string{"sh", "-c"},
							Args: []string{
								fmt.Sprintf(
									`echo "Waiting for database to be ready..."
until pg_isready -h %s -p 5432 -U "$PGUSER"; do
  echo "Database not ready yet, retrying in 5s..."
  sleep 5
done
echo "Database is ready"`, dbHost),
							},
							Env: buildCDCEnv(dbHost, secretNames),
						},
					},
					Containers: []corev1.Container{
						{
							Name:    "cdc-setup",
							Image:   pgImage,
							Command: []string{"sh", "/scripts/setup.sh"},
							Env:     buildCDCEnv(dbHost, secretNames),
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "scripts",
									MountPath: "/scripts",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "scripts",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: CDCConfigMapName(project),
									},
									DefaultMode: int32Ptr(0755),
								},
							},
						},
					},
				},
			},
		},
	}
}

// buildCDCEnv builds env vars for the CDC Job using supabase_admin credentials
func buildCDCEnv(dbHost string, secretNames *supabasev1alpha1.SecretNamesStatus) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name: "PGUSER",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.SupabaseAdmin,
					},
					Key: "username",
				},
			},
		},
		{
			Name: "PGPASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretNames.SupabaseAdmin,
					},
					Key: "password",
				},
			},
		},
		{
			Name:  "PGCONNSTR",
			Value: fmt.Sprintf("postgres://$(PGUSER):$(PGPASSWORD)@%s:5432/supabase?sslmode=disable", dbHost),
		},
	}
}

func int32Ptr(i int32) *int32 {
	return &i
}
