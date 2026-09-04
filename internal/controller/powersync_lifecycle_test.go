package controller

import (
	"context"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/configmaps"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/deployments"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/jobs"
)

func TestCleanupPowerSyncDeletesOwnedRuntimeResourcesAndClearsStatus(t *testing.T) {
	t.Parallel()

	scheme := newPowerSyncTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default", UID: "project-uid"},
		Status: supabasev1alpha1.SupabaseProjectStatus{
			Services: supabasev1alpha1.ServicesStatus{
				PowersyncAPI:         supabasev1alpha1.ServiceStatus{Ready: true},
				PowersyncReplication: supabasev1alpha1.ServiceStatus{Ready: true},
			},
			Conditions: []metav1.Condition{
				{Type: supabasev1alpha1.ConditionTypeCDCReady, Status: metav1.ConditionTrue},
				{Type: supabasev1alpha1.ConditionTypePowersyncReady, Status: metav1.ConditionTrue},
			},
		},
	}
	owned := []client.Object{
		&appsv1.Deployment{ObjectMeta: ownedMeta(project, deployments.PowersyncAPIDeploymentName(project))},
		&appsv1.Deployment{ObjectMeta: ownedMeta(project, deployments.PowersyncReplicationDeploymentName(project))},
		&corev1.Service{ObjectMeta: ownedMeta(project, project.Name+"-powersync-api")},
		&corev1.ConfigMap{ObjectMeta: ownedMeta(project, configmaps.PowersyncConfigMapName(project))},
		&corev1.ConfigMap{ObjectMeta: ownedMeta(project, configmaps.PowersyncSyncRulesConfigMapName(project))},
		&corev1.ConfigMap{ObjectMeta: ownedMeta(project, jobs.CDCConfigMapName(project))},
		&batchv1.Job{ObjectMeta: ownedMeta(project, jobs.CDCJobName(project))},
		&batchv1.CronJob{ObjectMeta: ownedMeta(project, deployments.PowersyncCompactCronJobName(project))},
		&cnpgv1.Publication{ObjectMeta: ownedMeta(project, project.Name+"-powersync")},
	}
	objects := append([]client.Object{project}, owned...)
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project).WithObjects(objects...).Build(),
		Scheme: scheme,
	}

	if err := reconciler.cleanupPowerSync(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	for _, object := range owned {
		err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(object), object)
		if !apierrors.IsNotFound(err) {
			t.Fatalf("%T %s was not deleted: %v", object, object.GetName(), err)
		}
	}
	if project.Status.Services.PowersyncAPI.Ready || project.Status.Services.PowersyncReplication.Ready {
		t.Fatal("stale PowerSync service status was not cleared")
	}
	if meta.FindStatusCondition(project.Status.Conditions, supabasev1alpha1.ConditionTypeCDCReady) != nil ||
		meta.FindStatusCondition(project.Status.Conditions, supabasev1alpha1.ConditionTypePowersyncReady) != nil {
		t.Fatal("stale PowerSync conditions were not cleared")
	}
}

func TestPowerSyncStatusNeedsCleanup(t *testing.T) {
	t.Parallel()

	project := &supabasev1alpha1.SupabaseProject{}
	if powerSyncStatusNeedsCleanup(project) {
		t.Fatal("never-enabled PowerSync must not trigger a status update")
	}
	project.Status.Services.PowersyncAPI.Ready = true
	if !powerSyncStatusNeedsCleanup(project) {
		t.Fatal("stale service status must trigger cleanup")
	}
}

func TestCleanupPowerSyncCompactDeletesOwnedCronJob(t *testing.T) {
	t.Parallel()

	scheme := newPowerSyncTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default", UID: "project-uid"}}
	cronJob := &batchv1.CronJob{ObjectMeta: ownedMeta(project, deployments.PowersyncCompactCronJobName(project))}
	reconciler := &SupabaseProjectReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(cronJob).Build(), Scheme: scheme}

	if err := reconciler.cleanupPowerSyncCompact(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(cronJob), cronJob); !apierrors.IsNotFound(err) {
		t.Fatalf("compact CronJob was not deleted: %v", err)
	}
}

func newPowerSyncTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		corev1.AddToScheme, appsv1.AddToScheme, batchv1.AddToScheme, cnpgv1.AddToScheme, supabasev1alpha1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	return scheme
}

func ownedMeta(project *supabasev1alpha1.SupabaseProject, name string) metav1.ObjectMeta {
	controller := true
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: project.Namespace,
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: supabasev1alpha1.GroupVersion.String(),
			Kind:       "SupabaseProject",
			Name:       project.Name,
			UID:        project.UID,
			Controller: &controller,
		}},
	}
}

func TestPowersyncDeploymentIsReadyForCurrentGeneration(t *testing.T) {
	t.Parallel()

	replicas := int32(2)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Generation: 3},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 3,
			UpdatedReplicas:    2,
			ReadyReplicas:      2,
			AvailableReplicas:  2,
		},
	}
	if !powersyncDeploymentIsReady(deployment) {
		t.Fatal("current rollout with all replicas ready must be ready")
	}
	deployment.Status.ObservedGeneration = 2
	if powersyncDeploymentIsReady(deployment) {
		t.Fatal("stale rollout status must not be ready")
	}
	deployment.Status.ObservedGeneration = 3
	deployment.Status.UpdatedReplicas = 1
	if powersyncDeploymentIsReady(deployment) {
		t.Fatal("old ready pods must not make an incomplete rollout ready")
	}
	deployment.Status.UpdatedReplicas = 2
	deployment.Status.ReadyReplicas = 1
	if powersyncDeploymentIsReady(deployment) {
		t.Fatal("partial rollout must not be ready")
	}
	deployment.Status.ReadyReplicas = 2
	deployment.Status.AvailableReplicas = 1
	deployment.Status.UnavailableReplicas = 1
	if powersyncDeploymentIsReady(deployment) {
		t.Fatal("unavailable rollout must not be ready")
	}
}

func TestPowerSyncManagedRolesReady(t *testing.T) {
	t.Parallel()

	cluster := &cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{ManagedRolesStatus: cnpgv1.ManagedRoles{
		ByStatus: map[cnpgv1.RoleStatus][]string{
			cnpgv1.RoleStatusReconciled: {"supabase_admin", "powersync_storage", "powersync_replication"},
		},
	}}}
	ready, err := powerSyncManagedRolesReady(cluster)
	if err != nil || !ready {
		t.Fatalf("reconciled roles should be ready: ready=%v err=%v", ready, err)
	}
	cluster.Status.ManagedRolesStatus.ByStatus[cnpgv1.RoleStatusReconciled] = []string{"powersync_storage"}
	ready, err = powerSyncManagedRolesReady(cluster)
	if err != nil || ready {
		t.Fatalf("missing role should be pending: ready=%v err=%v", ready, err)
	}
	cluster.Status.ManagedRolesStatus.CannotReconcile = map[string][]string{"powersync_replication": {"secret invalid"}}
	if _, err := powerSyncManagedRolesReady(cluster); err == nil {
		t.Fatal("unreconcilable PowerSync role must return an error")
	}
}

func TestLoadAndValidatePowerSyncRules(t *testing.T) {
	t.Parallel()

	scheme := newPowerSyncTestScheme(t)
	external := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "rules", Namespace: "default"},
		Data:       map[string]string{"sync_rules.yaml": "config:\n  edition: 3\nstreams: {}\n"},
	}
	reconciler := &SupabaseProjectReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(external).Build(), Scheme: scheme}

	tests := []struct {
		name    string
		rules   supabasev1alpha1.SyncRulesSpec
		wantErr bool
	}{
		{name: "inline edition 3", rules: supabasev1alpha1.SyncRulesSpec{Inline: "config:\n  edition: 3\nstreams: {}\n"}},
		{name: "inline missing streams", rules: supabasev1alpha1.SyncRulesSpec{Inline: "config:\n  edition: 3\n"}, wantErr: true},
		{name: "inline wrong edition", rules: supabasev1alpha1.SyncRulesSpec{Inline: "config:\n  edition: 2\nstreams: {}\n"}, wantErr: true},
		{name: "inline malformed", rules: supabasev1alpha1.SyncRulesSpec{Inline: "config: ["}, wantErr: true},
		{name: "external edition 3", rules: supabasev1alpha1.SyncRulesSpec{ConfigMapRef: "rules"}},
		{name: "external missing", rules: supabasev1alpha1.SyncRulesSpec{ConfigMapRef: "missing"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project := &supabasev1alpha1.SupabaseProject{
				ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default"},
				Spec:       supabasev1alpha1.SupabaseProjectSpec{Powersync: &supabasev1alpha1.PowersyncSpec{SyncRules: tt.rules}},
			}
			_, err := reconciler.loadAndValidatePowerSyncRules(context.Background(), project)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestApplyPowerSyncConfigHashChangesPodTemplate(t *testing.T) {
	t.Parallel()

	deploymentA := &appsv1.Deployment{}
	applyPowerSyncConfigHash(deploymentA, "config", []byte("rules-a"))
	hashA := deploymentA.Spec.Template.Annotations[powerSyncConfigHashAnnotation]
	if hashA == "" {
		t.Fatal("config hash annotation was not set")
	}
	deploymentB := &appsv1.Deployment{}
	applyPowerSyncConfigHash(deploymentB, "config", []byte("rules-b"))
	if hashB := deploymentB.Spec.Template.Annotations[powerSyncConfigHashAnnotation]; hashB == hashA {
		t.Fatal("sync rule changes must change the pod template hash")
	}
}

func TestMapExternalPowerSyncConfigMapToProjects(t *testing.T) {
	t.Parallel()

	scheme := newPowerSyncTestScheme(t)
	matching := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "matching", Namespace: "default"},
		Spec: supabasev1alpha1.SupabaseProjectSpec{Powersync: &supabasev1alpha1.PowersyncSpec{
			SyncRules: supabasev1alpha1.SyncRulesSpec{ConfigMapRef: "rules"},
		}},
	}
	unrelated := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "default"},
		Spec: supabasev1alpha1.SupabaseProjectSpec{Powersync: &supabasev1alpha1.PowersyncSpec{
			SyncRules: supabasev1alpha1.SyncRulesSpec{ConfigMapRef: "other"},
		}},
	}
	reconciler := &SupabaseProjectReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(matching, unrelated).Build(), Scheme: scheme}
	requests := reconciler.mapPowerSyncConfigMapToProjects(context.Background(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "rules", Namespace: "default"}})
	if len(requests) != 1 || requests[0].Name != "matching" || requests[0].Namespace != "default" {
		t.Fatalf("requests = %#v, want only default/matching", requests)
	}
}

func TestReconcilePowerSyncPublicationRepairsSpecAndOwnership(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := cnpgv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := supabasev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default", UID: "project-uid"},
		Spec:       supabasev1alpha1.SupabaseProjectSpec{Powersync: &supabasev1alpha1.PowersyncSpec{}},
	}
	publication := &cnpgv1.Publication{
		ObjectMeta: metav1.ObjectMeta{Name: "app-powersync", Namespace: "default"},
		Spec: cnpgv1.PublicationSpec{
			ClusterRef:    corev1.LocalObjectReference{Name: "app-pg"},
			Name:          "powersync",
			DBName:        "supabase",
			Target:        cnpgv1.PublicationTarget{Objects: []cnpgv1.PublicationTargetObject{{TablesInSchema: "wrong"}}},
			ReclaimPolicy: cnpgv1.PublicationReclaimRetain,
		},
		Status: cnpgv1.PublicationStatus{Applied: ptr.To(true)},
	}
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(project, publication).Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.reconcilePowerSyncPublication(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	updated := &cnpgv1.Publication{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(publication), updated); err != nil {
		t.Fatal(err)
	}
	if updated.Spec.Target.Objects[0].TablesInSchema != "public" || updated.Spec.ReclaimPolicy != cnpgv1.PublicationReclaimDelete {
		t.Fatalf("publication spec was not repaired: %#v", updated.Spec)
	}
	if !metav1.IsControlledBy(updated, project) {
		t.Fatal("publication owner reference was not repaired")
	}
}

func TestCreateOrCheckJobWaitsDuringRetryBackoff(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := supabasev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default", UID: "project-uid"},
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "app-cdc-permissions",
			Namespace:   "default",
			Annotations: map[string]string{cdcScriptHashAnnotation: "same"},
		},
		Status: batchv1.JobStatus{Failed: 1},
	}
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(job).Build(),
		Scheme: scheme,
	}

	completed, err := reconciler.createOrCheckJob(context.Background(), project, job.DeepCopy(), "same")
	if err != nil {
		t.Fatalf("retrying Job must not be terminal: %v", err)
	}
	if completed {
		t.Fatal("retrying Job must not be complete")
	}
}

func TestSetConditionRecordsObservedGeneration(t *testing.T) {
	t.Parallel()

	project := &supabasev1alpha1.SupabaseProject{ObjectMeta: metav1.ObjectMeta{Generation: 7}}
	reconciler := &SupabaseProjectReconciler{}
	reconciler.setCondition(project, supabasev1alpha1.ConditionTypePowersyncReady, metav1.ConditionFalse, "Pending", "waiting")
	if got := project.Status.Conditions[0].ObservedGeneration; got != 7 {
		t.Fatalf("observed generation = %d, want 7", got)
	}
}

func TestCreateOrCheckJobUsesTerminalConditions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		conditionType batchv1.JobConditionType
		wantComplete  bool
		wantError     bool
	}{
		{name: "complete", conditionType: batchv1.JobComplete, wantComplete: true},
		{name: "failed", conditionType: batchv1.JobFailed, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := batchv1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			if err := supabasev1alpha1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}

			project := &supabasev1alpha1.SupabaseProject{
				ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default", UID: "project-uid"},
			}
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "app-cdc-permissions",
					Namespace:   "default",
					Annotations: map[string]string{cdcScriptHashAnnotation: "same"},
				},
				Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: tt.conditionType, Status: "True"}}},
			}
			reconciler := &SupabaseProjectReconciler{
				Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(job).Build(),
				Scheme: scheme,
			}

			completed, err := reconciler.createOrCheckJob(context.Background(), project, job.DeepCopy(), "same")
			if (err != nil) != tt.wantError {
				t.Fatalf("error = %v, wantError %v", err, tt.wantError)
			}
			if completed != tt.wantComplete {
				t.Fatalf("completed = %v, want %v", completed, tt.wantComplete)
			}
		})
	}
}
