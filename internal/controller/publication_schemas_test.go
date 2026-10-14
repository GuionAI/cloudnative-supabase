package controller

import (
	"context"
	"reflect"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	cnpgresources "github.com/GuionAI/cloudnative-supabase/internal/resources/cnpg"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/deployments"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/jobs"
)

func TestPublicationSchemaChangesReconcileAndRerunCDC(t *testing.T) {
	ctx := context.Background()
	scheme := newPowerSyncTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default", UID: "project-uid"}, Spec: supabasev1alpha1.SupabaseProjectSpec{Powersync: &supabasev1alpha1.PowersyncSpec{}}}
	publication, err := cnpgresources.BuildPowerSyncPublication(project)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := jobs.BuildCDCMigrationsConfigMap(project)
	if err != nil {
		t.Fatal(err)
	}
	job := jobs.BuildCDCPermissionsJob(project, &project.Status.SecretNames)
	job.Annotations = map[string]string{cdcScriptHashAnnotation: cdcScriptHash(cm)}
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	r := &SupabaseProjectReconciler{Scheme: scheme, Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project).WithObjects(project, publication, job).Build()}
	restBefore := deployments.BuildRestDeployment(project, &project.Status.SecretNames)
	for _, schemas := range [][]string{{"public", "app_data"}, {"app_data"}} {
		project.Spec.Powersync.PublicationSchemas = schemas
		if err := r.Update(ctx, project); err != nil {
			t.Fatal(err)
		}
		if _, err := r.reconcilePowerSyncPublication(ctx, project); err != nil {
			t.Fatal(err)
		}
		got := &cnpgv1.Publication{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(publication), got); err != nil {
			t.Fatal(err)
		}
		var targets []string
		for _, target := range got.Spec.Target.Objects {
			targets = append(targets, target.TablesInSchema)
		}
		if !reflect.DeepEqual(targets, schemas) {
			t.Fatalf("targets = %v, want %v", targets, schemas)
		}
		if _, err := r.reconcileCDCPermissions(ctx, project); err != nil {
			t.Fatal(err)
		}
		if err := r.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{}); !apierrors.IsNotFound(err) {
			t.Fatalf("stale completed job retained: %v", err)
		}
		if _, err := r.reconcileCDCPermissions(ctx, project); err != nil {
			t.Fatal(err)
		}
		current := &batchv1.Job{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(job), current); err != nil {
			t.Fatal(err)
		}
		currentCM := &corev1.ConfigMap{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(cm), currentCM); err != nil {
			t.Fatal(err)
		}
		if current.Annotations[cdcScriptHashAnnotation] != cdcScriptHash(currentCM) || current.Annotations[cdcScriptHashAnnotation] == job.Annotations[cdcScriptHashAnnotation] {
			t.Fatal("new job must use changed script hash")
		}
		current.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
		if err := r.Status().Update(ctx, current); err != nil {
			t.Fatal(err)
		}
		job = current
	}
	if !reflect.DeepEqual(restBefore, deployments.BuildRestDeployment(project, &project.Status.SecretNames)) {
		t.Fatal("replication schemas changed REST exposure")
	}
}

func TestSchemaReconcileRefusesForeignControllers(t *testing.T) {
	scheme := newPowerSyncTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default", UID: "project-uid"}, Spec: supabasev1alpha1.SupabaseProjectSpec{Powersync: &supabasev1alpha1.PowersyncSpec{PublicationSchemas: []string{"app_data"}}}}
	publication, err := cnpgresources.BuildPowerSyncPublication(project)
	if err != nil {
		t.Fatal(err)
	}
	controlled := true
	foreign := []metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: "foreign", UID: "foreign-uid", Controller: &controlled}}
	publication.OwnerReferences = foreign
	publication.Spec.Target.Objects = []cnpgv1.PublicationTargetObject{{TablesInSchema: "public"}}
	job := jobs.BuildCDCPermissionsJob(project, &project.Status.SecretNames)
	job.OwnerReferences = foreign
	job.Annotations = map[string]string{cdcScriptHashAnnotation: "old"}
	r := &SupabaseProjectReconciler{Scheme: scheme, Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(publication, job).Build()}
	if _, err := r.reconcilePowerSyncPublication(context.Background(), project); err == nil {
		t.Fatal("adopted foreign publication")
	}
	if _, err := r.createOrCheckJob(context.Background(), project, jobs.BuildCDCPermissionsJob(project, &project.Status.SecretNames), "new"); err == nil {
		t.Fatal("replaced foreign job")
	}
	got := &cnpgv1.Publication{}
	if err := r.Get(context.Background(), client.ObjectKeyFromObject(publication), got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Spec, publication.Spec) {
		t.Fatal("mutated foreign publication")
	}
	if err := r.Get(context.Background(), client.ObjectKeyFromObject(job), &batchv1.Job{}); err != nil {
		t.Fatal("deleted foreign job")
	}
}

func TestCDCResourcesRefuseUnlabelledNames(t *testing.T) {
	scheme := newPowerSyncTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default", UID: "project-uid"}, Spec: supabasev1alpha1.SupabaseProjectSpec{Powersync: &supabasev1alpha1.PowersyncSpec{}}}
	publication, err := cnpgresources.BuildPowerSyncPublication(project)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := jobs.BuildCDCMigrationsConfigMap(project)
	if err != nil {
		t.Fatal(err)
	}
	job := jobs.BuildCDCPermissionsJob(project, &project.Status.SecretNames)
	for _, object := range []client.Object{publication, cm, job} {
		object.SetLabels(nil)
	}
	r := &SupabaseProjectReconciler{Scheme: scheme, Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(publication, cm, job).Build()}
	ctx := context.Background()
	if _, err := r.reconcilePowerSyncPublication(ctx, project); err == nil {
		t.Fatal("adopted unlabelled publication")
	}
	desiredCM, err := jobs.BuildCDCMigrationsConfigMap(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.createOrUpdateConfigMap(ctx, project, desiredCM); err == nil {
		t.Fatal("adopted unlabelled ConfigMap")
	}
	if _, err := r.createOrCheckJob(ctx, project, jobs.BuildCDCPermissionsJob(project, &project.Status.SecretNames), "new"); err == nil {
		t.Fatal("deleted unlabelled Job")
	}
	for _, object := range []client.Object{publication, cm, job} {
		if err := r.Get(ctx, client.ObjectKeyFromObject(object), object); err != nil {
			t.Fatal(err)
		}
		if len(object.GetLabels()) != 0 || len(object.GetOwnerReferences()) != 0 {
			t.Fatal("mutated unlabelled resource")
		}
	}
}
