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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	barmancloudv1 "github.com/cloudnative-pg/plugin-barman-cloud/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/cnpg"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/configmaps"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/deployments"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/jobs"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/secrets"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/services"
)

const (
	// RequeueDelay is the delay before requeueing when waiting for resources
	RequeueDelay = 10 * time.Second
)

// serviceReconcileConfig holds configuration for reconciling a service component.
type serviceReconcileConfig struct {
	name            string
	conditionType   string
	buildDeployment func() *appsv1.Deployment
	buildService    func() *corev1.Service
	setStatus       func(ready bool)
	logFields       []any // optional
}

// newServiceReconcileConfig creates a serviceReconcileConfig with required fields as parameters.
// This ensures callers cannot forget to provide required values (compile-time enforcement).
func newServiceReconcileConfig(
	name string,
	conditionType string,
	buildDeployment func() *appsv1.Deployment,
	buildService func() *corev1.Service,
	setStatus func(ready bool),
) serviceReconcileConfig {
	return serviceReconcileConfig{
		name:            name,
		conditionType:   conditionType,
		buildDeployment: buildDeployment,
		buildService:    buildService,
		setStatus:       setStatus,
	}
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
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=scheduledbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=publications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=barmancloud.cnpg.io,resources=objectstores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete

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

	// Phase 2.5: Ensure backup infrastructure exists (ObjectStore, ScheduledBackup)
	// This must happen BEFORE cluster creation because the Cluster references the ObjectStore
	if err := r.reconcileBackupInfrastructure(ctx, project); err != nil {
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

	// Phase 6: PowerSync (after core services)
	if project.Spec.Powersync != nil {
		syncRules, err := r.reconcilePowerSyncRulesValidation(ctx, project)
		if err != nil {
			return ctrl.Result{}, err
		}
		if result, err := r.waitForPowerSyncManagedRoles(ctx, project); err != nil || result.RequeueAfter > 0 {
			return result, err
		}
		if result, err := r.reconcilePowerSyncPublication(ctx, project); err != nil || result.RequeueAfter > 0 {
			return result, err
		}
		if result, err := r.reconcileCDCPermissions(ctx, project); err != nil || result.RequeueAfter > 0 {
			return result, err
		}
		if result, err := r.reconcilePowersync(ctx, project, syncRules); err != nil || result.RequeueAfter > 0 {
			return result, err
		}
	} else if err := r.cleanupPowerSync(ctx, project); err != nil {
		return ctrl.Result{}, err
	}

	// All phases complete
	project.Status.Phase = supabasev1alpha1.PhaseRunning
	project.Status.ObservedGeneration = project.Generation
	r.setCondition(project, supabasev1alpha1.ConditionTypeReady, metav1.ConditionTrue, "AllComponentsReady", "All Supabase components are running")

	if err := r.updateProjectStatus(ctx, project); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Reconciliation complete")
	return ctrl.Result{}, nil
}

func (r *SupabaseProjectReconciler) reconcilePowerSyncRulesValidation(ctx context.Context, project *supabasev1alpha1.SupabaseProject) ([]byte, error) {
	rules, err := r.loadAndValidatePowerSyncRules(ctx, project)
	if err == nil {
		return rules, nil
	}
	r.setCondition(project, supabasev1alpha1.ConditionTypePowersyncReady, metav1.ConditionFalse, "InvalidSyncRules", err.Error())
	if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
		return nil, statusErr
	}
	return nil, err
}

func (r *SupabaseProjectReconciler) loadAndValidatePowerSyncRules(ctx context.Context, project *supabasev1alpha1.SupabaseProject) ([]byte, error) {
	rules := []byte(project.Spec.Powersync.SyncRules.Inline)
	if ref := project.Spec.Powersync.SyncRules.ConfigMapRef; ref != "" {
		configMap := &corev1.ConfigMap{}
		if err := r.Get(ctx, types.NamespacedName{Name: ref, Namespace: project.Namespace}, configMap); err != nil {
			return nil, fmt.Errorf("getting PowerSync sync rules ConfigMap %s: %w", ref, err)
		}
		content, ok := configMap.Data["sync_rules.yaml"]
		if !ok || content == "" {
			return nil, fmt.Errorf("PowerSync sync rules ConfigMap %s must contain non-empty sync_rules.yaml", ref)
		}
		rules = []byte(content)
	}

	var document struct {
		Config struct {
			Edition int `json:"edition"`
		} `json:"config"`
		Streams map[string]any `json:"streams"`
	}
	if err := yaml.Unmarshal(rules, &document); err != nil {
		return nil, fmt.Errorf("parsing PowerSync sync_rules.yaml: %w", err)
	}
	if document.Config.Edition != 3 {
		return nil, fmt.Errorf("PowerSync sync_rules.yaml requires config.edition: 3")
	}
	if document.Streams == nil {
		return nil, fmt.Errorf("PowerSync sync_rules.yaml requires streams")
	}
	return rules, nil
}

func (r *SupabaseProjectReconciler) waitForPowerSyncManagedRoles(ctx context.Context, project *supabasev1alpha1.SupabaseProject) (ctrl.Result, error) {
	cluster := &cnpgv1.Cluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: cnpg.ClusterName(project), Namespace: project.Namespace}, cluster); err != nil {
		return ctrl.Result{}, err
	}
	ready, err := powerSyncManagedRolesReady(cluster)
	if err != nil {
		r.setCondition(project, supabasev1alpha1.ConditionTypeCDCReady, metav1.ConditionFalse, "ManagedRolesFailed", err.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, err
	}
	if !ready {
		r.setCondition(project, supabasev1alpha1.ConditionTypeCDCReady, metav1.ConditionFalse, "ManagedRolesPending", "Waiting for CloudNativePG to reconcile PowerSync roles")
		if err := r.updateProjectStatus(ctx, project); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: RequeueDelay}, nil
	}
	return ctrl.Result{}, nil
}

func powerSyncManagedRolesReady(cluster *cnpgv1.Cluster) (bool, error) {
	for _, role := range []string{"powersync_storage", "powersync_replication"} {
		if reasons := cluster.Status.ManagedRolesStatus.CannotReconcile[role]; len(reasons) > 0 {
			return false, fmt.Errorf("CloudNativePG cannot reconcile role %s: %v", role, reasons)
		}
	}
	reconciled := make(map[string]struct{})
	for _, role := range cluster.Status.ManagedRolesStatus.ByStatus[cnpgv1.RoleStatusReconciled] {
		reconciled[role] = struct{}{}
	}
	_, storageReady := reconciled["powersync_storage"]
	_, replicationReady := reconciled["powersync_replication"]
	return storageReady && replicationReady, nil
}

// reconcileSecrets ensures all required secrets exist
func (r *SupabaseProjectReconciler) reconcileSecrets(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	log := logf.FromContext(ctx)
	log.Info("Reconciling secrets")

	if project.Status.Phase == supabasev1alpha1.PhasePending {
		project.Status.Phase = supabasev1alpha1.PhaseProvisioning
	}

	// Check if user-specified secrets mode is enabled
	if project.Spec.Secrets != nil && !project.Spec.Secrets.AutoGenerate {
		if err := r.reconcileUserSpecifiedSecrets(ctx, project); err != nil {
			return err
		}
	} else {
		// Auto-generate mode: check if secrets already exist in the cluster.
		if err := r.reconcileAutoGeneratedSecrets(ctx, project); err != nil {
			return err
		}
	}

	return r.reconcileEmailHookSecret(ctx, project)
}

func (r *SupabaseProjectReconciler) reconcileEmailHookSecret(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	hook := project.Spec.Auth.EmailHook
	if hook == nil || !hook.Enabled {
		if project.Status.SecretNames.EmailHook == "" {
			return nil
		}
		project.Status.SecretNames.EmailHook = ""
		return r.updateProjectStatus(ctx, project)
	}

	generated, name, err := secrets.GenerateEmailHookSecret(project)
	if err != nil {
		return err
	}

	existing := &corev1.Secret{}
	err = r.Get(ctx, types.NamespacedName{Name: name, Namespace: project.Namespace}, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		if err := r.createOrUpdateSecret(ctx, project, generated); err != nil {
			return err
		}
	} else if err := secrets.ValidateEmailHookSecret(existing); err != nil {
		return err
	}

	project.Status.SecretNames.EmailHook = name
	return r.updateProjectStatus(ctx, project)
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
			if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
				return statusErr
			}
			return fmt.Errorf("JWT secret %s not found", secretNames.JWT)
		}
		return err
	}
	if err := secrets.ValidateJWTSecret(jwtSecret); err != nil {
		r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionFalse, "InvalidSecret", err.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
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
	if project.Spec.Powersync != nil {
		roleSecrets[secretNames.PowersyncStoragePassword] = "powersync_storage"
		roleSecrets[secretNames.PowersyncReplicationPassword] = "powersync_replication"
	}

	for secretName, roleName := range roleSecrets {
		secret := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: project.Namespace}, secret); err != nil {
			if apierrors.IsNotFound(err) {
				r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionFalse, "SecretNotFound",
					fmt.Sprintf("Secret %s for role %s not found", secretName, roleName))
				if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
					return statusErr
				}
				return fmt.Errorf("secret %s for role %s not found", secretName, roleName)
			}
			return err
		}
		if err := secrets.ValidateRoleSecret(secret, secretName); err != nil {
			r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionFalse, "InvalidSecret", err.Error())
			if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
				return statusErr
			}
			return err
		}
	}

	// All secrets validated successfully
	project.Status.SecretNames = secretNames
	r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionTrue, "SecretsValidated", "All user-specified secrets are valid")
	if err := r.updateProjectStatus(ctx, project); err != nil {
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

			// Also sync optional service secrets
			if project.Spec.Powersync != nil {
				if err := r.reconcilePowersyncSecrets(ctx, project, &secretNames); err != nil {
					return err
				}
			}

			project.Status.SecretNames = secretNames
			r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionTrue, "SecretsExist", "All secrets exist")
			if err := r.updateProjectStatus(ctx, project); err != nil {
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
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return statusErr
		}
		return err
	}

	// Create or update each secret
	for _, secret := range generatedSecrets {
		if err := r.createOrUpdateSecret(ctx, project, secret); err != nil {
			r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionFalse, "CreateFailed", err.Error())
			if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
				return statusErr
			}
			return err
		}
	}

	// Generate Powersync secrets if Powersync is enabled
	if project.Spec.Powersync != nil {
		if err := r.reconcilePowersyncSecrets(ctx, project, &secretNames); err != nil {
			return err
		}
	}

	// Update status with secret names
	project.Status.SecretNames = secretNames

	r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionTrue, "SecretsCreated", "All secrets have been created")
	if err := r.updateProjectStatus(ctx, project); err != nil {
		return err
	}

	return nil
}

// reconcilePowersyncSecrets generates Powersync-related secrets if they don't exist
func (r *SupabaseProjectReconciler) reconcilePowersyncSecrets(ctx context.Context, project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) error {
	log := logf.FromContext(ctx)

	storagePwdName, replPwdName := secrets.PowersyncSecretNames(project)

	allExist := true
	for _, name := range []string{storagePwdName, replPwdName} {
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
		log.Info("Powersync secrets already exist, syncing status")
		secretNames.PowersyncStoragePassword = storagePwdName
		secretNames.PowersyncReplicationPassword = replPwdName
		return nil
	}

	// Generate Powersync secrets
	log.Info("Generating Powersync secrets")
	psSecrets, err := secrets.GeneratePowersyncSecrets(project)
	if err != nil {
		r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionFalse, "PowersyncSecretsFailed", err.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return statusErr
		}
		return err
	}

	for _, secret := range psSecrets {
		if err := r.createOrUpdateSecret(ctx, project, secret); err != nil {
			r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionFalse, "CreateFailed", err.Error())
			if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
				return statusErr
			}
			return err
		}
	}

	secretNames.PowersyncStoragePassword = storagePwdName
	secretNames.PowersyncReplicationPassword = replPwdName
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
				if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
					return ctrl.Result{}, statusErr
				}
				return ctrl.Result{}, err
			}

			r.setCondition(project, supabasev1alpha1.ConditionTypeDatabaseReady, metav1.ConditionFalse, "Creating", "CNPG Cluster is being created")
			if err := r.updateProjectStatus(ctx, project); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{RequeueAfter: RequeueDelay}, nil
		}
		return ctrl.Result{}, err
	}

	// Optional PowerSync roles may be added after the cluster already exists.
	if syncManagedRoles(existing, cluster) {
		if err := r.Update(ctx, existing); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating CNPG managed roles: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

func syncManagedRoles(existing, desired *cnpgv1.Cluster) bool {
	if existing.Spec.Managed == nil {
		existing.Spec.Managed = &cnpgv1.ManagedConfiguration{}
	}
	if apiequality.Semantic.DeepEqual(existing.Spec.Managed.Roles, desired.Spec.Managed.Roles) {
		return false
	}
	existing.Spec.Managed.Roles = desired.Spec.Managed.Roles
	return true
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
		if err := r.updateProjectStatus(ctx, project); err != nil {
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
	if err := r.updateProjectStatus(ctx, project); err != nil {
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
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return statusErr
		}
		return err
	}

	// Create service
	service := config.buildService()
	if err := r.createOrUpdateService(ctx, project, service); err != nil {
		r.setCondition(project, config.conditionType, metav1.ConditionFalse, "ServiceFailed", err.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
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
	config := newServiceReconcileConfig(
		"Auth",
		supabasev1alpha1.ConditionTypeAuthReady,
		func() *appsv1.Deployment { return deployments.BuildAuthDeployment(project, secretNames) },
		func() *corev1.Service { return services.BuildAuthService(project) },
		func(ready bool) { project.Status.Services.Auth = supabasev1alpha1.ServiceStatus{Ready: ready} },
	)
	config.logFields = []any{
		"image", fmt.Sprintf("%s:%s", "supabase/gotrue", project.Spec.Auth.ImageTag),
		"replicas", project.Spec.Auth.Replicas,
		"hasProviders", project.Spec.Auth.Providers != nil,
		"hasEmailHook", project.Spec.Auth.EmailHook != nil && project.Spec.Auth.EmailHook.Enabled,
	}
	return r.reconcileServiceComponent(ctx, project, config)
}

// reconcileRest deploys the REST service
func (r *SupabaseProjectReconciler) reconcileRest(ctx context.Context, project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) error {
	config := newServiceReconcileConfig(
		"REST",
		supabasev1alpha1.ConditionTypeRestReady,
		func() *appsv1.Deployment { return deployments.BuildRestDeployment(project, secretNames) },
		func() *corev1.Service { return services.BuildRestService(project) },
		func(ready bool) { project.Status.Services.Rest = supabasev1alpha1.ServiceStatus{Ready: ready} },
	)
	config.logFields = []any{
		"image", fmt.Sprintf("%s:%s", "postgrest/postgrest", project.Spec.Rest.ImageTag),
		"schemas", project.Spec.Rest.Schemas,
	}
	return r.reconcileServiceComponent(ctx, project, config)
}

// reconcileStudio deploys the Studio service
func (r *SupabaseProjectReconciler) reconcileStudio(ctx context.Context, project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) error {
	config := newServiceReconcileConfig(
		"Studio",
		supabasev1alpha1.ConditionTypeStudioReady,
		func() *appsv1.Deployment { return deployments.BuildStudioDeployment(project, secretNames) },
		func() *corev1.Service { return services.BuildStudioService(project) },
		func(ready bool) { project.Status.Services.Studio = supabasev1alpha1.ServiceStatus{Ready: ready} },
	)
	config.logFields = []any{
		"image", fmt.Sprintf("%s:%s", "supabase/studio", project.Spec.Studio.ImageTag),
		"publicURL", project.Spec.Studio.PublicURL,
	}
	return r.reconcileServiceComponent(ctx, project, config)
}

// reconcileMeta deploys the Meta service
func (r *SupabaseProjectReconciler) reconcileMeta(ctx context.Context, project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) error {
	config := newServiceReconcileConfig(
		"Meta",
		supabasev1alpha1.ConditionTypeMetaReady,
		func() *appsv1.Deployment { return deployments.BuildMetaDeployment(project, secretNames) },
		func() *corev1.Service { return services.BuildMetaService(project) },
		func(ready bool) { project.Status.Services.Meta = supabasev1alpha1.ServiceStatus{Ready: ready} },
	)
	config.logFields = []any{
		"image", fmt.Sprintf("%s:%s", "supabase/postgres-meta", project.Spec.Meta.ImageTag),
	}
	return r.reconcileServiceComponent(ctx, project, config)
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
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
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
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return statusErr
		}
		return err
	}

	// Create service
	service := services.BuildKongService(project)
	if err := r.createOrUpdateService(ctx, project, service); err != nil {
		r.setCondition(project, supabasev1alpha1.ConditionTypeKongReady, metav1.ConditionFalse, "ServiceFailed", err.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
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
	if apiequality.Semantic.DeepEqual(existing.Data, configMap.Data) {
		return nil
	}
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

	desired := deployment.DeepCopy()
	actual := existing.DeepCopy()
	normalizePodTemplateDefaults(&desired.Spec.Template)
	normalizePodTemplateDefaults(&actual.Spec.Template)
	if apiequality.Semantic.DeepEqual(actual.Spec.Replicas, desired.Spec.Replicas) &&
		apiequality.Semantic.DeepEqual(actual.Spec.Template, desired.Spec.Template) {
		return nil
	}

	// Update existing - only update operator-owned fields.
	log.V(1).Info("Updating deployment", "name", deployment.Name)
	existing.Spec.Replicas = desired.Spec.Replicas
	existing.Spec.Template = desired.Spec.Template
	return r.Update(ctx, existing)
}

func normalizePodTemplateDefaults(template *corev1.PodTemplateSpec) {
	spec := &template.Spec
	if spec.RestartPolicy == "" {
		spec.RestartPolicy = corev1.RestartPolicyAlways
	}
	if spec.TerminationGracePeriodSeconds == nil {
		spec.TerminationGracePeriodSeconds = ptr.To[int64](30)
	}
	if spec.DNSPolicy == "" {
		spec.DNSPolicy = corev1.DNSClusterFirst
	}
	if spec.SecurityContext == nil {
		spec.SecurityContext = &corev1.PodSecurityContext{}
	}
	if spec.SchedulerName == "" {
		spec.SchedulerName = corev1.DefaultSchedulerName
	}
	for i := range spec.Volumes {
		normalizeVolumeDefaults(&spec.Volumes[i])
	}
	for i := range spec.InitContainers {
		normalizeContainerDefaults(&spec.InitContainers[i])
	}
	for i := range spec.Containers {
		normalizeContainerDefaults(&spec.Containers[i])
	}
}

func normalizeVolumeDefaults(volume *corev1.Volume) {
	const defaultMode int32 = 0o644
	if volume.ConfigMap != nil && volume.ConfigMap.DefaultMode == nil {
		volume.ConfigMap.DefaultMode = ptr.To(defaultMode)
	}
	if volume.Secret != nil && volume.Secret.DefaultMode == nil {
		volume.Secret.DefaultMode = ptr.To(defaultMode)
	}
	if volume.DownwardAPI != nil && volume.DownwardAPI.DefaultMode == nil {
		volume.DownwardAPI.DefaultMode = ptr.To(defaultMode)
	}
	if volume.Projected != nil && volume.Projected.DefaultMode == nil {
		volume.Projected.DefaultMode = ptr.To(defaultMode)
	}
}

func normalizeContainerDefaults(container *corev1.Container) {
	if container.TerminationMessagePath == "" {
		container.TerminationMessagePath = corev1.TerminationMessagePathDefault
	}
	if container.TerminationMessagePolicy == "" {
		container.TerminationMessagePolicy = corev1.TerminationMessageReadFile
	}
	for i := range container.Ports {
		if container.Ports[i].Protocol == "" {
			container.Ports[i].Protocol = corev1.ProtocolTCP
		}
	}
	normalizeProbeDefaults(container.LivenessProbe)
	normalizeProbeDefaults(container.ReadinessProbe)
	normalizeProbeDefaults(container.StartupProbe)
}

func normalizeProbeDefaults(probe *corev1.Probe) {
	if probe == nil {
		return
	}
	if probe.TimeoutSeconds == 0 {
		probe.TimeoutSeconds = 1
	}
	if probe.PeriodSeconds == 0 {
		probe.PeriodSeconds = 10
	}
	if probe.SuccessThreshold == 0 {
		probe.SuccessThreshold = 1
	}
	if probe.FailureThreshold == 0 {
		probe.FailureThreshold = 3
	}
	if probe.HTTPGet != nil && probe.HTTPGet.Scheme == "" {
		probe.HTTPGet.Scheme = corev1.URISchemeHTTP
	}
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
	if apiequality.Semantic.DeepEqual(existing.Spec.Ports, service.Spec.Ports) &&
		apiequality.Semantic.DeepEqual(existing.Spec.Selector, service.Spec.Selector) {
		return nil
	}
	existing.Spec.Ports = service.Spec.Ports
	existing.Spec.Selector = service.Spec.Selector
	return r.Update(ctx, existing)
}

// reconcileBackupInfrastructure ensures ObjectStore and ScheduledBackup resources exist
func (r *SupabaseProjectReconciler) reconcileBackupInfrastructure(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	if err := r.reconcileBackup(ctx, project); err != nil {
		return err
	}
	return r.reconcileRecovery(ctx, project)
}

// reconcileBackup handles backup ObjectStore and ScheduledBackup creation
func (r *SupabaseProjectReconciler) reconcileBackup(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	log := logf.FromContext(ctx)

	// If backup is disabled, clean up any existing backup resources
	if project.Spec.Database.Backup == nil || !project.Spec.Database.Backup.Enabled {
		return r.cleanupBackupResources(ctx, project)
	}

	log.Info("Reconciling backup infrastructure")

	backup := project.Spec.Database.Backup

	// Validate S3 credentials secret exists
	if err := r.validateS3Secret(ctx, project.Namespace, backup.S3CredentialsSecret); err != nil {
		return r.failBackup(ctx, project, "SecretError", err)
	}

	// Build and create/update ObjectStore
	objectStore := cnpg.BuildObjectStore(project)
	if err := controllerutil.SetControllerReference(project, objectStore, r.Scheme); err != nil {
		return r.failBackup(ctx, project, "OwnerRefError", fmt.Errorf("failed to set owner reference on ObjectStore: %w", err))
	}

	if err := r.createOrUpdateObjectStore(ctx, objectStore); err != nil {
		return r.failBackup(ctx, project, "ObjectStoreFailed", err)
	}

	// Build and create/update ScheduledBackup
	scheduledBackup := cnpg.BuildScheduledBackup(project)
	if err := controllerutil.SetControllerReference(project, scheduledBackup, r.Scheme); err != nil {
		return r.failBackup(ctx, project, "OwnerRefError", fmt.Errorf("failed to set owner reference on ScheduledBackup: %w", err))
	}

	if err := r.createOrUpdateScheduledBackup(ctx, scheduledBackup); err != nil {
		return r.failBackup(ctx, project, "ScheduledBackupFailed", err)
	}

	// Success
	r.setCondition(project, supabasev1alpha1.ConditionTypeBackupReady, metav1.ConditionTrue, "BackupConfigured", "Backup infrastructure is ready")
	return r.updateProjectStatus(ctx, project)
}

// cleanupBackupResources removes ObjectStore and ScheduledBackup when backup is disabled
func (r *SupabaseProjectReconciler) cleanupBackupResources(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	// Skip cleanup if backup was never configured (no BackupReady condition)
	if meta.FindStatusCondition(project.Status.Conditions, supabasev1alpha1.ConditionTypeBackupReady) == nil {
		return nil
	}

	log := logf.FromContext(ctx)

	// Delete ScheduledBackup if exists
	scheduledBackup := &cnpgv1.ScheduledBackup{}
	scheduledBackupName := cnpg.ScheduledBackupName(project)
	err := r.Get(ctx, types.NamespacedName{Name: scheduledBackupName, Namespace: project.Namespace}, scheduledBackup)
	if err == nil {
		log.Info("Deleting ScheduledBackup (backup disabled)", "name", scheduledBackupName)
		if err := r.Delete(ctx, scheduledBackup); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete ScheduledBackup: %w", err)
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get ScheduledBackup: %w", err)
	}

	// Delete ObjectStore if exists
	objectStore := &barmancloudv1.ObjectStore{}
	objectStoreName := cnpg.ObjectStoreName(project)
	err = r.Get(ctx, types.NamespacedName{Name: objectStoreName, Namespace: project.Namespace}, objectStore)
	if err == nil {
		log.Info("Deleting ObjectStore (backup disabled)", "name", objectStoreName)
		if err := r.Delete(ctx, objectStore); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete ObjectStore: %w", err)
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get ObjectStore: %w", err)
	}

	// Remove stale condition after successful cleanup
	meta.RemoveStatusCondition(&project.Status.Conditions, supabasev1alpha1.ConditionTypeBackupReady)
	return r.updateProjectStatus(ctx, project)
}

// reconcileRecovery handles recovery ObjectStore creation
func (r *SupabaseProjectReconciler) reconcileRecovery(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	log := logf.FromContext(ctx)

	// If recovery is disabled, clean up any existing recovery resources
	if project.Spec.Database.Recovery == nil || !project.Spec.Database.Recovery.Enabled {
		return r.cleanupRecoveryResources(ctx, project)
	}

	log.Info("Reconciling recovery infrastructure")

	recovery := project.Spec.Database.Recovery

	// Validate S3 credentials secret exists
	if err := r.validateS3Secret(ctx, project.Namespace, recovery.S3CredentialsSecret); err != nil {
		return r.failRecovery(ctx, project, "SecretError", err)
	}

	// Build and create/update ObjectStore
	objectStore := cnpg.BuildRecoveryObjectStore(project)
	if err := controllerutil.SetControllerReference(project, objectStore, r.Scheme); err != nil {
		return r.failRecovery(ctx, project, "OwnerRefError", fmt.Errorf("failed to set owner reference on recovery ObjectStore: %w", err))
	}

	if err := r.createOrUpdateObjectStore(ctx, objectStore); err != nil {
		return r.failRecovery(ctx, project, "ObjectStoreFailed", err)
	}

	// Success
	r.setCondition(project, supabasev1alpha1.ConditionTypeRecoveryReady, metav1.ConditionTrue, "RecoveryConfigured", "Recovery infrastructure is ready")
	return r.updateProjectStatus(ctx, project)
}

// cleanupRecoveryResources removes recovery ObjectStore when recovery is disabled
func (r *SupabaseProjectReconciler) cleanupRecoveryResources(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	// Skip cleanup if recovery was never configured (no RecoveryReady condition)
	if meta.FindStatusCondition(project.Status.Conditions, supabasev1alpha1.ConditionTypeRecoveryReady) == nil {
		return nil
	}

	log := logf.FromContext(ctx)

	// Delete recovery ObjectStore if exists
	objectStore := &barmancloudv1.ObjectStore{}
	objectStoreName := cnpg.RecoveryObjectStoreName(project)
	err := r.Get(ctx, types.NamespacedName{Name: objectStoreName, Namespace: project.Namespace}, objectStore)
	if err == nil {
		log.Info("Deleting recovery ObjectStore (recovery disabled)", "name", objectStoreName)
		if err := r.Delete(ctx, objectStore); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete recovery ObjectStore: %w", err)
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get recovery ObjectStore: %w", err)
	}

	// Remove stale condition after successful cleanup
	meta.RemoveStatusCondition(&project.Status.Conditions, supabasev1alpha1.ConditionTypeRecoveryReady)
	return r.updateProjectStatus(ctx, project)
}

// failRecovery sets RecoveryReady=False condition and updates status
func (r *SupabaseProjectReconciler) failRecovery(ctx context.Context, project *supabasev1alpha1.SupabaseProject, reason string, err error) error {
	r.setCondition(project, supabasev1alpha1.ConditionTypeRecoveryReady, metav1.ConditionFalse, reason, err.Error())
	if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
		return statusErr
	}
	return err
}

// validateS3Secret validates that the S3 credentials secret exists and has required keys
func (r *SupabaseProjectReconciler) validateS3Secret(ctx context.Context, namespace, secretName string) error {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("S3 credentials secret %s not found", secretName)
		}
		return fmt.Errorf("failed to get S3 credentials secret %s: %w", secretName, err)
	}

	// Validate required keys exist
	requiredKeys := []string{cnpg.DefaultAccessKeyIDKey, cnpg.DefaultSecretAccessKeyKey}
	for _, key := range requiredKeys {
		if _, ok := secret.Data[key]; !ok {
			return fmt.Errorf("S3 credentials secret %s missing required key: %s", secretName, key)
		}
	}

	return nil
}

// failBackup sets BackupReady=False condition and updates status
func (r *SupabaseProjectReconciler) failBackup(ctx context.Context, project *supabasev1alpha1.SupabaseProject, reason string, err error) error {
	r.setCondition(project, supabasev1alpha1.ConditionTypeBackupReady, metav1.ConditionFalse, reason, err.Error())
	if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
		return statusErr
	}
	return err
}

// createOrUpdateObjectStore creates or updates an ObjectStore resource
func (r *SupabaseProjectReconciler) createOrUpdateObjectStore(ctx context.Context, objectStore *barmancloudv1.ObjectStore) error {
	log := logf.FromContext(ctx)

	existing := &barmancloudv1.ObjectStore{}
	err := r.Get(ctx, types.NamespacedName{Name: objectStore.Name, Namespace: objectStore.Namespace}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Creating ObjectStore", "name", objectStore.Name)
			if err := r.Create(ctx, objectStore); err != nil {
				return fmt.Errorf("failed to create ObjectStore: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get ObjectStore: %w", err)
	}

	if apiequality.Semantic.DeepEqual(existing.Spec.Configuration, objectStore.Spec.Configuration) &&
		existing.Spec.RetentionPolicy == objectStore.Spec.RetentionPolicy {
		return nil
	}

	// Update only fields owned by this controller. The ObjectStore CRD defaults
	// instanceSidecarConfiguration, so preserve that API-managed field.
	existing.Spec.Configuration = objectStore.Spec.Configuration
	existing.Spec.RetentionPolicy = objectStore.Spec.RetentionPolicy
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update ObjectStore: %w", err)
	}
	return nil
}

// createOrUpdateScheduledBackup creates or updates a ScheduledBackup resource
func (r *SupabaseProjectReconciler) createOrUpdateScheduledBackup(ctx context.Context, scheduledBackup *cnpgv1.ScheduledBackup) error {
	log := logf.FromContext(ctx)

	existing := &cnpgv1.ScheduledBackup{}
	err := r.Get(ctx, types.NamespacedName{Name: scheduledBackup.Name, Namespace: scheduledBackup.Namespace}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Creating ScheduledBackup", "name", scheduledBackup.Name)
			if err := r.Create(ctx, scheduledBackup); err != nil {
				return fmt.Errorf("failed to create ScheduledBackup: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get ScheduledBackup: %w", err)
	}

	if apiequality.Semantic.DeepEqual(existing.Spec, scheduledBackup.Spec) {
		return nil
	}

	// Update existing ScheduledBackup.
	existing.Spec = scheduledBackup.Spec
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update ScheduledBackup: %w", err)
	}
	return nil
}

// reconcileCDCPermissions ensures CDC permissions are applied via a Job.
// Returns RequeueAfter when the Job is still running so we don't proceed
// to deploy PowerSync before permissions exist.
func (r *SupabaseProjectReconciler) reconcileCDCPermissions(ctx context.Context, project *supabasev1alpha1.SupabaseProject) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling CDC permissions")

	secretNames := &project.Status.SecretNames

	// Create CDC migrations ConfigMap
	configMap := jobs.BuildCDCMigrationsConfigMap(project)
	if err := r.createOrUpdateConfigMap(ctx, project, configMap); err != nil {
		r.setCondition(project, supabasev1alpha1.ConditionTypeCDCReady, metav1.ConditionFalse, "ConfigMapFailed", err.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, err
	}

	// Compute a hash of the CDC script so we can detect spec changes
	scriptHash := cdcScriptHash(project)

	// Create or check CDC permissions Job
	job := jobs.BuildCDCPermissionsJob(project, secretNames)
	completed, err := r.createOrCheckJob(ctx, project, job, scriptHash)
	if err != nil {
		r.setCondition(project, supabasev1alpha1.ConditionTypeCDCReady, metav1.ConditionFalse, "JobFailed", err.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, err
	}

	if !completed {
		r.setCondition(project, supabasev1alpha1.ConditionTypeCDCReady, metav1.ConditionFalse, "JobRunning", "CDC permissions Job is still running")
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{RequeueAfter: RequeueDelay}, nil
	}

	r.setCondition(project, supabasev1alpha1.ConditionTypeCDCReady, metav1.ConditionTrue, "CDCPermissionsApplied", "CDC permissions applied successfully")
	return ctrl.Result{}, nil
}

// cdcScriptHash computes a SHA-256 hash of the CDC setup script content.
// Used to detect permission script changes so the Job can be recreated.
func cdcScriptHash(project *supabasev1alpha1.SupabaseProject) string {
	cm := jobs.BuildCDCMigrationsConfigMap(project)
	h := sha256.Sum256([]byte(cm.Data["setup.sh"]))
	return hex.EncodeToString(h[:])
}

func (r *SupabaseProjectReconciler) reconcilePowerSyncPublication(ctx context.Context, project *supabasev1alpha1.SupabaseProject) (ctrl.Result, error) {
	desired := cnpg.BuildPowerSyncPublication(project)
	existing := &cnpgv1.Publication{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		if err := controllerutil.SetControllerReference(project, desired, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, desired); err != nil {
			return ctrl.Result{}, err
		}
		r.setCondition(project, supabasev1alpha1.ConditionTypeCDCReady, metav1.ConditionFalse, "PublicationPending", "Waiting for the PowerSync publication")
		if err := r.updateProjectStatus(ctx, project); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: RequeueDelay}, nil
	}

	needsUpdate := !apiequality.Semantic.DeepEqual(desired.Spec, existing.Spec)
	if needsUpdate {
		existing.Spec = desired.Spec
	}
	if existing.Labels == nil && len(desired.Labels) > 0 {
		existing.Labels = make(map[string]string, len(desired.Labels))
	}
	for key, value := range desired.Labels {
		if existing.Labels[key] != value {
			existing.Labels[key] = value
			needsUpdate = true
		}
	}
	ownerReferences := append([]metav1.OwnerReference(nil), existing.OwnerReferences...)
	if err := controllerutil.SetControllerReference(project, existing, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if !apiequality.Semantic.DeepEqual(ownerReferences, existing.OwnerReferences) {
		needsUpdate = true
	}
	if needsUpdate {
		if err := r.Update(ctx, existing); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating PowerSync publication: %w", err)
		}
		return ctrl.Result{RequeueAfter: RequeueDelay}, nil
	}

	if !publicationIsApplied(existing) {
		message := "Waiting for the PowerSync publication"
		if existing.Status.Message != "" {
			message = existing.Status.Message
		}
		r.setCondition(project, supabasev1alpha1.ConditionTypeCDCReady, metav1.ConditionFalse, "PublicationPending", message)
		if err := r.updateProjectStatus(ctx, project); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: RequeueDelay}, nil
	}

	return ctrl.Result{}, nil
}

func publicationIsApplied(publication *cnpgv1.Publication) bool {
	return publication.Status.ObservedGeneration == publication.Generation &&
		publication.Status.Applied != nil && *publication.Status.Applied
}

// reconcilePowersync deploys the Powersync service (API + Replication + ConfigMaps + CronJob)
func (r *SupabaseProjectReconciler) reconcilePowersync(ctx context.Context, project *supabasev1alpha1.SupabaseProject, syncRulesContent []byte) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling Powersync service")

	secretNames := &project.Status.SecretNames

	// Create Powersync config ConfigMap
	psConfig := configmaps.BuildPowersyncConfigMap(project)
	if err := r.createOrUpdateConfigMap(ctx, project, psConfig); err != nil {
		r.setCondition(project, supabasev1alpha1.ConditionTypePowersyncReady, metav1.ConditionFalse, "ConfigMapFailed", err.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, err
	}

	// Create sync rules ConfigMap (may be nil if external ConfigMapRef is used)
	syncRulesConfigMap := configmaps.BuildPowersyncSyncRulesConfigMap(project)
	if syncRulesConfigMap != nil {
		if err := r.createOrUpdateConfigMap(ctx, project, syncRulesConfigMap); err != nil {
			r.setCondition(project, supabasev1alpha1.ConditionTypePowersyncReady, metav1.ConditionFalse, "SyncRulesConfigMapFailed", err.Error())
			if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
				return ctrl.Result{}, statusErr
			}
			return ctrl.Result{}, err
		}
	}

	// Deploy Powersync API
	apiDeployment := deployments.BuildPowersyncAPIDeployment(project, secretNames)
	applyPowerSyncConfigHash(apiDeployment, psConfig.Data["config.json"], syncRulesContent)
	if err := r.createOrUpdateDeployment(ctx, project, apiDeployment); err != nil {
		r.setCondition(project, supabasev1alpha1.ConditionTypePowersyncReady, metav1.ConditionFalse, "APIDeploymentFailed", err.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, err
	}

	// Create Powersync API service
	apiService := services.BuildPowersyncAPIService(project)
	if err := r.createOrUpdateService(ctx, project, apiService); err != nil {
		r.setCondition(project, supabasev1alpha1.ConditionTypePowersyncReady, metav1.ConditionFalse, "APIServiceFailed", err.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, err
	}

	// Deploy Powersync Replication
	replDeployment := deployments.BuildPowersyncReplicationDeployment(project, secretNames)
	applyPowerSyncConfigHash(replDeployment, psConfig.Data["config.json"], syncRulesContent)
	if err := r.createOrUpdateDeployment(ctx, project, replDeployment); err != nil {
		r.setCondition(project, supabasev1alpha1.ConditionTypePowersyncReady, metav1.ConditionFalse, "ReplicationDeploymentFailed", err.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, err
	}

	// Deploy Powersync Compact CronJob
	compactCronJob := deployments.BuildPowersyncCompactCronJob(project, secretNames)
	if compactCronJob != nil {
		if err := r.createOrUpdateCronJob(ctx, project, compactCronJob); err != nil {
			r.setCondition(project, supabasev1alpha1.ConditionTypePowersyncReady, metav1.ConditionFalse, "CronJobFailed", err.Error())
			if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
				return ctrl.Result{}, statusErr
			}
			return ctrl.Result{}, err
		}
	} else if err := r.cleanupPowerSyncCompact(ctx, project); err != nil {
		return ctrl.Result{}, err
	}

	apiReady, apiAvailable, err := r.powersyncDeploymentStatus(ctx, apiDeployment)
	if err != nil {
		return ctrl.Result{}, err
	}
	replicationReady, replicationAvailable, err := r.powersyncDeploymentStatus(ctx, replDeployment)
	if err != nil {
		return ctrl.Result{}, err
	}
	project.Status.Services.PowersyncAPI = supabasev1alpha1.ServiceStatus{Ready: apiReady, AvailableReplicas: apiAvailable}
	project.Status.Services.PowersyncReplication = supabasev1alpha1.ServiceStatus{Ready: replicationReady, AvailableReplicas: replicationAvailable}
	if !apiReady || !replicationReady {
		r.setCondition(project, supabasev1alpha1.ConditionTypePowersyncReady, metav1.ConditionFalse, "DeploymentsPending", "Waiting for PowerSync deployments to become ready")
		if err := r.updateProjectStatus(ctx, project); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: RequeueDelay}, nil
	}

	r.setCondition(project, supabasev1alpha1.ConditionTypePowersyncReady, metav1.ConditionTrue, "Ready", "Powersync service is running")
	return ctrl.Result{}, nil
}

const powerSyncConfigHashAnnotation = "supabase.guion.dev/powersync-config-hash"

func applyPowerSyncConfigHash(deployment *appsv1.Deployment, config string, syncRules []byte) {
	hash := sha256.New()
	_, _ = hash.Write([]byte(config))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(syncRules)
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	deployment.Spec.Template.Annotations[powerSyncConfigHashAnnotation] = hex.EncodeToString(hash.Sum(nil))
}

// cleanupPowerSync removes operator-owned runtime resources when PowerSync is
// disabled. Database roles, generated credentials, and PowerSync's internal
// database data are deliberately retained; deleting those requires an explicit
// data-retention policy.
func (r *SupabaseProjectReconciler) cleanupPowerSync(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	resources := []client.Object{
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deployments.PowersyncAPIDeploymentName(project), Namespace: project.Namespace}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deployments.PowersyncReplicationDeploymentName(project), Namespace: project.Namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: project.Name + "-powersync-api", Namespace: project.Namespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configmaps.PowersyncConfigMapName(project), Namespace: project.Namespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configmaps.PowersyncSyncRulesConfigMapName(project), Namespace: project.Namespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: jobs.CDCConfigMapName(project), Namespace: project.Namespace}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: jobs.CDCJobName(project), Namespace: project.Namespace}},
		&batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: deployments.PowersyncCompactCronJobName(project), Namespace: project.Namespace}},
		&cnpgv1.Publication{ObjectMeta: metav1.ObjectMeta{Name: project.Name + "-powersync", Namespace: project.Namespace}},
	}
	for _, resource := range resources {
		if err := r.deletePowerSyncOwnedResource(ctx, project, resource); err != nil {
			return err
		}
	}

	if !powerSyncStatusNeedsCleanup(project) {
		return nil
	}
	project.Status.Services.PowersyncAPI = supabasev1alpha1.ServiceStatus{}
	project.Status.Services.PowersyncReplication = supabasev1alpha1.ServiceStatus{}
	meta.RemoveStatusCondition(&project.Status.Conditions, supabasev1alpha1.ConditionTypeCDCReady)
	meta.RemoveStatusCondition(&project.Status.Conditions, supabasev1alpha1.ConditionTypePowersyncReady)
	return r.updateProjectStatus(ctx, project)
}

func powerSyncStatusNeedsCleanup(project *supabasev1alpha1.SupabaseProject) bool {
	if !apiequality.Semantic.DeepEqual(project.Status.Services.PowersyncAPI, supabasev1alpha1.ServiceStatus{}) ||
		!apiequality.Semantic.DeepEqual(project.Status.Services.PowersyncReplication, supabasev1alpha1.ServiceStatus{}) {
		return true
	}
	return meta.FindStatusCondition(project.Status.Conditions, supabasev1alpha1.ConditionTypeCDCReady) != nil ||
		meta.FindStatusCondition(project.Status.Conditions, supabasev1alpha1.ConditionTypePowersyncReady) != nil
}

func (r *SupabaseProjectReconciler) cleanupPowerSyncCompact(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	cronJob := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{
		Name:      deployments.PowersyncCompactCronJobName(project),
		Namespace: project.Namespace,
	}}
	return r.deletePowerSyncOwnedResource(ctx, project, cronJob)
}

func (r *SupabaseProjectReconciler) deletePowerSyncOwnedResource(ctx context.Context, project *supabasev1alpha1.SupabaseProject, resource client.Object) error {
	if err := r.Get(ctx, client.ObjectKeyFromObject(resource), resource); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !metav1.IsControlledBy(resource, project) {
		return nil
	}
	return client.IgnoreNotFound(r.Delete(ctx, resource))
}

func (r *SupabaseProjectReconciler) powersyncDeploymentStatus(ctx context.Context, desired *appsv1.Deployment) (bool, int32, error) {
	existing := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing); err != nil {
		return false, 0, err
	}
	return powersyncDeploymentIsReady(existing), existing.Status.AvailableReplicas, nil
}

func powersyncDeploymentIsReady(deployment *appsv1.Deployment) bool {
	expected := int32(1)
	if deployment.Spec.Replicas != nil {
		expected = *deployment.Spec.Replicas
	}
	return deployment.Status.ObservedGeneration == deployment.Generation &&
		deployment.Status.UpdatedReplicas == expected &&
		deployment.Status.ReadyReplicas == expected &&
		deployment.Status.AvailableReplicas == expected &&
		deployment.Status.UnavailableReplicas == 0
}

func (r *SupabaseProjectReconciler) updateProjectStatus(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	current := &supabasev1alpha1.SupabaseProject{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(project), current); err != nil {
		return err
	}
	if apiequality.Semantic.DeepEqual(current.Status, project.Status) {
		return nil
	}
	return r.Status().Update(ctx, project)
}

// createOrUpdateCronJob creates or updates a CronJob resource
func (r *SupabaseProjectReconciler) createOrUpdateCronJob(ctx context.Context, project *supabasev1alpha1.SupabaseProject, cronJob *batchv1.CronJob) error {
	log := logf.FromContext(ctx)

	if err := controllerutil.SetControllerReference(project, cronJob, r.Scheme); err != nil {
		return err
	}

	existing := &batchv1.CronJob{}
	err := r.Get(ctx, types.NamespacedName{Name: cronJob.Name, Namespace: cronJob.Namespace}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Creating CronJob", "name", cronJob.Name)
			return r.Create(ctx, cronJob)
		}
		return err
	}

	desired := cronJob.DeepCopy()
	actual := existing.DeepCopy()
	normalizeCronJobDefaults(&desired.Spec)
	normalizeCronJobDefaults(&actual.Spec)
	if apiequality.Semantic.DeepEqual(actual.Spec, desired.Spec) {
		return nil
	}

	// Update existing.
	existing.Spec = desired.Spec
	return r.Update(ctx, existing)
}

func normalizeCronJobDefaults(spec *batchv1.CronJobSpec) {
	if spec.Suspend == nil {
		spec.Suspend = ptr.To(false)
	}
	normalizePodTemplateDefaults(&spec.JobTemplate.Spec.Template)
}

const cdcScriptHashAnnotation = "supabase.guion.dev/cdc-script-hash"

// createOrCheckJob creates a Job if it doesn't exist, or checks status of an existing Job.
// scriptHash is compared against an annotation on the existing Job. When the
// permission script changes, the old Job is deleted and a new one is created.
// Returns (true, nil) when the Job has completed successfully, (false, nil) when still
// running or just created, and (false, err) on failure.
func (r *SupabaseProjectReconciler) createOrCheckJob(ctx context.Context, project *supabasev1alpha1.SupabaseProject, job *batchv1.Job, scriptHash string) (bool, error) {
	log := logf.FromContext(ctx)

	// Set owner reference
	if err := controllerutil.SetControllerReference(project, job, r.Scheme); err != nil {
		return false, err
	}

	// Annotate the Job with the script hash
	if job.Annotations == nil {
		job.Annotations = make(map[string]string)
	}
	job.Annotations[cdcScriptHashAnnotation] = scriptHash

	// Check if Job exists
	existing := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Creating Job", "name", job.Name)
			return false, r.Create(ctx, job)
		}
		return false, err
	}

	// Check if the script has changed since the existing Job was created.
	// If the hash differs, delete the old Job so a fresh one runs with the new script.
	existingHash := existing.Annotations[cdcScriptHashAnnotation]
	if existingHash != scriptHash {
		log.Info("CDC script changed, recreating Job", "name", job.Name, "oldHash", existingHash, "newHash", scriptHash)
		propagation := metav1.DeletePropagationForeground
		if err := r.Delete(ctx, existing, &client.DeleteOptions{
			PropagationPolicy: &propagation,
		}); err != nil && !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("failed to delete outdated Job %s: %w", job.Name, err)
		}
		// Requeue — the next reconcile will create the new Job once the old one is gone
		return false, nil
	}

	// Job exists with matching hash — only terminal conditions are authoritative.
	for _, condition := range existing.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		switch condition.Type {
		case batchv1.JobComplete:
			log.V(1).Info("Job completed successfully", "name", job.Name)
			return true, nil
		case batchv1.JobFailed:
			return false, fmt.Errorf("job %s has failed: %s", job.Name, condition.Message)
		}
	}

	// Job still running
	log.V(1).Info("Job still running", "name", job.Name, "active", existing.Status.Active)
	return false, nil
}

func (r *SupabaseProjectReconciler) setCondition(project *supabasev1alpha1.SupabaseProject, conditionType string, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: project.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	meta.SetStatusCondition(&project.Status.Conditions, condition)
}

func (r *SupabaseProjectReconciler) mapPowerSyncConfigMapToProjects(ctx context.Context, object client.Object) []reconcile.Request {
	projects := &supabasev1alpha1.SupabaseProjectList{}
	if err := r.List(ctx, projects, client.InNamespace(object.GetNamespace())); err != nil {
		logf.FromContext(ctx).Error(err, "listing SupabaseProjects for PowerSync ConfigMap", "configMap", object.GetName())
		return nil
	}
	requests := make([]reconcile.Request, 0)
	for i := range projects.Items {
		project := &projects.Items[i]
		if project.Spec.Powersync == nil || project.Spec.Powersync.SyncRules.ConfigMapRef != object.GetName() {
			continue
		}
		requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Name: project.Name, Namespace: project.Namespace}})
	}
	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *SupabaseProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&supabasev1alpha1.SupabaseProject{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.mapPowerSyncConfigMapToProjects)).
		Owns(&corev1.Service{}).
		Owns(&appsv1.Deployment{}).
		Owns(&batchv1.Job{}).
		Owns(&batchv1.CronJob{}).
		Owns(&cnpgv1.Cluster{}).
		Owns(&cnpgv1.Publication{}).
		Owns(&cnpgv1.ScheduledBackup{}).
		Owns(&barmancloudv1.ObjectStore{}).
		Named("supabaseproject").
		Complete(r)
}
