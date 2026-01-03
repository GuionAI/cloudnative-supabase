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
	barmanapi "github.com/cloudnative-pg/barman-cloud/pkg/api"
	machineryapi "github.com/cloudnative-pg/machinery/pkg/api"
	barmancloudv1 "github.com/cloudnative-pg/plugin-barman-cloud/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
)

const (
	// DefaultAccessKeyIDKey is the default key for access key ID in secrets
	DefaultAccessKeyIDKey = "ACCESS_KEY_ID"
	// DefaultSecretAccessKeyKey is the default key for secret access key in secrets
	DefaultSecretAccessKeyKey = "SECRET_ACCESS_KEY"
)

// ObjectStoreName returns the ObjectStore name for backup
func ObjectStoreName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-backup"
}

// RecoveryObjectStoreName returns the ObjectStore name for recovery
func RecoveryObjectStoreName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-recovery"
}

// BuildObjectStore creates a barman-cloud ObjectStore resource for backups
func BuildObjectStore(project *supabasev1alpha1.SupabaseProject) *barmancloudv1.ObjectStore {
	backup := project.Spec.Database.Backup
	if backup == nil || !backup.Enabled {
		return nil
	}

	name := ObjectStoreName(project)

	// Determine compression types
	walCompression := barmanapi.CompressionTypeGzip
	if backup.WalCompression != "" {
		walCompression = barmanapi.CompressionType(backup.WalCompression)
	}

	dataCompression := barmanapi.CompressionTypeGzip
	if backup.DataCompression != "" {
		dataCompression = barmanapi.CompressionType(backup.DataCompression)
	}

	objectStore := &barmancloudv1.ObjectStore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, "backup"),
		},
		Spec: barmancloudv1.ObjectStoreSpec{
			Configuration: barmanapi.BarmanObjectStoreConfiguration{
				DestinationPath: backup.DestinationPath,
				EndpointURL:     backup.EndpointURL,
				BarmanCredentials: barmanapi.BarmanCredentials{
					AWS: &barmanapi.S3Credentials{
						AccessKeyIDReference: &machineryapi.SecretKeySelector{
							LocalObjectReference: machineryapi.LocalObjectReference{
								Name: backup.S3CredentialsSecret,
							},
							Key: DefaultAccessKeyIDKey,
						},
						SecretAccessKeyReference: &machineryapi.SecretKeySelector{
							LocalObjectReference: machineryapi.LocalObjectReference{
								Name: backup.S3CredentialsSecret,
							},
							Key: DefaultSecretAccessKeyKey,
						},
					},
				},
				Wal: &barmanapi.WalBackupConfiguration{
					Compression: walCompression,
					MaxParallel: 2,
				},
				Data: &barmanapi.DataBackupConfiguration{
					Compression: dataCompression,
				},
			},
			RetentionPolicy: backup.RetentionPolicy,
		},
	}

	return objectStore
}

// BuildRecoveryObjectStore creates a barman-cloud ObjectStore resource for recovery
func BuildRecoveryObjectStore(project *supabasev1alpha1.SupabaseProject) *barmancloudv1.ObjectStore {
	recovery := project.Spec.Database.Recovery
	if recovery == nil || !recovery.Enabled {
		return nil
	}

	name := RecoveryObjectStoreName(project)

	objectStore := &barmancloudv1.ObjectStore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, "recovery"),
		},
		Spec: barmancloudv1.ObjectStoreSpec{
			Configuration: barmanapi.BarmanObjectStoreConfiguration{
				DestinationPath: recovery.DestinationPath,
				EndpointURL:     recovery.EndpointURL,
				BarmanCredentials: barmanapi.BarmanCredentials{
					AWS: &barmanapi.S3Credentials{
						AccessKeyIDReference: &machineryapi.SecretKeySelector{
							LocalObjectReference: machineryapi.LocalObjectReference{
								Name: recovery.S3CredentialsSecret,
							},
							Key: DefaultAccessKeyIDKey,
						},
						SecretAccessKeyReference: &machineryapi.SecretKeySelector{
							LocalObjectReference: machineryapi.LocalObjectReference{
								Name: recovery.S3CredentialsSecret,
							},
							Key: DefaultSecretAccessKeyKey,
						},
					},
				},
			},
			// No retention policy for recovery - this is read-only
		},
	}

	return objectStore
}
