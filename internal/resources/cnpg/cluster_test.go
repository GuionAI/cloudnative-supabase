package cnpg

import (
	"reflect"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

func TestBuildClusterProjectsPGroongaPreloads(t *testing.T) {
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "search", Namespace: "test"},
		Spec: supabasev1alpha1.SupabaseProjectSpec{Database: supabasev1alpha1.DatabaseSpec{
			Image:                      "example.test/postgres-pgroonga:18",
			Storage:                    supabaseStorage("1Gi"),
			AdditionalPreloadLibraries: []string{"pgroonga_wal_resource_manager", "pgroonga_crash_safer"},
			Parameters: map[string]string{
				"pgroonga.enable_wal_resource_manager": "on",
				"pgroonga.enable_crash_safe":           "on",
			},
		}},
	}
	cluster := BuildCluster(project, &supabasev1alpha1.SecretNamesStatus{})
	if cluster.Spec.ImageName != project.Spec.Database.Image {
		t.Fatalf("image = %q", cluster.Spec.ImageName)
	}
	want := []string{"pg_stat_statements", "pgaudit", "auto_explain", "pgroonga_wal_resource_manager", "pgroonga_crash_safer"}
	if !reflect.DeepEqual(cluster.Spec.PostgresConfiguration.AdditionalLibraries, want) {
		t.Fatalf("preload libraries = %v, want %v", cluster.Spec.PostgresConfiguration.AdditionalLibraries, want)
	}
	for name, value := range project.Spec.Database.Parameters {
		if cluster.Spec.PostgresConfiguration.Parameters[name] != value {
			t.Fatalf("parameter %s = %q, want %q", name, cluster.Spec.PostgresConfiguration.Parameters[name], value)
		}
	}
}

func TestBuildClusterRecoveryAndBackupUseSeparateStores(t *testing.T) {
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "restored", Namespace: "supabase"},
		Spec: supabasev1alpha1.SupabaseProjectSpec{
			Database: supabasev1alpha1.DatabaseSpec{
				Instances: 1,
				Storage:   supabaseStorage("20Gi"),
				Recovery: &supabasev1alpha1.RecoverySpec{
					Enabled:    true,
					ServerName: "source-cluster",
					S3Config: supabasev1alpha1.S3Config{
						DestinationPath:     "s3://recovery/source",
						S3CredentialsSecret: "recovery-s3",
					},
					TargetTime: "2026-01-02T03:04:05Z",
				},
				Backup: &supabasev1alpha1.BackupSpec{
					Enabled: true,
					S3Config: supabasev1alpha1.S3Config{
						DestinationPath:     "s3://backups/restored",
						S3CredentialsSecret: "backup-s3",
					},
				},
			},
		},
	}
	secretNames := &supabasev1alpha1.SecretNamesStatus{SupabaseAdmin: "restored-admin"}
	cluster := BuildCluster(project, secretNames)

	if len(cluster.Spec.Plugins) != 1 {
		t.Fatalf("plugins = %#v, want one WAL archiver", cluster.Spec.Plugins)
	}
	plugin := cluster.Spec.Plugins[0]
	if plugin.Name != BarmanCloudPluginName || plugin.IsWALArchiver == nil || !*plugin.IsWALArchiver {
		t.Fatalf("backup plugin = %#v", plugin)
	}
	if plugin.Parameters["barmanObjectName"] != ObjectStoreName(project) {
		t.Fatalf("backup plugin object store = %q, want %q", plugin.Parameters["barmanObjectName"], ObjectStoreName(project))
	}
	if cluster.Spec.Bootstrap == nil || cluster.Spec.Bootstrap.Recovery == nil || cluster.Spec.Bootstrap.InitDB != nil {
		t.Fatalf("bootstrap = %#v, want recovery only", cluster.Spec.Bootstrap)
	}
	recovery := cluster.Spec.Bootstrap.Recovery
	if recovery.Source != RecoverySourceName(project) || recovery.RecoveryTarget == nil || recovery.RecoveryTarget.TargetTime != "2026-01-02T03:04:05Z" {
		t.Fatalf("recovery bootstrap = %#v", recovery)
	}
	if len(cluster.Spec.ExternalClusters) != 1 {
		t.Fatalf("external clusters = %#v", cluster.Spec.ExternalClusters)
	}
	external := cluster.Spec.ExternalClusters[0]
	if external.Name != RecoverySourceName(project) || external.PluginConfiguration == nil ||
		external.PluginConfiguration.Parameters["barmanObjectName"] != RecoveryObjectStoreName(project) {
		t.Fatalf("recovery external cluster = %#v", external)
	}
	if ObjectStoreName(project) == RecoveryObjectStoreName(project) ||
		project.Spec.Database.Backup.S3CredentialsSecret == project.Spec.Database.Recovery.S3CredentialsSecret {
		t.Fatal("backup and recovery stores must remain distinct")
	}
}

func supabaseStorage(size string) cnpgv1.StorageConfiguration {
	return cnpgv1.StorageConfiguration{Size: size}
}
