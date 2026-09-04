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
	cnpgresources "github.com/GuionAI/cloudnative-supabase/internal/resources/cnpg"
	commonresources "github.com/GuionAI/cloudnative-supabase/internal/resources/common"
	deploymentresources "github.com/GuionAI/cloudnative-supabase/internal/resources/deployments"
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
	objectStore := &barmancloudv1.ObjectStore{ObjectMeta: metav1.ObjectMeta{Name: "store", Namespace: "default", Labels: commonresources.CommonLabels(project)}, Spec: barmancloudv1.ObjectStoreSpec{RetentionPolicy: "30d"}}
	scheduledBackup := &cnpgv1.ScheduledBackup{ObjectMeta: metav1.ObjectMeta{Name: "backup", Namespace: "default", Labels: commonresources.CommonLabels(project)}, Spec: cnpgv1.ScheduledBackupSpec{Schedule: "0 0 2 * * *"}}

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

func TestOwnedResourceHelpersRepairClearedFields(t *testing.T) {
	t.Parallel()

	scheme := newIdempotencyTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{
		TypeMeta:   metav1.TypeMeta{APIVersion: supabasev1alpha1.GroupVersion.String(), Kind: "SupabaseProject"},
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default", UID: "project-uid"},
		Spec: supabasev1alpha1.SupabaseProjectSpec{Auth: supabasev1alpha1.AuthSpec{
			EmailHook: &supabasev1alpha1.EmailHookSpec{Enabled: true, URI: "https://hook.example.com"},
		}},
	}
	secretNames := supabasev1alpha1.SecretNamesStatus{
		GoTrueFallback: "app-gotrue-jwt-secret", AuthAdmin: "app-auth-admin-password",
	}
	existingDeployment := deploymentresources.BuildAuthDeployment(project, &secretNames)
	project.Spec.Auth.EmailHook.Enabled = false
	deployment := deploymentresources.BuildAuthDeployment(project, &secretNames)
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "service", Namespace: "default"}}
	cronJob := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "cron", Namespace: "default"}}
	for _, object := range []client.Object{deployment, service, cronJob} {
		if err := setTestControllerReference(project, object, scheme); err != nil {
			t.Fatal(err)
		}
	}
	objectStore := &barmancloudv1.ObjectStore{ObjectMeta: metav1.ObjectMeta{Name: "store", Namespace: "default", Labels: commonresources.CommonLabels(project)}}
	scheduledBackup := &cnpgv1.ScheduledBackup{ObjectMeta: metav1.ObjectMeta{Name: "backup", Namespace: "default", Labels: commonresources.CommonLabels(project)}}

	existingService := service.DeepCopy()
	existingService.Spec.Ports = []corev1.ServicePort{{Name: "stale", Port: 80}}
	existingCronJob := cronJob.DeepCopy()
	existingCronJob.Spec.Schedule = "0 3 * * *"
	existingObjectStore := objectStore.DeepCopy()
	existingObjectStore.Spec.RetentionPolicy = "30d"
	existingObjectStore.Spec.InstanceSidecarConfiguration.LogLevel = "info"
	existingScheduledBackup := scheduledBackup.DeepCopy()
	existingScheduledBackup.Spec.Schedule = "0 0 2 * * *"

	countingClient := &updateCountingClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		existingDeployment, existingService, existingCronJob, existingObjectStore, existingScheduledBackup,
	).Build()}
	reconciler := &SupabaseProjectReconciler{Client: countingClient, Scheme: scheme}
	ctx := context.Background()

	for _, call := range []func() error{
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
	if got := countingClient.updates.Load(); got != 5 {
		t.Fatalf("cleared-field updates = %d, want 5", got)
	}

	updatedDeployment := &appsv1.Deployment{}
	if err := countingClient.Get(ctx, client.ObjectKeyFromObject(deployment), updatedDeployment); err != nil {
		t.Fatal(err)
	}
	for _, env := range updatedDeployment.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "GOTRUE_HOOK_SEND_EMAIL_ENABLED" || env.Name == "GOTRUE_HOOK_SEND_EMAIL_URI" {
			t.Fatalf("disabled email hook env was not removed: %#v", env)
		}
	}
	updatedService := &corev1.Service{}
	if err := countingClient.Get(ctx, client.ObjectKeyFromObject(service), updatedService); err != nil {
		t.Fatal(err)
	}
	if got := updatedService.Spec.Ports; len(got) != 0 {
		t.Fatalf("service ports were not cleared: %#v", got)
	}
	updatedObjectStore := &barmancloudv1.ObjectStore{}
	if err := countingClient.Get(ctx, client.ObjectKeyFromObject(objectStore), updatedObjectStore); err != nil {
		t.Fatal(err)
	}
	if updatedObjectStore.Spec.RetentionPolicy != "" {
		t.Fatalf("retention policy = %q, want empty", updatedObjectStore.Spec.RetentionPolicy)
	}
	if updatedObjectStore.Spec.InstanceSidecarConfiguration.LogLevel != "info" {
		t.Fatalf("API-managed sidecar settings were overwritten: %#v", updatedObjectStore.Spec.InstanceSidecarConfiguration)
	}
}

func TestReconcilePowerSyncPublicationRemovesStaleParameters(t *testing.T) {
	t.Parallel()

	scheme := newIdempotencyTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{
		TypeMeta:   metav1.TypeMeta{APIVersion: supabasev1alpha1.GroupVersion.String(), Kind: "SupabaseProject"},
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default", UID: "project-uid"},
	}
	existing := cnpgresources.BuildPowerSyncPublication(project)
	existing.Spec.Parameters = map[string]string{"publish": "insert"}
	existing.Status.Applied = ptr.To(true)
	if err := setTestControllerReference(project, existing, scheme); err != nil {
		t.Fatal(err)
	}
	countingClient := &updateCountingClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()}
	reconciler := &SupabaseProjectReconciler{Client: countingClient, Scheme: scheme}
	ctx := context.Background()

	if _, err := reconciler.reconcilePowerSyncPublication(ctx, project); err != nil {
		t.Fatal(err)
	}
	if got := countingClient.updates.Load(); got != 1 {
		t.Fatalf("publication updates = %d, want 1", got)
	}
	updated := &cnpgv1.Publication{}
	if err := countingClient.Get(ctx, client.ObjectKeyFromObject(existing), updated); err != nil {
		t.Fatal(err)
	}
	if len(updated.Spec.Parameters) != 0 {
		t.Fatalf("publication parameters were not cleared: %#v", updated.Spec.Parameters)
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
