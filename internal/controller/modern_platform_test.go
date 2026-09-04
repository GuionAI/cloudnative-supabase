package controller

import (
	"context"
	"reflect"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	cnpgresources "github.com/GuionAI/cloudnative-supabase/internal/resources/cnpg"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
)

func TestReconcileClusterMutableFieldsPreservesForeignConfiguration(t *testing.T) {
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "mutable", Namespace: "default"},
		Spec: supabasev1alpha1.SupabaseProjectSpec{Database: supabasev1alpha1.DatabaseSpec{
			Instances:             2,
			Image:                 "postgres:17-custom",
			EnableSuperuserAccess: true,
			Storage:               cnpgv1.StorageConfiguration{Size: "20Gi"},
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
	if existing.Spec.Resources.Requests.Cpu().String() != "10m" {
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
		{APIVersion: supabasev1alpha1.GroupVersion.String(), Kind: "SupabaseProject", Name: project.Name, UID: project.UID, Controller: &controller},
		{APIVersion: "example.dev/v1", Kind: "DatabaseOperator", Name: "database-owner", UID: "foreign-uid"},
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
	if len(updated.OwnerReferences) != 1 || updated.OwnerReferences[0].Kind != "DatabaseOperator" {
		t.Fatalf("owner references = %#v, want only foreign owner", updated.OwnerReferences)
	}
	if updated.Labels["foreign.example/keep"] != "yes" || updated.Labels[common.LabelInstance] != project.Name {
		t.Fatalf("labels were not preserved/repaired: %#v", updated.Labels)
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
