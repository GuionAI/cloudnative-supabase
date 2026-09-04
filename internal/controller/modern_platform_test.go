package controller

import (
	"context"
	"reflect"
	"strings"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	barmancloudv1 "github.com/cloudnative-pg/plugin-barman-cloud/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	cnpgresources "github.com/GuionAI/cloudnative-supabase/internal/resources/cnpg"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
	deploymentresources "github.com/GuionAI/cloudnative-supabase/internal/resources/deployments"
	secretresources "github.com/GuionAI/cloudnative-supabase/internal/resources/secrets"
)

func TestReconcileClusterMutableFieldsPreservesForeignConfiguration(t *testing.T) {
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "mutable", Namespace: "default"},
		Spec: supabasev1alpha1.SupabaseProjectSpec{Database: supabasev1alpha1.DatabaseSpec{
			Instances:             2,
			Image:                 "postgres:17-custom",
			EnableSuperuserAccess: true,
			Storage:               cnpgv1.StorageConfiguration{Size: "20Gi"},
			Resources:             corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resourceQuantity("20m")}},
			Parameters:            map[string]string{"operator.custom": "new"},
			Backup: &supabasev1alpha1.BackupSpec{
				Enabled: true,
				S3Config: supabasev1alpha1.S3Config{
					DestinationPath:     "s3://backup/mutable",
					S3CredentialsSecret: "backup-s3",
				},
			},
		}},
	}
	secretNames := &supabasev1alpha1.SecretNamesStatus{SupabaseAdmin: "mutable-admin"}
	desired := cnpgresources.BuildCluster(project, secretNames)
	existing := desired.DeepCopy()
	existing.Spec.Instances = 1
	existing.Spec.ImageName = "postgres:16-old"
	existing.Spec.Resources = corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resourceQuantity("10m")}}
	existing.Spec.EnableSuperuserAccess = ptr.To(false)
	existing.Spec.PostgresConfiguration.Parameters = map[string]string{
		"foreign.parameter": "keep",
		"operator.custom":   "old",
	}
	existing.Spec.PostgresConfiguration.PgHBA = append(existing.Spec.PostgresConfiguration.PgHBA, "host all foreign 10.1.0.0/16 trust")
	existing.Spec.PostgresConfiguration.AdditionalLibraries = append(existing.Spec.PostgresConfiguration.AdditionalLibraries, "foreign_lib")
	existing.Spec.Affinity = cnpgv1.AffinityConfiguration{
		EnablePodAntiAffinity: ptr.To(false),
		TopologyKey:           "topology.kubernetes.io/zone",
	}
	existing.Spec.Managed = &cnpgv1.ManagedConfiguration{Roles: []cnpgv1.RoleConfiguration{{Name: "foreign-role"}}}
	existing.Spec.Plugins = append([]cnpgv1.PluginConfiguration{{Name: "foreign.plugin", Parameters: map[string]string{"keep": "yes"}}}, existing.Spec.Plugins...)
	existing.Spec.StorageConfiguration.Size = "10Gi"

	changed, err := reconcileClusterMutableFields(existing, desired)
	if err != nil {
		t.Fatalf("reconcileClusterMutableFields() error = %v", err)
	}
	if !changed {
		t.Fatal("expected mutable fields to change")
	}
	if existing.Spec.Instances != 2 || existing.Spec.ImageName != "postgres:17-custom" ||
		existing.Spec.EnableSuperuserAccess == nil || !*existing.Spec.EnableSuperuserAccess {
		t.Fatalf("core mutable fields did not converge: %#v", existing.Spec)
	}
	if existing.Spec.Resources.Requests.Cpu().String() != "20m" {
		t.Fatalf("resources did not converge: %#v", existing.Spec.Resources)
	}
	if existing.Spec.PostgresConfiguration.Parameters["foreign.parameter"] != "keep" ||
		existing.Spec.PostgresConfiguration.Parameters["operator.custom"] != "new" {
		t.Fatalf("parameters were not merged safely: %#v", existing.Spec.PostgresConfiguration.Parameters)
	}
	if existing.Spec.Affinity.TopologyKey != "topology.kubernetes.io/zone" ||
		existing.Spec.Affinity.EnablePodAntiAffinity == nil || !*existing.Spec.Affinity.EnablePodAntiAffinity {
		t.Fatalf("foreign affinity was overwritten: %#v", existing.Spec.Affinity)
	}
	if !containsString(existing.Spec.PostgresConfiguration.PgHBA, "host all foreign 10.1.0.0/16 trust") ||
		!containsString(existing.Spec.PostgresConfiguration.AdditionalLibraries, "foreign_lib") {
		t.Fatal("foreign PostgreSQL settings were not preserved")
	}
	if len(existing.Spec.Plugins) != 2 || existing.Spec.Plugins[0].Name != "foreign.plugin" || existing.Spec.Plugins[1].Name != cnpgresources.BarmanCloudPluginName {
		t.Fatalf("plugins were not merged safely: %#v", existing.Spec.Plugins)
	}
	if existing.Spec.StorageConfiguration.Size != "20Gi" {
		t.Fatalf("storage size = %q, want expansion to 20Gi", existing.Spec.StorageConfiguration.Size)
	}
}

func TestReconcileClusterMutableFieldsClearsOperatorOwnedResources(t *testing.T) {
	existing := &cnpgv1.Cluster{Spec: cnpgv1.ClusterSpec{
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resourceQuantity("100m")},
			Limits:   corev1.ResourceList{corev1.ResourceMemory: resourceQuantity("256Mi")},
		},
	}}
	desired := existing.DeepCopy()
	desired.Spec.Resources = corev1.ResourceRequirements{}

	changed, err := reconcileClusterMutableFields(existing, desired)
	if err != nil {
		t.Fatalf("reconcileClusterMutableFields() error = %v", err)
	}
	if !changed {
		t.Fatal("expected clearing operator-owned resources to change the cluster")
	}
	if !apiequality.Semantic.DeepEqual(existing.Spec.Resources, corev1.ResourceRequirements{}) {
		t.Fatalf("resources = %#v, want cleared", existing.Spec.Resources)
	}
}

func TestReconcileClusterMutableFieldsRejectsStorageShrinkWithoutMutation(t *testing.T) {
	existing := &cnpgv1.Cluster{Spec: cnpgv1.ClusterSpec{
		Instances:            1,
		StorageConfiguration: cnpgv1.StorageConfiguration{Size: "20Gi"},
	}}
	desired := existing.DeepCopy()
	desired.Spec.Instances = 2
	desired.Spec.StorageConfiguration.Size = "10Gi"
	before := existing.DeepCopy()

	changed, err := reconcileClusterMutableFields(existing, desired)
	if err == nil || changed {
		t.Fatalf("storage shrink result = changed:%v err:%v, want error and no change", changed, err)
	}
	if !reflect.DeepEqual(existing, before) {
		t.Fatalf("invalid shrink mutated cluster: before=%#v after=%#v", before.Spec, existing.Spec)
	}
}

func TestReconcileCNPGClusterRemovesOnlyProjectOwnerAndPreservesLabels(t *testing.T) {
	scheme := newPowerSyncTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{ObjectMeta: metav1.ObjectMeta{
		Name: "retained", Namespace: "default", UID: "project-uid",
	}, Status: supabasev1alpha1.SupabaseProjectStatus{SecretNames: supabasev1alpha1.SecretNamesStatus{
		SupabaseAdmin: "retained-admin", Authenticator: "retained-authenticator", AuthAdmin: "retained-auth-admin",
	}}}
	desired := cnpgresources.BuildCluster(project, &project.Status.SecretNames)
	controller := true
	existing := desired.DeepCopy()
	existing.OwnerReferences = []metav1.OwnerReference{
		{APIVersion: supabasev1alpha1.GroupVersion.Group + "/v1beta1", Kind: "SupabaseProject", Name: project.Name, UID: "stale-project-uid", Controller: &controller},
		{APIVersion: "example.dev/v1", Kind: "SupabaseProject", Name: project.Name, UID: "foreign-uid"},
	}
	existing.Labels["foreign.example/keep"] = "yes"
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(project, existing).Build(),
		Scheme: scheme,
	}
	if _, err := reconciler.reconcileCNPGCluster(context.Background(), project); err != nil {
		t.Fatalf("reconcileCNPGCluster() error = %v", err)
	}
	updated := &cnpgv1.Cluster{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(existing), updated); err != nil {
		t.Fatal(err)
	}
	if len(updated.OwnerReferences) != 1 || updated.OwnerReferences[0].APIVersion != "example.dev/v1" || updated.OwnerReferences[0].Kind != "SupabaseProject" || updated.OwnerReferences[0].Name != project.Name {
		t.Fatalf("owner references = %#v, want only foreign owner", updated.OwnerReferences)
	}
	if updated.Labels["foreign.example/keep"] != "yes" || updated.Labels[common.LabelInstance] != project.Name {
		t.Fatalf("labels were not preserved/repaired: %#v", updated.Labels)
	}
}

func TestDurableAdoptionRequiresMatchingInstanceLabel(t *testing.T) {
	t.Parallel()

	scheme := newIdempotencyTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "adopt", Namespace: "default", UID: "project-uid"},
		Status: supabasev1alpha1.SupabaseProjectStatus{SecretNames: supabasev1alpha1.SecretNamesStatus{
			SupabaseAdmin: "adopt-admin", Authenticator: "adopt-authenticator", AuthAdmin: "adopt-auth-admin",
		}},
		Spec: supabasev1alpha1.SupabaseProjectSpec{Database: supabasev1alpha1.DatabaseSpec{
			Backup: &supabasev1alpha1.BackupSpec{Enabled: true, S3Config: supabasev1alpha1.S3Config{DestinationPath: "s3://backup", S3CredentialsSecret: "backup-s3"}},
		}},
	}
	cluster := cnpgresources.BuildCluster(project, &project.Status.SecretNames)
	objectStore := cnpgresources.BuildObjectStore(project)
	scheduledBackup := cnpgresources.BuildScheduledBackup(project)
	foreignLabels := map[string]string{common.LabelInstance: "another-project"}
	cluster.Labels = nil
	objectStore.Labels = foreignLabels
	scheduledBackup.Labels = nil
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project).WithObjects(project, cluster, objectStore, scheduledBackup).Build(),
		Scheme: scheme,
	}
	if _, err := reconciler.reconcileCNPGCluster(context.Background(), project); err == nil {
		t.Fatal("unlabelled CNPG cluster was adopted")
	}
	if err := reconciler.createOrUpdateObjectStore(context.Background(), cnpgresources.BuildObjectStore(project)); err == nil {
		t.Fatal("foreign-labelled ObjectStore was adopted")
	}
	if err := reconciler.createOrUpdateScheduledBackup(context.Background(), cnpgresources.BuildScheduledBackup(project)); err == nil {
		t.Fatal("unlabelled ScheduledBackup was adopted")
	}
	for _, object := range []client.Object{cluster, objectStore, scheduledBackup} {
		if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(object), object); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCleanupDoesNotDeleteUnlabelledOrForeignDurableResources(t *testing.T) {
	t.Parallel()

	scheme := newIdempotencyTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "cleanup", Namespace: "default"},
		Status:     supabasev1alpha1.SupabaseProjectStatus{Conditions: []metav1.Condition{{Type: supabasev1alpha1.ConditionTypeBackupReady, Status: metav1.ConditionTrue}, {Type: supabasev1alpha1.ConditionTypeRecoveryReady, Status: metav1.ConditionTrue}}},
	}
	scheduledBackup := &cnpgv1.ScheduledBackup{ObjectMeta: metav1.ObjectMeta{Name: cnpgresources.ScheduledBackupName(project), Namespace: project.Namespace}}
	backupStore := &barmancloudv1.ObjectStore{ObjectMeta: metav1.ObjectMeta{Name: cnpgresources.ObjectStoreName(project), Namespace: project.Namespace, Labels: map[string]string{common.LabelInstance: "another-project"}}}
	recoveryStore := &barmancloudv1.ObjectStore{ObjectMeta: metav1.ObjectMeta{Name: cnpgresources.RecoveryObjectStoreName(project), Namespace: project.Namespace}}
	reconciler := &SupabaseProjectReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project).WithObjects(project, scheduledBackup, backupStore, recoveryStore).Build(), Scheme: scheme}
	if err := reconciler.cleanupBackupResources(context.Background(), project); err == nil {
		t.Fatal("cleanup unexpectedly accepted an unlabelled ScheduledBackup")
	}
	if err := reconciler.cleanupRecoveryResources(context.Background(), project); err == nil {
		t.Fatal("cleanup unexpectedly accepted an unlabelled recovery ObjectStore")
	}
	for _, object := range []client.Object{scheduledBackup, backupStore, recoveryStore} {
		if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(object), object); err != nil {
			t.Fatalf("durable resource %s was deleted: %v", object.GetName(), err)
		}
	}
}

func TestRecoveryBootstrapIntentIncludesExternalSourceParameters(t *testing.T) {
	t.Parallel()

	scheme := newPowerSyncTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "recovery", Namespace: "default"},
		Spec: supabasev1alpha1.SupabaseProjectSpec{
			Database: supabasev1alpha1.DatabaseSpec{
				Instances: 1,
				Storage:   cnpgv1.StorageConfiguration{Size: "1Gi"},
				Recovery:  &supabasev1alpha1.RecoverySpec{Enabled: true, ServerName: "source-v1", S3Config: supabasev1alpha1.S3Config{DestinationPath: "s3://source", S3CredentialsSecret: "source-s3"}},
			},
		},
		Status: supabasev1alpha1.SupabaseProjectStatus{SecretNames: supabasev1alpha1.SecretNamesStatus{SupabaseAdmin: "recovery-admin"}},
	}
	desired := cnpgresources.BuildCluster(project, &project.Status.SecretNames)
	existing := desired.DeepCopy()
	existing.Spec.ExternalClusters[0].PluginConfiguration.Parameters["serverName"] = "source-v0"
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project).WithObjects(project, existing).Build(),
		Scheme: scheme,
	}
	if _, err := reconciler.reconcileCNPGCluster(context.Background(), project); err == nil {
		t.Fatal("external recovery source parameter change was accepted")
	}
	updated := &cnpgv1.Cluster{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(existing), updated); err != nil {
		t.Fatal(err)
	}
	if updated.Spec.ExternalClusters[0].PluginConfiguration.Parameters["serverName"] != "source-v0" {
		t.Fatal("immutable source parameter was silently mutated")
	}
	condition := meta.FindStatusCondition(project.Status.Conditions, supabasev1alpha1.ConditionTypeDatabaseReady)
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != "BootstrapImmutable" {
		t.Fatalf("database condition = %#v, want BootstrapImmutable false", condition)
	}
}

func TestRecoveryBootstrapIgnoresUnrecognizedExternalPluginParameters(t *testing.T) {
	t.Parallel()

	scheme := newPowerSyncTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "recovery-plugin-parameter", Namespace: "default"},
		Spec: supabasev1alpha1.SupabaseProjectSpec{Database: supabasev1alpha1.DatabaseSpec{
			Instances: 1,
			Storage:   cnpgv1.StorageConfiguration{Size: "1Gi"},
			Recovery: &supabasev1alpha1.RecoverySpec{Enabled: true, ServerName: "source", S3Config: supabasev1alpha1.S3Config{
				DestinationPath:     "s3://source",
				S3CredentialsSecret: "source-s3",
			}},
		}},
		Status: supabasev1alpha1.SupabaseProjectStatus{SecretNames: supabasev1alpha1.SecretNamesStatus{SupabaseAdmin: "recovery-plugin-parameter-admin"}},
	}
	desired := cnpgresources.BuildCluster(project, &project.Status.SecretNames)
	existing := desired.DeepCopy()
	existing.Spec.ExternalClusters[0].PluginConfiguration.Parameters["future-plugin-option"] = "preserved"
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(project, existing).Build(),
		Scheme: scheme,
	}
	if _, err := reconciler.reconcileCNPGCluster(context.Background(), project); err != nil {
		t.Fatalf("unrecognized external plugin parameter blocked reconciliation: %v", err)
	}
	updated := &cnpgv1.Cluster{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(existing), updated); err != nil {
		t.Fatal(err)
	}
	if got := updated.Spec.ExternalClusters[0].PluginConfiguration.Parameters["future-plugin-option"]; got != "preserved" {
		t.Fatalf("unrecognized external plugin parameter = %q, want preserved", got)
	}
}

func TestRecoveryBootstrapIgnoresUnrelatedExternalClustersInInitDBMode(t *testing.T) {
	t.Parallel()

	scheme := newPowerSyncTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "initdb-foreign-source", Namespace: "default"},
		Spec: supabasev1alpha1.SupabaseProjectSpec{Database: supabasev1alpha1.DatabaseSpec{
			Instances: 1,
			Storage:   cnpgv1.StorageConfiguration{Size: "1Gi"},
		}},
		Status: supabasev1alpha1.SupabaseProjectStatus{SecretNames: supabasev1alpha1.SecretNamesStatus{SupabaseAdmin: "initdb-foreign-source-admin"}},
	}
	desired := cnpgresources.BuildCluster(project, &project.Status.SecretNames)
	existing := desired.DeepCopy()
	existing.Spec.ExternalClusters = []cnpgv1.ExternalCluster{{
		Name: "unrelated-recovery-source",
		PluginConfiguration: &cnpgv1.PluginConfiguration{
			Name:       cnpgresources.BarmanCloudPluginName,
			Parameters: map[string]string{"serverName": "foreign-source"},
		},
	}}
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(project, existing).Build(),
		Scheme: scheme,
	}
	if _, err := reconciler.reconcileCNPGCluster(context.Background(), project); err != nil {
		t.Fatalf("unrelated external cluster blocked initdb reconciliation: %v", err)
	}
	updated := &cnpgv1.Cluster{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(existing), updated); err != nil {
		t.Fatal(err)
	}
	if len(updated.Spec.ExternalClusters) != 1 || updated.Spec.ExternalClusters[0].Name != "unrelated-recovery-source" || updated.Spec.ExternalClusters[0].PluginConfiguration.Parameters["serverName"] != "foreign-source" {
		t.Fatalf("unrelated external cluster was not preserved: %#v", updated.Spec.ExternalClusters)
	}
}

func TestRecoveryObjectStoreIdentityIsNotRewrittenForExistingCluster(t *testing.T) {
	t.Parallel()

	scheme := newIdempotencyTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "source-store", Namespace: "default"},
		Spec: supabasev1alpha1.SupabaseProjectSpec{Database: supabasev1alpha1.DatabaseSpec{
			Instances: 1,
			Storage:   cnpgv1.StorageConfiguration{Size: "1Gi"},
			Recovery:  &supabasev1alpha1.RecoverySpec{Enabled: true, ServerName: "source", S3Config: supabasev1alpha1.S3Config{DestinationPath: "s3://new", EndpointURL: "https://s3.new", S3CredentialsSecret: "new-s3"}},
		}},
		Status: supabasev1alpha1.SupabaseProjectStatus{SecretNames: supabasev1alpha1.SecretNamesStatus{SupabaseAdmin: "source-store-admin"}},
	}
	cluster := cnpgresources.BuildCluster(project, &project.Status.SecretNames)
	oldProject := project.DeepCopy()
	oldProject.Spec.Database.Recovery.DestinationPath = "s3://old"
	oldProject.Spec.Database.Recovery.EndpointURL = "https://s3.old"
	oldProject.Spec.Database.Recovery.S3CredentialsSecret = "old-s3"
	store := cnpgresources.BuildRecoveryObjectStore(oldProject)
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(project, cluster, store).Build(),
		Scheme: scheme,
	}
	if err := reconciler.validateRecoveryBootstrapIntent(context.Background(), project); err == nil {
		t.Fatal("recovery ObjectStore identity change was accepted")
	}
	updated := &barmancloudv1.ObjectStore{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(store), updated); err != nil {
		t.Fatal(err)
	}
	if updated.Spec.Configuration.DestinationPath != "s3://old" || updated.Spec.Configuration.EndpointURL != "https://s3.old" {
		t.Fatalf("recovery ObjectStore was rewritten: %#v", updated.Spec.Configuration)
	}
}

func TestRecoveryObjectStoreCredentialReferenceRotatesAfterBootstrap(t *testing.T) {
	t.Parallel()

	scheme := newIdempotencyTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "rotating-recovery-credentials", Namespace: "default"},
		Spec: supabasev1alpha1.SupabaseProjectSpec{Database: supabasev1alpha1.DatabaseSpec{
			Instances: 1,
			Storage:   cnpgv1.StorageConfiguration{Size: "1Gi"},
			Recovery: &supabasev1alpha1.RecoverySpec{Enabled: true, ServerName: "source", S3Config: supabasev1alpha1.S3Config{
				DestinationPath:     "s3://source",
				EndpointURL:         "https://s3.example",
				S3CredentialsSecret: "new-s3",
			}},
		}},
		Status: supabasev1alpha1.SupabaseProjectStatus{SecretNames: supabasev1alpha1.SecretNamesStatus{SupabaseAdmin: "rotating-recovery-admin"}},
	}
	cluster := cnpgresources.BuildCluster(project, &project.Status.SecretNames)
	oldProject := project.DeepCopy()
	oldProject.Spec.Database.Recovery.S3CredentialsSecret = "old-s3"
	store := cnpgresources.BuildRecoveryObjectStore(oldProject)
	credentials := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "new-s3", Namespace: project.Namespace}, Data: map[string][]byte{
		cnpgresources.DefaultAccessKeyIDKey:     []byte("new-access"),
		cnpgresources.DefaultSecretAccessKeyKey: []byte("new-secret"),
	}}
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project).WithObjects(project, cluster, store, credentials).Build(),
		Scheme: scheme,
	}
	if err := reconciler.reconcileRecovery(context.Background(), project); err != nil {
		t.Fatalf("reconcileRecovery() rejected a rotatable credential reference: %v", err)
	}
	updated := &barmancloudv1.ObjectStore{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(store), updated); err != nil {
		t.Fatal(err)
	}
	if updated.Spec.Configuration.AWS == nil ||
		updated.Spec.Configuration.AWS.AccessKeyIDReference == nil ||
		updated.Spec.Configuration.AWS.AccessKeyIDReference.Name != "new-s3" ||
		updated.Spec.Configuration.AWS.SecretAccessKeyReference == nil ||
		updated.Spec.Configuration.AWS.SecretAccessKeyReference.Name != "new-s3" {
		t.Fatalf("recovery ObjectStore credential references = %#v, want new-s3", updated.Spec.Configuration.BarmanCredentials)
	}
}

func TestRecoveryDisableRejectsBootstrapChangeBeforeSourceCleanup(t *testing.T) {
	t.Parallel()

	scheme := newIdempotencyTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "disable-recovery", Namespace: "default"},
		Spec: supabasev1alpha1.SupabaseProjectSpec{Database: supabasev1alpha1.DatabaseSpec{
			Instances: 1,
			Storage:   cnpgv1.StorageConfiguration{Size: "1Gi"},
		}},
		Status: supabasev1alpha1.SupabaseProjectStatus{
			SecretNames: supabasev1alpha1.SecretNamesStatus{SupabaseAdmin: "disable-recovery-admin"},
			Conditions:  []metav1.Condition{{Type: supabasev1alpha1.ConditionTypeRecoveryReady, Status: metav1.ConditionTrue}},
		},
	}
	recoveredProject := project.DeepCopy()
	recoveredProject.Spec.Database.Recovery = &supabasev1alpha1.RecoverySpec{
		Enabled:    true,
		ServerName: "source",
		S3Config: supabasev1alpha1.S3Config{
			DestinationPath:     "s3://source",
			S3CredentialsSecret: "source-s3",
		},
	}
	cluster := cnpgresources.BuildCluster(recoveredProject, &project.Status.SecretNames)
	sourceStore := cnpgresources.BuildRecoveryObjectStore(recoveredProject)
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project).WithObjects(project, cluster, sourceStore).Build(),
		Scheme: scheme,
	}

	if err := reconciler.reconcileRecovery(context.Background(), project); err == nil {
		t.Fatal("recovery disable unexpectedly accepted an immutable bootstrap change")
	}
	retained := &barmancloudv1.ObjectStore{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(sourceStore), retained); err != nil {
		t.Fatalf("recovery source ObjectStore was deleted before immutable validation: %v", err)
	}
	condition := meta.FindStatusCondition(project.Status.Conditions, supabasev1alpha1.ConditionTypeDatabaseReady)
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != "BootstrapImmutable" {
		t.Fatalf("database condition = %#v, want BootstrapImmutable false", condition)
	}
}

func TestDurableMetadataProtectionRunsBeforeInvalidCredentials(t *testing.T) {
	t.Parallel()

	scheme := newIdempotencyTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{
		TypeMeta:   metav1.TypeMeta{APIVersion: supabasev1alpha1.GroupVersion.String(), Kind: "SupabaseProject"},
		ObjectMeta: metav1.ObjectMeta{Name: "protected", Namespace: "default", UID: "project-uid"},
		Spec: supabasev1alpha1.SupabaseProjectSpec{
			ProjectCredentialsSecret: "invalid-credentials",
			Database:                 supabasev1alpha1.DatabaseSpec{Instances: 1, Storage: cnpgv1.StorageConfiguration{Size: "1Gi"}},
		},
	}
	controller := true
	owner := metav1.OwnerReference{APIVersion: supabasev1alpha1.GroupVersion.Group + "/v1beta1", Kind: "SupabaseProject", Name: project.Name, UID: project.UID, Controller: &controller}
	cluster := cnpgresources.BuildCluster(project, &supabasev1alpha1.SecretNamesStatus{SupabaseAdmin: "protected-admin"})
	owner.UID = "old-project-uid"
	cluster.OwnerReferences = []metav1.OwnerReference{owner, {APIVersion: "example.dev/v1", Kind: "SupabaseProject", Name: project.Name, UID: "foreign"}}
	credentials := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "invalid-credentials", Namespace: project.Namespace}, Data: map[string][]byte{"publishableKey": []byte("not-valid")}}
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project).WithObjects(project, cluster, credentials).Build(),
		Scheme: scheme,
	}
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(project)}); err == nil {
		t.Fatal("invalid credentials unexpectedly reconciled")
	}
	updated := &cnpgv1.Cluster{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(cluster), updated); err != nil {
		t.Fatal(err)
	}
	if len(updated.OwnerReferences) != 1 || updated.OwnerReferences[0].APIVersion != "example.dev/v1" || updated.OwnerReferences[0].Kind != "SupabaseProject" || updated.OwnerReferences[0].Name != project.Name {
		t.Fatalf("owner references after invalid validation = %#v, want foreign owner only", updated.OwnerReferences)
	}
	status := &supabasev1alpha1.SupabaseProject{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(project), status); err != nil {
		t.Fatal(err)
	}
	condition := meta.FindStatusCondition(status.Status.Conditions, supabasev1alpha1.ConditionTypeSecretsReady)
	if condition == nil || condition.Status != metav1.ConditionFalse {
		t.Fatalf("SecretsReady condition = %#v, want false", condition)
	}
}

func TestCredentialRotationRollsOnlyCredentialConsumers(t *testing.T) {
	t.Parallel()

	scheme := newPowerSyncTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "rotation", Namespace: "default", UID: "project-uid"},
		Spec:       supabasev1alpha1.SupabaseProjectSpec{ProjectCredentialsSecret: "rotation-credentials"},
		Status: supabasev1alpha1.SupabaseProjectStatus{SecretNames: supabasev1alpha1.SecretNamesStatus{
			SupabaseAdmin: "rotation-admin", Authenticator: "rotation-authenticator", AuthAdmin: "rotation-auth-admin",
		}},
	}
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project).WithObjects(project).Build(),
		Scheme: scheme,
	}
	ctx := context.Background()
	if err := reconciler.reconcileServices(ctx, project, &secretresources.ProjectCredentials{PodTemplateHash: "old-hash"}); err != nil {
		t.Fatalf("initial reconcileServices() error = %v", err)
	}
	before := make(map[string]map[string]string)
	for _, name := range []string{
		deploymentresources.AuthDeploymentName(project), deploymentresources.RestDeploymentName(project), deploymentresources.StudioDeploymentName(project),
		deploymentresources.MetaDeploymentName(project), common.GatewayName(project),
	} {
		deployment := &appsv1.Deployment{}
		if err := reconciler.Get(ctx, types.NamespacedName{Name: name, Namespace: project.Namespace}, deployment); err != nil {
			t.Fatal(err)
		}
		before[name] = deployment.Spec.Template.Annotations
	}
	if err := reconciler.reconcileServices(ctx, project, &secretresources.ProjectCredentials{PodTemplateHash: "new-hash"}); err != nil {
		t.Fatalf("rotated reconcileServices() error = %v", err)
	}
	for _, name := range []string{
		deploymentresources.AuthDeploymentName(project), deploymentresources.RestDeploymentName(project), deploymentresources.StudioDeploymentName(project), common.GatewayName(project),
	} {
		deployment := &appsv1.Deployment{}
		if err := reconciler.Get(ctx, types.NamespacedName{Name: name, Namespace: project.Namespace}, deployment); err != nil {
			t.Fatal(err)
		}
		if got := deployment.Spec.Template.Annotations[projectCredentialsHashAnnotation]; got != "new-hash" {
			t.Fatalf("%s credential hash = %q, want new-hash", name, got)
		}
	}
	metaDeployment := &appsv1.Deployment{}
	if err := reconciler.Get(ctx, types.NamespacedName{Name: deploymentresources.MetaDeploymentName(project), Namespace: project.Namespace}, metaDeployment); err != nil {
		t.Fatal(err)
	}
	if _, exists := metaDeployment.Spec.Template.Annotations[projectCredentialsHashAnnotation]; exists {
		t.Fatalf("meta deployment received a credential hash: %#v", metaDeployment.Spec.Template.Annotations)
	}
	if _, exists := before[deploymentresources.MetaDeploymentName(project)][projectCredentialsHashAnnotation]; exists {
		t.Fatal("meta deployment unexpectedly had an old credential hash")
	}
}

func TestInvalidCredentialRotationLeavesExistingWorkloadsUnchanged(t *testing.T) {
	t.Parallel()

	scheme := newIdempotencyTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{
		TypeMeta:   metav1.TypeMeta{APIVersion: supabasev1alpha1.GroupVersion.String(), Kind: "SupabaseProject"},
		ObjectMeta: metav1.ObjectMeta{Name: "invalid-rotation", Namespace: "default", UID: "project-uid"},
		Spec: supabasev1alpha1.SupabaseProjectSpec{
			ProjectCredentialsSecret: "invalid-rotation-credentials",
			Database:                 supabasev1alpha1.DatabaseSpec{Instances: 1, Storage: cnpgv1.StorageConfiguration{Size: "1Gi"}},
		},
		Status: supabasev1alpha1.SupabaseProjectStatus{
			SecretNames: supabasev1alpha1.SecretNamesStatus{
				SupabaseAdmin: "invalid-rotation-admin", Authenticator: "invalid-rotation-authenticator", AuthAdmin: "invalid-rotation-auth-admin",
			},
			Conditions: []metav1.Condition{{Type: supabasev1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: "AllComponentsReady"}},
		},
	}
	invalidCredentials := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: project.Spec.ProjectCredentialsSecret, Namespace: project.Namespace}, Data: map[string][]byte{
		secretresources.ProjectCredentialsSigningKeysKey:    []byte("{"),
		secretresources.ProjectCredentialsPublishableKey:    []byte("sb_publishable_previous"),
		secretresources.ProjectCredentialsSecretKey:         []byte("sb_secret_previous"),
		secretresources.ProjectCredentialsAnonRoleJWTKey:    []byte("previous.anon.jwt"),
		secretresources.ProjectCredentialsServiceRoleJWTKey: []byte("previous.service.jwt"),
	}}
	workloads := []client.Object{
		deploymentresources.BuildAuthDeployment(project, &project.Status.SecretNames),
		deploymentresources.BuildRestDeployment(project, &project.Status.SecretNames),
		deploymentresources.BuildStudioDeployment(project, &project.Status.SecretNames),
		deploymentresources.BuildMetaDeployment(project, &project.Status.SecretNames),
		deploymentresources.BuildGatewayDeployment(project),
	}
	before := make(map[string]appsv1.DeploymentSpec, len(workloads))
	for _, object := range workloads {
		deployment := object.(*appsv1.Deployment)
		deployment.Spec.Template.Annotations[projectCredentialsHashAnnotation] = "last-valid"
		before[deployment.Name] = *deployment.Spec.DeepCopy()
	}
	objects := append([]client.Object{project, invalidCredentials}, workloads...)
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project).WithObjects(objects...).Build(),
		Scheme: scheme,
	}
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(project)}); err == nil {
		t.Fatal("invalid credential rotation unexpectedly reconciled")
	}
	for name, want := range before {
		deployment := &appsv1.Deployment{}
		if err := reconciler.Get(context.Background(), types.NamespacedName{Name: name, Namespace: project.Namespace}, deployment); err != nil {
			t.Fatal(err)
		}
		if !apiequality.Semantic.DeepEqual(deployment.Spec, want) {
			t.Fatalf("workload %s changed after invalid credential rotation", name)
		}
	}
	status := &supabasev1alpha1.SupabaseProject{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(project), status); err != nil {
		t.Fatal(err)
	}
	condition := meta.FindStatusCondition(status.Status.Conditions, supabasev1alpha1.ConditionTypeSecretsReady)
	if condition == nil || condition.Status != metav1.ConditionFalse {
		t.Fatalf("SecretsReady condition = %#v, want false", condition)
	}
	ready := meta.FindStatusCondition(status.Status.Conditions, supabasev1alpha1.ConditionTypeReady)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != "SecretsNotReady" {
		t.Fatalf("Ready condition = %#v, want SecretsNotReady false", ready)
	}
}

func TestReconcileSecretsReferenceFailuresClearOverallReady(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		credential string
		reason     string
	}{
		{name: "missing reference", reason: "CredentialsReferenceMissing"},
		{name: "unavailable secret", credential: "missing-credentials", reason: "CredentialsSecretUnavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			project := &supabasev1alpha1.SupabaseProject{
				ObjectMeta: metav1.ObjectMeta{Name: "secret-failure-" + strings.ReplaceAll(test.name, " ", "-"), Namespace: "default"},
				Spec:       supabasev1alpha1.SupabaseProjectSpec{ProjectCredentialsSecret: test.credential},
				Status: supabasev1alpha1.SupabaseProjectStatus{
					Conditions: []metav1.Condition{{Type: supabasev1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue}},
				},
			}
			scheme := newIdempotencyTestScheme(t)
			reconciler := &SupabaseProjectReconciler{
				Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project).WithObjects(project).Build(),
				Scheme: scheme,
			}
			if _, err := reconciler.reconcileSecrets(context.Background(), project); err == nil {
				t.Fatal("reconcileSecrets() unexpectedly succeeded")
			}

			updated := &supabasev1alpha1.SupabaseProject{}
			if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(project), updated); err != nil {
				t.Fatal(err)
			}
			secretsReady := meta.FindStatusCondition(updated.Status.Conditions, supabasev1alpha1.ConditionTypeSecretsReady)
			if secretsReady == nil || secretsReady.Status != metav1.ConditionFalse || secretsReady.Reason != test.reason {
				t.Fatalf("SecretsReady condition = %#v, want %s false", secretsReady, test.reason)
			}
			ready := meta.FindStatusCondition(updated.Status.Conditions, supabasev1alpha1.ConditionTypeReady)
			if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != "SecretsNotReady" {
				t.Fatalf("Ready condition = %#v, want SecretsNotReady false", ready)
			}
		})
	}
}

func TestMapDurableResourceToProjectIsNamespaceAndNameSafe(t *testing.T) {
	scheme := newPowerSyncTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{ObjectMeta: metav1.ObjectMeta{Name: "mapped", Namespace: "default"}}
	matching := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{
		Name: cnpgresources.ClusterName(project), Namespace: project.Namespace,
		Labels: common.CommonLabels(project),
	}}
	foreign := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{
		Name: cnpgresources.ClusterName(project), Namespace: "other",
		Labels: map[string]string{common.LabelInstance: project.Name},
	}}
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(project).Build(),
		Scheme: scheme,
	}
	matchingRequests := reconciler.mapDurableResourceToProject(context.Background(), matching)
	if len(matchingRequests) != 1 || matchingRequests[0].Name != project.Name || matchingRequests[0].Namespace != project.Namespace {
		t.Fatalf("matching requests = %#v", matchingRequests)
	}
	if requests := reconciler.mapDurableResourceToProject(context.Background(), foreign); len(requests) != 0 {
		t.Fatalf("cross-namespace resource was mapped: %#v", requests)
	}
	for _, durable := range []client.Object{
		&cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: cnpgresources.ClusterName(project), Namespace: project.Namespace}},
		&cnpgv1.ScheduledBackup{ObjectMeta: metav1.ObjectMeta{Name: cnpgresources.ScheduledBackupName(project), Namespace: project.Namespace}},
		&barmancloudv1.ObjectStore{ObjectMeta: metav1.ObjectMeta{Name: cnpgresources.ObjectStoreName(project), Namespace: project.Namespace}},
	} {
		if requests := reconciler.mapDurableResourceToProject(context.Background(), durable); len(requests) != 0 {
			t.Fatalf("missing-label %T was mapped: %#v", durable, requests)
		}
		durable.SetLabels(map[string]string{common.LabelInstance: "another-project"})
		if requests := reconciler.mapDurableResourceToProject(context.Background(), durable); len(requests) != 0 {
			t.Fatalf("foreign-label %T was mapped: %#v", durable, requests)
		}
	}
}

func TestMapProjectCredentialsSecretToProjects(t *testing.T) {
	scheme := newPowerSyncTestScheme(t)
	matching := &supabasev1alpha1.SupabaseProject{ObjectMeta: metav1.ObjectMeta{Name: "matching", Namespace: "default"}, Spec: supabasev1alpha1.SupabaseProjectSpec{ProjectCredentialsSecret: "credentials"}}
	unrelated := &supabasev1alpha1.SupabaseProject{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "default"}, Spec: supabasev1alpha1.SupabaseProjectSpec{ProjectCredentialsSecret: "other"}}
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(matching, unrelated).Build(),
		Scheme: scheme,
	}
	requests := reconciler.mapProjectCredentialsSecretToProjects(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "credentials", Namespace: "default"}})
	if len(requests) != 1 || requests[0].Name != matching.Name || requests[0].Namespace != matching.Namespace {
		t.Fatalf("credential secret requests = %#v", requests)
	}
}

func resourceQuantity(value string) resource.Quantity {
	return resource.MustParse(value)
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
