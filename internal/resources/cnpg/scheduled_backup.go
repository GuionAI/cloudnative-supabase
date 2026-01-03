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
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
)

const (
	// BarmanCloudPluginName is the name of the barman-cloud plugin
	BarmanCloudPluginName = "barman-cloud.cloudnative-pg.io"
)

// ScheduledBackupName returns the ScheduledBackup name for a project
func ScheduledBackupName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-backup"
}

// BuildScheduledBackup creates a CNPG ScheduledBackup resource
func BuildScheduledBackup(project *supabasev1alpha1.SupabaseProject) *cnpgv1.ScheduledBackup {
	backup := project.Spec.Database.Backup
	if backup == nil || !backup.Enabled {
		return nil
	}

	name := ScheduledBackupName(project)
	clusterName := ClusterName(project)

	// Default schedule if not specified
	schedule := backup.Schedule
	if schedule == "" {
		schedule = "0 0 2 * * *" // 2 AM UTC daily
	}

	scheduledBackup := &cnpgv1.ScheduledBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, "backup"),
		},
		Spec: cnpgv1.ScheduledBackupSpec{
			Schedule: schedule,
			Cluster: cnpgv1.LocalObjectReference{
				Name: clusterName,
			},
			// Set owner reference to self so backup is deleted when schedule is deleted
			BackupOwnerReference: "self",
			// Use the barman-cloud plugin for backups
			Method: cnpgv1.BackupMethodPlugin,
			PluginConfiguration: &cnpgv1.BackupPluginConfiguration{
				Name: BarmanCloudPluginName,
			},
		},
	}

	return scheduledBackup
}
