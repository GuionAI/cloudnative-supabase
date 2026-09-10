package controller

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	deploymentresources "github.com/GuionAI/cloudnative-supabase/internal/resources/deployments"
	secretresources "github.com/GuionAI/cloudnative-supabase/internal/resources/secrets"
)

func TestReconcilePowersyncSecretsCreatesAPITokenWithoutRotatingExistingRoles(t *testing.T) {
	t.Parallel()

	project := powersyncTokenTestProject("token-create")
	scheme := newPowerSyncTestScheme(t)
	storage := powersyncRoleSecret(project, "powersync-storage-password", "powersync_storage", "storage-before")
	replication := powersyncRoleSecret(project, "powersync-replication-password", "powersync_replication", "replication-before")
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(project, storage, replication).Build(),
		Scheme: scheme,
	}
	names := &supabasev1alpha1.SecretNamesStatus{}

	if err := reconciler.reconcilePowersyncSecrets(context.Background(), project, names); err != nil {
		t.Fatalf("reconcilePowersyncSecrets() error = %v", err)
	}

	assertPowerSyncRolePassword(t, reconciler, storage, "storage-before")
	assertPowerSyncRolePassword(t, reconciler, replication, "replication-before")
	token := &corev1.Secret{}
	if err := reconciler.Get(context.Background(), types.NamespacedName{
		Name: secretresources.PowersyncAPITokenSecretName(project), Namespace: project.Namespace,
	}, token); err != nil {
		t.Fatalf("created API token Secret: %v", err)
	}
	if err := secretresources.ValidatePowersyncAPITokenSecret(token); err != nil {
		t.Fatalf("created API token Secret failed validation: %v", err)
	}
	if owner := metav1.GetControllerOf(token); owner == nil || owner.Name != project.Name || owner.UID != project.UID {
		t.Fatalf("API token Secret owner = %#v, want project %s/%s", owner, project.Namespace, project.Name)
	}
	if names.PowersyncStoragePassword != storage.Name || names.PowersyncReplicationPassword != replication.Name {
		t.Fatalf("PowerSync role names = %#v", names)
	}
}

func TestReconcileImplementationSecretsCreatesPowerSyncTokenForFreshEnabledProject(t *testing.T) {
	t.Parallel()

	project := powersyncTokenTestProject("token-fresh")
	scheme := newPowerSyncTestScheme(t)
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(project).Build(),
		Scheme: scheme,
	}
	if err := reconciler.reconcileImplementationSecrets(context.Background(), project); err != nil {
		t.Fatalf("reconcileImplementationSecrets() error = %v", err)
	}

	for _, name := range []string{
		secretresources.PowersyncAPITokenSecretName(project),
		project.Name + "-powersync-storage-password",
		project.Name + "-powersync-replication-password",
	} {
		secret := &corev1.Secret{}
		if err := reconciler.Get(context.Background(), types.NamespacedName{Name: name, Namespace: project.Namespace}, secret); err != nil {
			t.Fatalf("fresh PowerSync Secret %q: %v", name, err)
		}
	}
}

func TestReconcilePowersyncSecretsPreservesTokenAndRoleCredentialsOnRepeat(t *testing.T) {
	t.Parallel()

	project := powersyncTokenTestProject("token-repeat")
	scheme := newPowerSyncTestScheme(t)
	storage := powersyncRoleSecret(project, "powersync-storage-password", "powersync_storage", "storage-stable")
	replication := powersyncRoleSecret(project, "powersync-replication-password", "powersync_replication", "replication-stable")
	token := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretresources.PowersyncAPITokenSecretName(project), Namespace: project.Namespace},
		Data:       map[string][]byte{secretresources.PowersyncAPITokenSecretKey: []byte("fixture-api-token")},
	}
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(project, storage, replication, token).Build(),
		Scheme: scheme,
	}

	for range 2 {
		if err := reconciler.reconcilePowersyncSecrets(context.Background(), project, &supabasev1alpha1.SecretNamesStatus{}); err != nil {
			t.Fatalf("reconcilePowersyncSecrets() error = %v", err)
		}
	}

	assertPowerSyncRolePassword(t, reconciler, storage, "storage-stable")
	assertPowerSyncRolePassword(t, reconciler, replication, "replication-stable")
	updatedToken := &corev1.Secret{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(token), updatedToken); err != nil {
		t.Fatal(err)
	}
	if got := string(updatedToken.Data[secretresources.PowersyncAPITokenSecretKey]); got != "fixture-api-token" {
		t.Fatalf("API token changed on repeat reconcile: %q", got)
	}
	if owner := metav1.GetControllerOf(updatedToken); owner == nil || owner.Name != project.Name {
		t.Fatalf("existing API token Secret was not adopted by the project: %#v", owner)
	}
}

func TestReconcilePowersyncSecretsRejectsInvalidAPITokenWithoutReplacement(t *testing.T) {
	t.Parallel()

	project := powersyncTokenTestProject("token-invalid")
	scheme := newPowerSyncTestScheme(t)
	storage := powersyncRoleSecret(project, "powersync-storage-password", "powersync_storage", "storage-stable")
	replication := powersyncRoleSecret(project, "powersync-replication-password", "powersync_replication", "replication-stable")
	token := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretresources.PowersyncAPITokenSecretName(project), Namespace: project.Namespace},
		Data:       map[string][]byte{},
	}
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(project, storage, replication, token).Build(),
		Scheme: scheme,
	}

	err := reconciler.reconcilePowersyncSecrets(context.Background(), project, &supabasev1alpha1.SecretNamesStatus{})
	if err == nil || !strings.Contains(err.Error(), "missing required non-empty key") {
		t.Fatalf("invalid API token result = %v, want missing-key error", err)
	}
	if strings.Contains(err.Error(), "fixture") {
		t.Fatalf("API token value leaked in validation error: %v", err)
	}
	unchanged := &corev1.Secret{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(token), unchanged); err != nil {
		t.Fatal(err)
	}
	if len(unchanged.Data) != 0 {
		t.Fatalf("invalid API token Secret was replaced: %#v", unchanged.Data)
	}
}

func TestReconcileSecretsReportsPowerSyncTokenFailureWithoutValue(t *testing.T) {
	t.Parallel()

	project := powersyncTokenTestProject("token-status")
	project.Spec.ProjectCredentialsSecret = "token-status-credentials"
	scheme := newPowerSyncTestScheme(t)
	credentials := validProjectCredentialsSecret(t, project, project.Spec.ProjectCredentialsSecret)
	invalidToken := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretresources.PowersyncAPITokenSecretName(project), Namespace: project.Namespace},
		Data:       map[string][]byte{},
	}
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project).WithObjects(project, credentials, invalidToken).Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.reconcileSecrets(context.Background(), project); err == nil {
		t.Fatal("reconcileSecrets() unexpectedly accepted an invalid PowerSync API token")
	}
	updated := &supabasev1alpha1.SupabaseProject{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(project), updated); err != nil {
		t.Fatal(err)
	}
	condition := meta.FindStatusCondition(updated.Status.Conditions, supabasev1alpha1.ConditionTypeSecretsReady)
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != "ImplementationSecretFailed" {
		t.Fatalf("SecretsReady condition = %#v, want safe implementation-secret failure", condition)
	}
	if strings.Contains(condition.Message, "fixture-api-token") {
		t.Fatalf("API token value leaked into status: %q", condition.Message)
	}
}

func TestReconcilePowersyncSecretsDoesNotTakeOverForeignAPIToken(t *testing.T) {
	t.Parallel()

	project := powersyncTokenTestProject("token-foreign")
	scheme := newPowerSyncTestScheme(t)
	storage := powersyncRoleSecret(project, "powersync-storage-password", "powersync_storage", "storage-stable")
	replication := powersyncRoleSecret(project, "powersync-replication-password", "powersync_replication", "replication-stable")
	controller := true
	token := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretresources.PowersyncAPITokenSecretName(project),
			Namespace: project.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "secrets.example/v1", Kind: "SecretManager", Name: "foreign", UID: "foreign-uid", Controller: &controller,
			}},
		},
		Data: map[string][]byte{secretresources.PowersyncAPITokenSecretKey: []byte("foreign-api-token")},
	}
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(project, storage, replication, token).Build(),
		Scheme: scheme,
	}

	err := reconciler.reconcilePowersyncSecrets(context.Background(), project, &supabasev1alpha1.SecretNamesStatus{})
	if err == nil || !strings.Contains(err.Error(), "already owned") {
		t.Fatalf("foreign API token result = %v, want ownership error", err)
	}
	unchanged := &corev1.Secret{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(token), unchanged); err != nil {
		t.Fatal(err)
	}
	if len(unchanged.OwnerReferences) != 1 || unchanged.OwnerReferences[0].Name != "foreign" {
		t.Fatalf("foreign API token ownership changed: %#v", unchanged.OwnerReferences)
	}
}

func TestReconcileImplementationSecretsDoesNotCreatePowerSyncTokenWhenDisabled(t *testing.T) {
	t.Parallel()

	project := &supabasev1alpha1.SupabaseProject{ObjectMeta: metav1.ObjectMeta{Name: "token-disabled", Namespace: "default"}}
	scheme := newPowerSyncTestScheme(t)
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(project).Build(),
		Scheme: scheme,
	}
	if err := reconciler.reconcileImplementationSecrets(context.Background(), project); err != nil {
		t.Fatalf("reconcileImplementationSecrets() error = %v", err)
	}
	token := &corev1.Secret{}
	err := reconciler.Get(context.Background(), types.NamespacedName{Name: secretresources.PowersyncAPITokenSecretName(project), Namespace: project.Namespace}, token)
	if err == nil {
		t.Fatal("disabled PowerSync unexpectedly created an API token Secret")
	}
}

func TestCreateOrUpdateDeploymentReplacesPowerSyncReplicationStrategy(t *testing.T) {
	t.Parallel()

	project := powersyncTokenTestProject("strategy")
	scheme := newPowerSyncTestScheme(t)
	desired := deploymentresources.BuildPowersyncReplicationDeployment(project, &supabasev1alpha1.SecretNamesStatus{
		PowersyncStoragePassword:     "storage",
		PowersyncReplicationPassword: "replication",
	})
	existing := desired.DeepCopy()
	existing.Spec.Strategy = appsv1.DeploymentStrategy{
		Type:          appsv1.RollingUpdateDeploymentStrategyType,
		RollingUpdate: &appsv1.RollingUpdateDeployment{MaxUnavailable: ptr.To(intstr.FromString("25%"))},
	}
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(project, existing).Build(),
		Scheme: scheme,
	}
	if err := reconciler.createOrUpdateDeployment(context.Background(), project, desired); err != nil {
		t.Fatalf("createOrUpdateDeployment() error = %v", err)
	}
	updated := &appsv1.Deployment{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(existing), updated); err != nil {
		t.Fatal(err)
	}
	if updated.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType || updated.Spec.Strategy.RollingUpdate != nil {
		t.Fatalf("updated replication strategy = %#v, want Recreate without rollingUpdate", updated.Spec.Strategy)
	}
}

func powersyncTokenTestProject(name string) *supabasev1alpha1.SupabaseProject {
	return &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", UID: types.UID(name + "-uid")},
		Spec:       supabasev1alpha1.SupabaseProjectSpec{Powersync: &supabasev1alpha1.PowersyncSpec{}},
	}
}

func powersyncRoleSecret(project *supabasev1alpha1.SupabaseProject, suffix, username, password string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: project.Name + "-" + suffix, Namespace: project.Namespace},
		Data: map[string][]byte{
			"username": []byte(username),
			"password": []byte(password),
		},
	}
}

func assertPowerSyncRolePassword(t *testing.T, reconciler *SupabaseProjectReconciler, original *corev1.Secret, want string) {
	t.Helper()
	updated := &corev1.Secret{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(original), updated); err != nil {
		t.Fatal(err)
	}
	if got := string(updated.Data["password"]); got != want {
		t.Fatalf("%s password = %q, want %q", original.Name, got, want)
	}
}
