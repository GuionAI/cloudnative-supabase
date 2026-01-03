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

package controller

import (
	"context"
	"fmt"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/cnpg"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/configmaps"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/deployments"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/secrets"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/services"
)

const (
	// RequeueDelay is the delay before requeueing when waiting for resources
	RequeueDelay = 10 * time.Second
)

// serviceReconcileConfig holds configuration for reconciling a service component
type serviceReconcileConfig struct {
	name            string
	conditionType   string
	buildDeployment func() *appsv1.Deployment
	buildService    func() *corev1.Service
	setStatus       func(ready bool)
	logFields       []any
}

// SupabaseProjectReconciler reconciles a SupabaseProject object
type SupabaseProjectReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=supabase.guion.dev,resources=supabaseprojects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=supabase.guion.dev,resources=supabaseprojects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=supabase.guion.dev,resources=supabaseprojects/finalizers,verbs=update
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *SupabaseProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the SupabaseProject instance
	project := &supabasev1alpha1.SupabaseProject{}
	if err := r.Get(ctx, req.NamespacedName, project); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("SupabaseProject resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log.Info("Reconciling SupabaseProject", "phase", project.Status.Phase)

	// Initialize status if needed
	if project.Status.Phase == "" {
		project.Status.Phase = supabasev1alpha1.PhasePending
	}

	// Phase 1: Ensure secrets exist
	if err := r.reconcileSecrets(ctx, project); err != nil {
		return ctrl.Result{}, err
	}

	// Phase 2: Ensure init SQL ConfigMap exists
	if err := r.reconcileInitSQL(ctx, project); err != nil {
		return ctrl.Result{}, err
	}

	// Phase 3: Ensure CNPG Cluster exists
	if result, err := r.reconcileCNPGCluster(ctx, project); err != nil || result.RequeueAfter > 0 {
		return result, err
	}

	// Phase 4: Wait for database to be ready
	if result, err := r.waitForDatabase(ctx, project); err != nil || result.RequeueAfter > 0 {
		return result, err
	}

	// Phase 5: Deploy Supabase services
	if err := r.reconcileServices(ctx, project); err != nil {
		return ctrl.Result{}, err
	}

	// All phases complete
	project.Status.Phase = supabasev1alpha1.PhaseRunning
	project.Status.ObservedGeneration = project.Generation
	r.setCondition(project, supabasev1alpha1.ConditionTypeReady, metav1.ConditionTrue, "AllComponentsReady", "All Supabase components are running")

	if err := r.Status().Update(ctx, project); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Reconciliation complete")
	return ctrl.Result{}, nil
}

// reconcileSecrets ensures all required secrets exist
func (r *SupabaseProjectReconciler) reconcileSecrets(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	log := logf.FromContext(ctx)
	log.Info("Reconciling secrets")

	project.Status.Phase = supabasev1alpha1.PhaseProvisioning

	// Check if user-specified secrets mode is enabled
	if project.Spec.Secrets != nil && !project.Spec.Secrets.AutoGenerate {
		return r.reconcileUserSpecifiedSecrets(ctx, project)
	}

	// Auto-generate mode: check if secrets already exist in the cluster
	return r.reconcileAutoGeneratedSecrets(ctx, project)
}

// reconcileUserSpecifiedSecrets validates and uses user-provided secrets
func (r *SupabaseProjectReconciler) reconcileUserSpecifiedSecrets(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	log := logf.FromContext(ctx)
	log.Info("Using user-specified secrets")

	secretNames := secrets.GetSecretNamesFromSpec(project.Spec.Secrets)

	// Validate JWT secret exists and has required keys
	jwtSecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: secretNames.JWT, Namespace: project.Namespace}, jwtSecret); err != nil {
		if apierrors.IsNotFound(err) {
			r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionFalse, "SecretNotFound",
				fmt.Sprintf("JWT secret %s not found", secretNames.JWT))
			if statusErr := r.Status().Update(ctx, project); statusErr != nil {
				return statusErr
			}
			return fmt.Errorf("JWT secret %s not found", secretNames.JWT)
		}
		return err
	}
	if err := secrets.ValidateJWTSecret(jwtSecret); err != nil {
		r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionFalse, "InvalidSecret", err.Error())
		if statusErr := r.Status().Update(ctx, project); statusErr != nil {
			return statusErr
		}
		return err
	}

	// Validate role secrets exist and have required keys
	roleSecrets := map[string]string{
		secretNames.SupabaseAdmin: "supabase_admin",
		secretNames.Authenticator: "authenticator",
		secretNames.AuthAdmin:     "supabase_auth_admin",
	}

	for secretName, roleName := range roleSecrets {
		secret := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: project.Namespace}, secret); err != nil {
			if apierrors.IsNotFound(err) {
				r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionFalse, "SecretNotFound",
					fmt.Sprintf("Secret %s for role %s not found", secretName, roleName))
				if statusErr := r.Status().Update(ctx, project); statusErr != nil {
					return statusErr
				}
				return fmt.Errorf("secret %s for role %s not found", secretName, roleName)
			}
			return err
		}
		if err := secrets.ValidateRoleSecret(secret, secretName); err != nil {
			r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionFalse, "InvalidSecret", err.Error())
			if statusErr := r.Status().Update(ctx, project); statusErr != nil {
				return statusErr
			}
			return err
		}
	}

	// All secrets validated successfully
	project.Status.SecretNames = secretNames
	r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionTrue, "SecretsValidated", "All user-specified secrets are valid")
	if err := r.Status().Update(ctx, project); err != nil {
		return err
	}

	log.Info("User-specified secrets validated successfully")
	return nil
}

// reconcileAutoGeneratedSecrets handles the auto-generation mode
func (r *SupabaseProjectReconciler) reconcileAutoGeneratedSecrets(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	log := logf.FromContext(ctx)

	// Check if secrets already exist in the cluster (not just in status)
	// This prevents regenerating secrets if the operator restarts after creating
	// secrets but before updating status
	jwtSecretName := project.Name + "-jwt"
	// Support legacy spec.jwt.secretRef for backwards compatibility
	if project.Spec.JWT != nil && project.Spec.JWT.SecretRef != "" {
		jwtSecretName = project.Spec.JWT.SecretRef
	}

	existingJWTSecret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: jwtSecretName, Namespace: project.Namespace}, existingJWTSecret)
	if err == nil {
		// JWT secret exists - check if all other secrets exist too
		secretNames := supabasev1alpha1.SecretNamesStatus{
			JWT:           jwtSecretName,
			SupabaseAdmin: project.Name + "-supabase-admin-password",
			Authenticator: project.Name + "-authenticator-password",
			AuthAdmin:     project.Name + "-auth-admin-password",
		}

		allExist := true
		for _, name := range []string{secretNames.SupabaseAdmin, secretNames.Authenticator, secretNames.AuthAdmin} {
			existing := &corev1.Secret{}
			if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: project.Namespace}, existing); err != nil {
				if apierrors.IsNotFound(err) {
					allExist = false
					break
				}
				return err
			}
		}

		if allExist {
			log.Info("Secrets already exist in cluster, syncing status")
			project.Status.SecretNames = secretNames
			r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionTrue, "SecretsExist", "All secrets exist")
			if err := r.Status().Update(ctx, project); err != nil {
				return err
			}
			return nil
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Secrets don't exist - generate them
	log.Info("Generating new secrets")
	generatedSecrets, secretNames, err := secrets.GenerateSecrets(project)
	if err != nil {
		r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionFalse, "GenerationFailed", err.Error())
		if statusErr := r.Status().Update(ctx, project); statusErr != nil {
			return statusErr
		}
		return err
	}

	// Create or update each secret
	for _, secret := range generatedSecrets {
		if err := r.createOrUpdateSecret(ctx, project, secret); err != nil {
			r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionFalse, "CreateFailed", err.Error())
			if statusErr := r.Status().Update(ctx, project); statusErr != nil {
				return statusErr
			}
			return err
		}
	}

	// Update status with secret names
	project.Status.SecretNames = secretNames

	r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionTrue, "SecretsCreated", "All secrets have been created")
	if err := r.Status().Update(ctx, project); err != nil {
		return err
	}

	return nil
}

// reconcileInitSQL ensures the init SQL ConfigMap exists
func (r *SupabaseProjectReconciler) reconcileInitSQL(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	log := logf.FromContext(ctx)
	log.Info("Reconciling init SQL ConfigMap")

	configMap := configmaps.BuildInitSQLConfigMap(project)
	return r.createOrUpdateConfigMap(ctx, project, configMap)
}

// reconcileCNPGCluster ensures the CNPG Cluster exists
func (r *SupabaseProjectReconciler) reconcileCNPGCluster(ctx context.Context, project *supabasev1alpha1.SupabaseProject) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling CNPG Cluster")

	cluster := cnpg.BuildCluster(project, &project.Status.SecretNames)

	// Check if cluster exists
	existing := &cnpgv1.Cluster{}
	err := r.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Set owner reference
			if err := controllerutil.SetControllerReference(project, cluster, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}

			log.Info("Creating CNPG Cluster", "name", cluster.Name)
			if err := r.Create(ctx, cluster); err != nil {
				r.setCondition(project, supabasev1alpha1.ConditionTypeDatabaseReady, metav1.ConditionFalse, "CreateFailed", err.Error())
				if statusErr := r.Status().Update(ctx, project); statusErr != nil {
					return ctrl.Result{}, statusErr
				}
				return ctrl.Result{}, err
			}

			r.setCondition(project, supabasev1alpha1.ConditionTypeDatabaseReady, metav1.ConditionFalse, "Creating", "CNPG Cluster is being created")
			if err := r.Status().Update(ctx, project); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{RequeueAfter: RequeueDelay}, nil
		}
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// waitForDatabase waits for the CNPG Cluster to be ready
func (r *SupabaseProjectReconciler) waitForDatabase(ctx context.Context, project *supabasev1alpha1.SupabaseProject) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Checking database status")

	cluster := &cnpgv1.Cluster{}
	clusterName := cnpg.ClusterName(project)
	err := r.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: project.Namespace}, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Check if cluster is ready
	if cluster.Status.ReadyInstances < cluster.Spec.Instances {
		log.Info("Waiting for database to be ready",
			"ready", cluster.Status.ReadyInstances,
			"expected", cluster.Spec.Instances)

		r.setCondition(project, supabasev1alpha1.ConditionTypeDatabaseReady, metav1.ConditionFalse, "WaitingForInstances",
			fmt.Sprintf("Waiting for database instances: %d/%d ready", cluster.Status.ReadyInstances, cluster.Spec.Instances))
		if err := r.Status().Update(ctx, project); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: RequeueDelay}, nil
	}

	// Update database status
	project.Status.Database = supabasev1alpha1.DatabaseStatus{
		Ready:          true,
		Phase:          cluster.Status.Phase,
		ReadyInstances: int32(cluster.Status.ReadyInstances),
		PrimaryHost:    cluster.Status.CurrentPrimary,
	}

	// Update endpoints
	project.Status.Endpoints = supabasev1alpha1.EndpointsStatus{
		Database: fmt.Sprintf("%s:5432", cnpg.ClusterRWServiceName(project)),
	}

	r.setCondition(project, supabasev1alpha1.ConditionTypeDatabaseReady, metav1.ConditionTrue, "Ready", "Database is ready")
	if err := r.Status().Update(ctx, project); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcileServices deploys all Supabase services
func (r *SupabaseProjectReconciler) reconcileServices(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	log := logf.FromContext(ctx)
	log.Info("Reconciling Supabase services")

	secretNames := &project.Status.SecretNames

	// Deploy Auth (GoTrue)
	if err := r.reconcileAuth(ctx, project, secretNames); err != nil {
		return err
	}

	// Deploy REST (PostgREST)
	if err := r.reconcileRest(ctx, project, secretNames); err != nil {
		return err
	}

	// Deploy Studio
	if err := r.reconcileStudio(ctx, project, secretNames); err != nil {
		return err
	}

	// Deploy Meta (postgres-meta)
	if err := r.reconcileMeta(ctx, project, secretNames); err != nil {
		return err
	}

	// Deploy Kong
	if err := r.reconcileKong(ctx, project, secretNames); err != nil {
		return err
	}

	return nil
}

// reconcileServiceComponent is a generic helper for reconciling a service component (deployment + service)
func (r *SupabaseProjectReconciler) reconcileServiceComponent(ctx context.Context, project *supabasev1alpha1.SupabaseProject, config serviceReconcileConfig) error {
	log := logf.FromContext(ctx)
	log.Info(fmt.Sprintf("Reconciling %s service", config.name))

	// Create deployment
	deployment := config.buildDeployment()
	log.V(1).Info(fmt.Sprintf("Built %s deployment", config.name), config.logFields...)
	if err := r.createOrUpdateDeployment(ctx, project, deployment); err != nil {
		r.setCondition(project, config.conditionType, metav1.ConditionFalse, "DeploymentFailed", err.Error())
		if statusErr := r.Status().Update(ctx, project); statusErr != nil {
			return statusErr
		}
		return err
	}

	// Create service
	service := config.buildService()
	if err := r.createOrUpdateService(ctx, project, service); err != nil {
		r.setCondition(project, config.conditionType, metav1.ConditionFalse, "ServiceFailed", err.Error())
		if statusErr := r.Status().Update(ctx, project); statusErr != nil {
			return statusErr
		}
		return err
	}

	config.setStatus(true)
	r.setCondition(project, config.conditionType, metav1.ConditionTrue, "Ready", fmt.Sprintf("%s service is running", config.name))
	return nil
}

// reconcileAuth deploys the Auth service
func (r *SupabaseProjectReconciler) reconcileAuth(ctx context.Context, project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) error {
	return r.reconcileServiceComponent(ctx, project, serviceReconcileConfig{
		name:          "Auth",
		conditionType: supabasev1alpha1.ConditionTypeAuthReady,
		buildDeployment: func() *appsv1.Deployment {
			return deployments.BuildAuthDeployment(project, secretNames)
		},
		buildService: func() *corev1.Service {
			return services.BuildAuthService(project)
		},
		setStatus: func(ready bool) {
			project.Status.Services.Auth = supabasev1alpha1.ServiceStatus{Ready: ready}
		},
		logFields: []any{
			"image", fmt.Sprintf("%s:%s", "supabase/gotrue", project.Spec.Auth.ImageTag),
			"replicas", project.Spec.Auth.Replicas,
			"hasProviders", project.Spec.Auth.Providers != nil,
			"hasEmailHook", project.Spec.Auth.EmailHook != nil && project.Spec.Auth.EmailHook.Enabled,
		},
	})
}

// reconcileRest deploys the REST service
func (r *SupabaseProjectReconciler) reconcileRest(ctx context.Context, project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) error {
	return r.reconcileServiceComponent(ctx, project, serviceReconcileConfig{
		name:          "REST",
		conditionType: supabasev1alpha1.ConditionTypeRestReady,
		buildDeployment: func() *appsv1.Deployment {
			return deployments.BuildRestDeployment(project, secretNames)
		},
		buildService: func() *corev1.Service {
			return services.BuildRestService(project)
		},
		setStatus: func(ready bool) {
			project.Status.Services.Rest = supabasev1alpha1.ServiceStatus{Ready: ready}
		},
		logFields: []any{
			"image", fmt.Sprintf("%s:%s", "postgrest/postgrest", project.Spec.Rest.ImageTag),
			"schemas", project.Spec.Rest.Schemas,
		},
	})
}

// reconcileStudio deploys the Studio service
func (r *SupabaseProjectReconciler) reconcileStudio(ctx context.Context, project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) error {
	return r.reconcileServiceComponent(ctx, project, serviceReconcileConfig{
		name:          "Studio",
		conditionType: supabasev1alpha1.ConditionTypeStudioReady,
		buildDeployment: func() *appsv1.Deployment {
			return deployments.BuildStudioDeployment(project, secretNames)
		},
		buildService: func() *corev1.Service {
			return services.BuildStudioService(project)
		},
		setStatus: func(ready bool) {
			project.Status.Services.Studio = supabasev1alpha1.ServiceStatus{Ready: ready}
		},
		logFields: []any{
			"image", fmt.Sprintf("%s:%s", "supabase/studio", project.Spec.Studio.ImageTag),
			"publicURL", project.Spec.Studio.PublicURL,
		},
	})
}

// reconcileMeta deploys the Meta service
func (r *SupabaseProjectReconciler) reconcileMeta(ctx context.Context, project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) error {
	return r.reconcileServiceComponent(ctx, project, serviceReconcileConfig{
		name:          "Meta",
		conditionType: supabasev1alpha1.ConditionTypeMetaReady,
		buildDeployment: func() *appsv1.Deployment {
			return deployments.BuildMetaDeployment(project, secretNames)
		},
		buildService: func() *corev1.Service {
			return services.BuildMetaService(project)
		},
		setStatus: func(ready bool) {
			project.Status.Services.Meta = supabasev1alpha1.ServiceStatus{Ready: ready}
		},
		logFields: []any{
			"image", fmt.Sprintf("%s:%s", "supabase/postgres-meta", project.Spec.Meta.ImageTag),
		},
	})
}

// reconcileKong deploys the Kong API gateway
func (r *SupabaseProjectReconciler) reconcileKong(ctx context.Context, project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) error {
	log := logf.FromContext(ctx)
	log.Info("Reconciling Kong service")

	// Create Kong config ConfigMap
	kongConfig := configmaps.BuildKongConfigMap(project)
	log.V(1).Info("Built Kong ConfigMap", "name", kongConfig.Name)
	if err := r.createOrUpdateConfigMap(ctx, project, kongConfig); err != nil {
		r.setCondition(project, supabasev1alpha1.ConditionTypeKongReady, metav1.ConditionFalse, "ConfigMapFailed", err.Error())
		if statusErr := r.Status().Update(ctx, project); statusErr != nil {
			return statusErr
		}
		return err
	}

	// Create deployment
	deployment := deployments.BuildKongDeployment(project, secretNames)
	log.V(1).Info("Built Kong deployment",
		"image", fmt.Sprintf("%s:%s", "kong", project.Spec.Kong.ImageTag))
	if err := r.createOrUpdateDeployment(ctx, project, deployment); err != nil {
		r.setCondition(project, supabasev1alpha1.ConditionTypeKongReady, metav1.ConditionFalse, "DeploymentFailed", err.Error())
		if statusErr := r.Status().Update(ctx, project); statusErr != nil {
			return statusErr
		}
		return err
	}

	// Create service
	service := services.BuildKongService(project)
	if err := r.createOrUpdateService(ctx, project, service); err != nil {
		r.setCondition(project, supabasev1alpha1.ConditionTypeKongReady, metav1.ConditionFalse, "ServiceFailed", err.Error())
		if statusErr := r.Status().Update(ctx, project); statusErr != nil {
			return statusErr
		}
		return err
	}

	// Update API endpoint in status
	project.Status.Endpoints.API = fmt.Sprintf("%s-kong:8000", project.Name)

	project.Status.Services.Kong = supabasev1alpha1.ServiceStatus{Ready: true}
	r.setCondition(project, supabasev1alpha1.ConditionTypeKongReady, metav1.ConditionTrue, "Ready", "Kong gateway is running")
	return nil
}

// Helper methods

func (r *SupabaseProjectReconciler) createOrUpdateSecret(ctx context.Context, project *supabasev1alpha1.SupabaseProject, secret *corev1.Secret) error {
	// Set owner reference
	if err := controllerutil.SetControllerReference(project, secret, r.Scheme); err != nil {
		return err
	}

	// Check if exists
	existing := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return r.Create(ctx, secret)
		}
		return err
	}

	// Secret exists - don't overwrite data (preserve generated passwords)
	return nil
}

func (r *SupabaseProjectReconciler) createOrUpdateConfigMap(ctx context.Context, project *supabasev1alpha1.SupabaseProject, configMap *corev1.ConfigMap) error {
	// Set owner reference
	if err := controllerutil.SetControllerReference(project, configMap, r.Scheme); err != nil {
		return err
	}

	// Check if exists
	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: configMap.Name, Namespace: configMap.Namespace}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return r.Create(ctx, configMap)
		}
		return err
	}

	// Update existing
	existing.Data = configMap.Data
	return r.Update(ctx, existing)
}

func (r *SupabaseProjectReconciler) createOrUpdateDeployment(ctx context.Context, project *supabasev1alpha1.SupabaseProject, deployment *appsv1.Deployment) error {
	log := logf.FromContext(ctx)
	if deployment == nil {
		log.V(1).Info("Skipping nil deployment - service may be disabled")
		return nil
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(project, deployment, r.Scheme); err != nil {
		return err
	}

	// Check if exists
	existing := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("Creating deployment", "name", deployment.Name)
			return r.Create(ctx, deployment)
		}
		return err
	}

	// Update existing - only update operator-owned fields
	log.V(1).Info("Updating deployment", "name", deployment.Name)
	existing.Spec.Replicas = deployment.Spec.Replicas
	existing.Spec.Template = deployment.Spec.Template
	return r.Update(ctx, existing)
}

func (r *SupabaseProjectReconciler) createOrUpdateService(ctx context.Context, project *supabasev1alpha1.SupabaseProject, service *corev1.Service) error {
	if service == nil {
		logf.FromContext(ctx).V(1).Info("Skipping nil service - service may be disabled")
		return nil
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(project, service, r.Scheme); err != nil {
		return err
	}

	// Check if exists
	existing := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return r.Create(ctx, service)
		}
		return err
	}

	// Update existing (preserve ClusterIP)
	existing.Spec.Ports = service.Spec.Ports
	existing.Spec.Selector = service.Spec.Selector
	return r.Update(ctx, existing)
}

func (r *SupabaseProjectReconciler) setCondition(project *supabasev1alpha1.SupabaseProject, conditionType string, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	meta.SetStatusCondition(&project.Status.Conditions, condition)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SupabaseProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&supabasev1alpha1.SupabaseProject{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.Deployment{}).
		Owns(&cnpgv1.Cluster{}).
		Named("supabaseproject").
		Complete(r)
}
