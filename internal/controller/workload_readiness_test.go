package controller

import (
	"context"
	"errors"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/cnpg"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/deployments"
	secretresources "github.com/GuionAI/cloudnative-supabase/internal/resources/secrets"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/services"
)

func TestReconcileServiceComponentReportsPendingDeployment(t *testing.T) {
	t.Parallel()

	project := &supabasev1alpha1.SupabaseProject{
		TypeMeta: metav1.TypeMeta{APIVersion: supabasev1alpha1.GroupVersion.String(), Kind: "SupabaseProject"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "pending", Namespace: "default", UID: "pending-project-uid",
		},
	}
	replicas := int32(2)
	setStatus := supabasev1alpha1.ServiceStatus{}
	config := newServiceReconcileConfig(
		"Auth",
		supabasev1alpha1.ConditionTypeAuthReady,
		func() *appsv1.Deployment {
			return &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "pending-auth", Namespace: project.Namespace},
				Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
			}
		},
		func() *corev1.Service {
			return &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "pending-auth", Namespace: project.Namespace}}
		},
		func(status supabasev1alpha1.ServiceStatus) { setStatus = status },
	)
	scheme := newIdempotencyTestScheme(t)
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(project).Build(),
		Scheme: scheme,
	}

	if err := reconciler.reconcileServiceComponent(context.Background(), project, config); err != nil {
		t.Fatal(err)
	}
	if setStatus.Ready || setStatus.AvailableReplicas != 0 {
		t.Fatalf("pending status = %#v, want not ready with zero available replicas", setStatus)
	}
	condition := meta.FindStatusCondition(project.Status.Conditions, supabasev1alpha1.ConditionTypeAuthReady)
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != "DeploymentPending" {
		t.Fatalf("AuthReady condition = %#v, want DeploymentPending false", condition)
	}
}

func TestReconcileServicesReportsMixedDeploymentReadiness(t *testing.T) {
	t.Parallel()

	project, reconciler := newCoreReadinessFixture(t, true)

	if err := reconciler.reconcileServices(context.Background(), project, &secretresources.ProjectCredentials{}); err != nil {
		t.Fatal(err)
	}

	status := &supabasev1alpha1.SupabaseProject{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(project), status); err != nil {
		t.Fatal(err)
	}
	if status.Status.Phase != supabasev1alpha1.PhaseProvisioning {
		t.Fatalf("phase = %q, want Provisioning", status.Status.Phase)
	}
	if status.Status.ObservedGeneration != 4 {
		t.Fatalf("observedGeneration = %d, want previous generation 4", status.Status.ObservedGeneration)
	}
	if status.Status.Services.Auth.AvailableReplicas != 2 || !status.Status.Services.Auth.Ready {
		t.Fatalf("auth status = %#v, want ready with two available replicas", status.Status.Services.Auth)
	}
	if status.Status.Services.Rest.AvailableReplicas != 1 || status.Status.Services.Rest.Ready {
		t.Fatalf("rest status = %#v, want pending with one available replica", status.Status.Services.Rest)
	}
	for name, service := range map[string]supabasev1alpha1.ServiceStatus{
		"studio":  status.Status.Services.Studio,
		"meta":    status.Status.Services.Meta,
		"gateway": status.Status.Services.Gateway,
	} {
		if !service.Ready || service.AvailableReplicas != 1 {
			t.Fatalf("%s status = %#v, want ready with one available replica", name, service)
		}
	}
	ready := meta.FindStatusCondition(status.Status.Conditions, supabasev1alpha1.ConditionTypeReady)
	if ready == nil || ready.Status != metav1.ConditionFalse {
		t.Fatalf("Ready condition = %#v, want false while one core deployment converges", ready)
	}
	for _, conditionType := range []string{
		supabasev1alpha1.ConditionTypeRestReady,
	} {
		condition := meta.FindStatusCondition(status.Status.Conditions, conditionType)
		if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != "DeploymentPending" {
			t.Fatalf("%s condition = %#v, want DeploymentPending false", conditionType, condition)
		}
	}
}

func TestReconcileServicesReportsAllCoreDeploymentsReady(t *testing.T) {
	t.Parallel()

	project, reconciler := newCoreReadinessFixture(t, false)
	if err := reconciler.reconcileServices(context.Background(), project, &secretresources.ProjectCredentials{}); err != nil {
		t.Fatal(err)
	}
	if !coreServicesReady(project) {
		t.Fatalf("core services = %#v, want all ready", project.Status.Services)
	}
	for _, conditionType := range []string{
		supabasev1alpha1.ConditionTypeAuthReady,
		supabasev1alpha1.ConditionTypeRestReady,
		supabasev1alpha1.ConditionTypeStudioReady,
		supabasev1alpha1.ConditionTypeMetaReady,
		supabasev1alpha1.ConditionTypeGatewayReady,
	} {
		condition := meta.FindStatusCondition(project.Status.Conditions, conditionType)
		if condition == nil || condition.Status != metav1.ConditionTrue {
			t.Fatalf("%s condition = %#v, want true", conditionType, condition)
		}
	}
}

func TestReconcileReturnsRequeueUntilCoreDeploymentsAreReady(t *testing.T) {
	t.Parallel()

	project := &supabasev1alpha1.SupabaseProject{
		TypeMeta: metav1.TypeMeta{APIVersion: supabasev1alpha1.GroupVersion.String(), Kind: "SupabaseProject"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "aggregate", Namespace: "default", UID: "aggregate-project-uid", Generation: 7,
		},
		Spec: supabasev1alpha1.SupabaseProjectSpec{
			ProjectCredentialsSecret: "aggregate-credentials",
			Database: supabasev1alpha1.DatabaseSpec{
				Instances: 1,
				Storage:   cnpgv1.StorageConfiguration{Size: "1Gi"},
			},
			Auth: supabasev1alpha1.AuthSpec{SiteURL: "https://app.example.com", ExternalURL: "https://auth.example.com"},
		},
	}
	credentials := validProjectCredentialsSecret(t, project, project.Spec.ProjectCredentialsSecret)
	generated, names, err := secretresources.GenerateSecrets(project)
	if err != nil {
		t.Fatal(err)
	}
	project.Status.SecretNames = names
	cluster := cnpg.BuildCluster(project, &project.Status.SecretNames)
	cluster.Default()
	cluster.Status.ReadyInstances = cluster.Spec.Instances

	apiCredentials, err := secretresources.ValidateProjectCredentials(credentials)
	if err != nil {
		t.Fatal(err)
	}
	scheme := newIdempotencyTestScheme(t)
	coreDeployments := []*appsv1.Deployment{
		deployments.BuildAuthDeployment(project, &project.Status.SecretNames),
		deployments.BuildRestDeployment(project, &project.Status.SecretNames),
		deployments.BuildStudioDeployment(project, &project.Status.SecretNames),
		deployments.BuildMetaDeployment(project, &project.Status.SecretNames),
		deployments.BuildGatewayDeployment(project),
	}
	for i, deployment := range coreDeployments {
		if i != 3 {
			applyProjectCredentialsHash(deployment, apiCredentials.PodTemplateHash)
		}
		markCoreDeploymentStatus(t, deployment, 7, i != 1, 0)
		if err := setTestControllerReference(project, deployment, scheme); err != nil {
			t.Fatal(err)
		}
	}

	objects := []client.Object{project, credentials, cluster}
	for _, secret := range generated {
		objects = append(objects, secret)
	}
	for _, deployment := range coreDeployments {
		objects = append(objects, deployment)
	}
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project).WithObjects(objects...).Build(),
		Scheme: scheme,
	}
	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(project)})
	if err != nil {
		t.Fatal(err)
	}
	if result.RequeueAfter != RequeueDelay {
		t.Fatalf("requeueAfter = %s, want %s", result.RequeueAfter, RequeueDelay)
	}
	status := &supabasev1alpha1.SupabaseProject{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(project), status); err != nil {
		t.Fatal(err)
	}
	if status.Status.Phase != supabasev1alpha1.PhaseProvisioning || status.Status.ObservedGeneration != 0 {
		t.Fatalf("status = %#v, want Provisioning with old observedGeneration", status.Status)
	}
	if ready := meta.FindStatusCondition(status.Status.Conditions, supabasev1alpha1.ConditionTypeReady); ready == nil || ready.Status != metav1.ConditionFalse {
		t.Fatalf("Ready condition = %#v, want false", ready)
	}

	rest := &appsv1.Deployment{}
	if err := reconciler.Get(context.Background(), types.NamespacedName{Name: deployments.RestDeploymentName(project), Namespace: project.Namespace}, rest); err != nil {
		t.Fatal(err)
	}
	markCoreDeploymentStatus(t, rest, rest.Generation, true, 0)
	if err := reconciler.Status().Update(context.Background(), rest); err != nil {
		t.Fatal(err)
	}
	result, err = reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(project)})
	if err != nil {
		t.Fatal(err)
	}
	if result != (ctrl.Result{}) {
		t.Fatalf("ready reconcile result = %#v, want no requeue", result)
	}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(project), status); err != nil {
		t.Fatal(err)
	}
	if status.Status.Phase != supabasev1alpha1.PhaseRunning || status.Status.ObservedGeneration != project.Generation {
		t.Fatalf("final status = %#v, want Running at generation %d", status.Status, project.Generation)
	}
	ready := meta.FindStatusCondition(status.Status.Conditions, supabasev1alpha1.ConditionTypeReady)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		t.Fatalf("final Ready condition = %#v, want true", ready)
	}
}

func TestReconcileClearsAggregateReadinessOnCoreServiceFailure(t *testing.T) {
	t.Parallel()

	project := &supabasev1alpha1.SupabaseProject{
		TypeMeta: metav1.TypeMeta{APIVersion: supabasev1alpha1.GroupVersion.String(), Kind: "SupabaseProject"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "failure", Namespace: "default", UID: "failure-project-uid", Generation: 7,
		},
		Status: supabasev1alpha1.SupabaseProjectStatus{
			Phase:              supabasev1alpha1.PhaseRunning,
			ObservedGeneration: 7,
			Services: supabasev1alpha1.ServicesStatus{
				Auth:    supabasev1alpha1.ServiceStatus{Ready: true, AvailableReplicas: 1},
				Rest:    supabasev1alpha1.ServiceStatus{Ready: true, AvailableReplicas: 1},
				Studio:  supabasev1alpha1.ServiceStatus{Ready: true, AvailableReplicas: 1},
				Meta:    supabasev1alpha1.ServiceStatus{Ready: true, AvailableReplicas: 1},
				Gateway: supabasev1alpha1.ServiceStatus{Ready: true, AvailableReplicas: 1},
			},
		},
		Spec: supabasev1alpha1.SupabaseProjectSpec{
			ProjectCredentialsSecret: "failure-credentials",
			Database: supabasev1alpha1.DatabaseSpec{
				Instances: 1,
				Storage:   cnpgv1.StorageConfiguration{Size: "1Gi"},
			},
			Auth: supabasev1alpha1.AuthSpec{SiteURL: "https://app.example.com", ExternalURL: "https://auth.example.com"},
		},
	}
	for _, conditionType := range []string{
		supabasev1alpha1.ConditionTypeReady,
		supabasev1alpha1.ConditionTypeAuthReady,
		supabasev1alpha1.ConditionTypeRestReady,
		supabasev1alpha1.ConditionTypeStudioReady,
		supabasev1alpha1.ConditionTypeMetaReady,
		supabasev1alpha1.ConditionTypeGatewayReady,
	} {
		project.Status.Conditions = append(project.Status.Conditions, metav1.Condition{
			Type: conditionType, Status: metav1.ConditionTrue, ObservedGeneration: project.Generation,
			Reason: "Ready", Message: "component is running",
		})
	}

	credentials := validProjectCredentialsSecret(t, project, project.Spec.ProjectCredentialsSecret)
	generated, names, err := secretresources.GenerateSecrets(project)
	if err != nil {
		t.Fatal(err)
	}
	project.Status.SecretNames = names
	cluster := cnpg.BuildCluster(project, &project.Status.SecretNames)
	cluster.Default()
	cluster.Status.ReadyInstances = cluster.Spec.Instances
	apiCredentials, err := secretresources.ValidateProjectCredentials(credentials)
	if err != nil {
		t.Fatal(err)
	}
	scheme := newIdempotencyTestScheme(t)
	coreDeployments := []*appsv1.Deployment{
		deployments.BuildAuthDeployment(project, &project.Status.SecretNames),
		deployments.BuildRestDeployment(project, &project.Status.SecretNames),
		deployments.BuildStudioDeployment(project, &project.Status.SecretNames),
		deployments.BuildMetaDeployment(project, &project.Status.SecretNames),
		deployments.BuildGatewayDeployment(project),
	}
	for i, deployment := range coreDeployments {
		if i != 3 {
			applyProjectCredentialsHash(deployment, apiCredentials.PodTemplateHash)
		}
		markCoreDeploymentStatus(t, deployment, project.Generation, true, 0)
		if err := setTestControllerReference(project, deployment, scheme); err != nil {
			t.Fatal(err)
		}
	}
	objects := []client.Object{project, credentials, cluster}
	for _, secret := range generated {
		objects = append(objects, secret)
	}
	for _, deployment := range coreDeployments {
		objects = append(objects, deployment)
	}
	injected := errors.New("injected auth service create failure")
	baseClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project).WithObjects(objects...).Build()
	failingClient := &failCoreServiceCreateClient{
		Client:      baseClient,
		ServiceName: services.BuildAuthService(project).Name,
		Err:         injected,
	}
	reconciler := &SupabaseProjectReconciler{Client: failingClient, Scheme: scheme}

	_, err = reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(project)})
	if !errors.Is(err, injected) {
		t.Fatalf("reconcile error = %v, want injected service error", err)
	}
	status := &supabasev1alpha1.SupabaseProject{}
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(project), status); err != nil {
		t.Fatal(err)
	}
	if status.Status.Phase != supabasev1alpha1.PhaseProvisioning {
		t.Fatalf("phase = %q, want Provisioning", status.Status.Phase)
	}
	if status.Status.Services.Auth.Ready || status.Status.Services.Auth.AvailableReplicas != 0 {
		t.Fatalf("auth status = %#v, want cleared after service failure", status.Status.Services.Auth)
	}
	ready := meta.FindStatusCondition(status.Status.Conditions, supabasev1alpha1.ConditionTypeReady)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != "CoreServicesFailed" {
		t.Fatalf("Ready condition = %#v, want CoreServicesFailed false", ready)
	}
}

type failCoreServiceCreateClient struct {
	client.Client
	ServiceName string
	Err         error
}

func (c *failCoreServiceCreateClient) Create(ctx context.Context, object client.Object, options ...client.CreateOption) error {
	service, ok := object.(*corev1.Service)
	if ok && service.Name == c.ServiceName {
		return c.Err
	}
	return c.Client.Create(ctx, object, options...)
}

func newCoreReadinessFixture(t *testing.T, pendingRest bool) (*supabasev1alpha1.SupabaseProject, *SupabaseProjectReconciler) {
	t.Helper()
	project := &supabasev1alpha1.SupabaseProject{
		TypeMeta: metav1.TypeMeta{APIVersion: supabasev1alpha1.GroupVersion.String(), Kind: "SupabaseProject"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "readiness", Namespace: "default", UID: "readiness-project-uid", Generation: 7,
		},
		Status: supabasev1alpha1.SupabaseProjectStatus{
			Phase:              supabasev1alpha1.PhaseProvisioning,
			ObservedGeneration: 4,
		},
		Spec: supabasev1alpha1.SupabaseProjectSpec{
			Auth:    supabasev1alpha1.AuthSpec{Replicas: 2},
			Rest:    supabasev1alpha1.RestSpec{Replicas: 2},
			Studio:  supabasev1alpha1.StudioSpec{Replicas: 1},
			Meta:    supabasev1alpha1.MetaSpec{Replicas: 1},
			Gateway: supabasev1alpha1.GatewaySpec{Replicas: 1},
		},
	}
	secretNames := &project.Status.SecretNames
	desired := []*appsv1.Deployment{
		deployments.BuildAuthDeployment(project, secretNames),
		deployments.BuildRestDeployment(project, secretNames),
		deployments.BuildStudioDeployment(project, secretNames),
		deployments.BuildMetaDeployment(project, secretNames),
		deployments.BuildGatewayDeployment(project),
	}
	scheme := newPowerSyncTestScheme(t)
	objects := []client.Object{project}
	for i, deployment := range desired {
		markCoreDeploymentStatus(t, deployment, 7, !pendingRest || i != 1, 1)
		if err := setTestControllerReference(project, deployment, scheme); err != nil {
			t.Fatal(err)
		}
		objects = append(objects, deployment)
	}
	reconciler := &SupabaseProjectReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project).WithObjects(objects...).Build(),
		Scheme: scheme,
	}
	return project, reconciler
}

func markCoreDeploymentStatus(t *testing.T, desired *appsv1.Deployment, generation int64, ready bool, available int32) {
	t.Helper()
	if desired.Spec.Replicas == nil {
		desired.Spec.Replicas = ptr.To(int32(1))
	}
	desired.Generation = generation
	desired.Status.ObservedGeneration = generation
	desired.Status.AvailableReplicas = available
	if ready {
		desired.Status.UpdatedReplicas = *desired.Spec.Replicas
		desired.Status.ReadyReplicas = *desired.Spec.Replicas
		desired.Status.AvailableReplicas = *desired.Spec.Replicas
		desired.Status.UnavailableReplicas = 0
		return
	}
	desired.Status.UpdatedReplicas = *desired.Spec.Replicas
	desired.Status.ReadyReplicas = *desired.Spec.Replicas
	desired.Status.UnavailableReplicas = *desired.Spec.Replicas - available
}
