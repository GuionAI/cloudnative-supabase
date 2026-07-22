package controller

import (
	"context"
	"sync/atomic"
	"testing"

	barmancloudv1 "github.com/cloudnative-pg/plugin-barman-cloud/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

type updateCountingClient struct {
	client.Client
	updates       atomic.Int32
	statusUpdates atomic.Int32
}

func (c *updateCountingClient) Update(ctx context.Context, object client.Object, options ...client.UpdateOption) error {
	c.updates.Add(1)
	return c.Client.Update(ctx, object, options...)
}

func (c *updateCountingClient) Status() client.SubResourceWriter {
	return &statusUpdateCounter{SubResourceWriter: c.Client.Status(), updates: &c.statusUpdates}
}

type statusUpdateCounter struct {
	client.SubResourceWriter
	updates *atomic.Int32
}

func (w *statusUpdateCounter) Update(ctx context.Context, object client.Object, options ...client.SubResourceUpdateOption) error {
	w.updates.Add(1)
	return w.SubResourceWriter.Update(ctx, object, options...)
}

func TestOwnedResourceHelpersSkipNoOpUpdates(t *testing.T) {
	t.Parallel()

	scheme := newIdempotencyTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{
		TypeMeta:   metav1.TypeMeta{APIVersion: supabasev1alpha1.GroupVersion.String(), Kind: "SupabaseProject"},
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default", UID: "project-uid"},
	}
	configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: "default"}, Data: map[string]string{"key": "value"}}
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "deployment", Namespace: "default"}, Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)}}
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "service", Namespace: "default"}, Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 80}}}}
	cronJob := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "cron", Namespace: "default"}, Spec: batchv1.CronJobSpec{Schedule: "0 3 * * *"}}
	for _, object := range []client.Object{configMap, deployment, service, cronJob} {
		if err := setTestControllerReference(project, object, scheme); err != nil {
			t.Fatal(err)
		}
	}
	objectStore := &barmancloudv1.ObjectStore{ObjectMeta: metav1.ObjectMeta{Name: "store", Namespace: "default"}, Spec: barmancloudv1.ObjectStoreSpec{RetentionPolicy: "30d"}}
	scheduledBackup := &cnpgv1.ScheduledBackup{ObjectMeta: metav1.ObjectMeta{Name: "backup", Namespace: "default"}, Spec: cnpgv1.ScheduledBackupSpec{Schedule: "0 0 2 * * *"}}

	objects := []client.Object{
		configMap.DeepCopy(), deployment.DeepCopy(), service.DeepCopy(), cronJob.DeepCopy(), objectStore.DeepCopy(), scheduledBackup.DeepCopy(),
	}
	countingClient := &updateCountingClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()}
	reconciler := &SupabaseProjectReconciler{Client: countingClient, Scheme: scheme}

	ctx := context.Background()
	for _, call := range []func() error{
		func() error { return reconciler.createOrUpdateConfigMap(ctx, project, configMap.DeepCopy()) },
		func() error { return reconciler.createOrUpdateDeployment(ctx, project, deployment.DeepCopy()) },
		func() error { return reconciler.createOrUpdateService(ctx, project, service.DeepCopy()) },
		func() error { return reconciler.createOrUpdateCronJob(ctx, project, cronJob.DeepCopy()) },
		func() error { return reconciler.createOrUpdateObjectStore(ctx, objectStore.DeepCopy()) },
		func() error { return reconciler.createOrUpdateScheduledBackup(ctx, scheduledBackup.DeepCopy()) },
	} {
		if err := call(); err != nil {
			t.Fatal(err)
		}
	}
	if got := countingClient.updates.Load(); got != 0 {
		t.Fatalf("no-op owned resource updates = %d, want 0", got)
	}

	configMap.Data["key"] = "changed"
	deployment.Spec.Replicas = ptr.To[int32](2)
	service.Spec.Ports[0].Port = 8080
	cronJob.Spec.Schedule = "0 4 * * *"
	objectStore.Spec.RetentionPolicy = "60d"
	scheduledBackup.Spec.Schedule = "0 0 4 * * *"
	for _, call := range []func() error{
		func() error { return reconciler.createOrUpdateConfigMap(ctx, project, configMap.DeepCopy()) },
		func() error { return reconciler.createOrUpdateDeployment(ctx, project, deployment.DeepCopy()) },
		func() error { return reconciler.createOrUpdateService(ctx, project, service.DeepCopy()) },
		func() error { return reconciler.createOrUpdateCronJob(ctx, project, cronJob.DeepCopy()) },
		func() error { return reconciler.createOrUpdateObjectStore(ctx, objectStore.DeepCopy()) },
		func() error { return reconciler.createOrUpdateScheduledBackup(ctx, scheduledBackup.DeepCopy()) },
	} {
		if err := call(); err != nil {
			t.Fatal(err)
		}
	}
	if got := countingClient.updates.Load(); got != 6 {
		t.Fatalf("drift updates = %d, want 6", got)
	}
}

func TestReconcileSecretsSkipsUnchangedStatusUpdate(t *testing.T) {
	t.Parallel()

	scheme := newIdempotencyTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default", UID: "project-uid"},
		Status: supabasev1alpha1.SupabaseProjectStatus{
			Phase: supabasev1alpha1.PhaseRunning,
		},
	}
	objects := make([]client.Object, 0, 5)
	objects = append(objects, project)
	for _, name := range []string{"app-jwt", "app-supabase-admin-password", "app-authenticator-password", "app-auth-admin-password"} {
		objects = append(objects, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}})
	}
	countingClient := &updateCountingClient{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project).WithObjects(objects...).Build(),
	}
	reconciler := &SupabaseProjectReconciler{Client: countingClient, Scheme: scheme}

	ctx := context.Background()
	if err := reconciler.reconcileSecrets(ctx, project); err != nil {
		t.Fatal(err)
	}
	countingClient.statusUpdates.Store(0)
	if err := reconciler.reconcileSecrets(ctx, project); err != nil {
		t.Fatal(err)
	}
	if got := countingClient.statusUpdates.Load(); got != 0 {
		t.Fatalf("unchanged status updates = %d, want 0", got)
	}
}

func newIdempotencyTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := newPowerSyncTestScheme(t)
	if err := barmancloudv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func setTestControllerReference(project *supabasev1alpha1.SupabaseProject, object client.Object, scheme *runtime.Scheme) error {
	return controllerutil.SetControllerReference(project, object, scheme)
}
