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
	"strings"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	barmancloudv1 "github.com/cloudnative-pg/plugin-barman-cloud/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/configmaps"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/defaults"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/deployments"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/jobs"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/secrets"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/services"
)

const (
	// RequeueDelay is the delay before requeueing when waiting for resources
	RequeueDelay = 10 * time.Second

	emailHookSecretHashAnnotation    = "supabase.guion.dev/email-hook-secret-hash"
	projectCredentialsHashAnnotation = "supabase.guion.dev/project-credentials-hash"
)

// serviceReconcileConfig holds configuration for reconciling a service component.
type serviceReconcileConfig struct {
	name            string
	conditionType   string
	buildDeployment func() *appsv1.Deployment
	buildService    func() *corev1.Service
	setStatus       func(status supabasev1alpha1.ServiceStatus)
	logFields       []any // optional
	credentialHash  string
}

// newServiceReconcileConfig creates a serviceReconcileConfig with required fields as parameters.
// This ensures callers cannot forget to provide required values (compile-time enforcement).
func newServiceReconcileConfig(
	name string,
	conditionType string,
	buildDeployment func() *appsv1.Deployment,
	buildService func() *corev1.Service,
	setStatus func(status supabasev1alpha1.ServiceStatus),
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

	// Phase 0: repair durable ownership metadata before validating any external
	// input. A bad credential or backup secret must not leave an existing
	// database vulnerable to garbage collection during an upgrade.
	if err := r.reconcileDurableMetadataProtection(ctx, project); err != nil {
		return ctrl.Result{}, err
	}

	// Phase 1: Validate the externally managed project credential bundle and
	// ensure implementation secrets exist. No dependent workload is touched on
	// a validation failure.
	credentials, err := r.reconcileSecrets(ctx, project)
	if err != nil {
		return ctrl.Result{}, err
	}
	// Validate creation-time recovery intent after implementation secrets have
	// been synchronized, so a recreated project has the deterministic secret
	// references used by its retained CNPG bootstrap.
	if err := r.validateRecoveryBootstrapIntent(ctx, project); err != nil {
		r.setCondition(project, supabasev1alpha1.ConditionTypeDatabaseReady, metav1.ConditionFalse, "BootstrapImmutable", err.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, err
	}

	// Phase 2: Ensure init SQL ConfigMap exists
	if err := r.reconcileInitSQL(ctx, project); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileJWKSConfigMap(ctx, project, credentials.PublicJWKS); err != nil {
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
	if err := r.reconcileServices(ctx, project, credentials); err != nil {
		return ctrl.Result{}, err
	}
	if !coreServicesReady(project) {
		return ctrl.Result{RequeueAfter: RequeueDelay}, nil
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
		// PowerSync verifies sessions through the stable public JWKS URL and does
		// not consume any project credential value directly, so key rotation must
		// not roll its workloads.
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

// reconcileSecrets validates the external credential SSOT and ensures only
// operator-owned implementation Secrets are present. It returns a transient
// credential projection used for derived configuration and rollout hashes.
func (r *SupabaseProjectReconciler) reconcileSecrets(ctx context.Context, project *supabasev1alpha1.SupabaseProject) (*secrets.ProjectCredentials, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling project credentials and implementation secrets")
	if project.Status.Phase == supabasev1alpha1.PhasePending {
		project.Status.Phase = supabasev1alpha1.PhaseProvisioning
	}

	if project.Spec.ProjectCredentialsSecret == "" {
		err := fmt.Errorf("project credential field %q: Secret reference is required", "projectCredentialsSecret")
		return r.failReconcileSecrets(ctx, project, "CredentialsReferenceMissing", err.Error(), err)
	}
	external := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: project.Spec.ProjectCredentialsSecret, Namespace: project.Namespace}, external); err != nil {
		message := fmt.Sprintf("project credential Secret %q is unavailable", project.Spec.ProjectCredentialsSecret)
		if !apierrors.IsNotFound(err) {
			message = fmt.Sprintf("project credential Secret %q could not be read", project.Spec.ProjectCredentialsSecret)
		}
		return r.failReconcileSecrets(ctx, project, "CredentialsSecretUnavailable", message, fmt.Errorf("project credential Secret %q unavailable", project.Spec.ProjectCredentialsSecret))
	}
	credentials, err := secrets.ValidateProjectCredentials(external)
	if err != nil {
		return r.failReconcileSecrets(ctx, project, "InvalidProjectCredentials", err.Error(), err)
	}

	if err := r.reconcileImplementationSecrets(ctx, project); err != nil {
		return r.failReconcileSecrets(ctx, project, "ImplementationSecretFailed", err.Error(), err)
	}
	r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionTrue, "CredentialsValidated", "Project credentials and implementation secrets are ready")
	if err := r.updateProjectStatus(ctx, project); err != nil {
		return nil, err
	}
	return credentials, nil
}

// failReconcileSecrets records both the detailed SecretsReady failure and the
// aggregate Ready failure. The aggregate condition must be cleared immediately
// so an already-ready project cannot remain advertised as ready after a bad
// credential rotation.
func (r *SupabaseProjectReconciler) failReconcileSecrets(ctx context.Context, project *supabasev1alpha1.SupabaseProject, reason, message string, reconcileErr error) (*secrets.ProjectCredentials, error) {
	r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionFalse, reason, message)
	r.setCondition(project, supabasev1alpha1.ConditionTypeReady, metav1.ConditionFalse, "SecretsNotReady", "Project credentials and implementation secrets are not ready")
	if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
		return nil, statusErr
	}
	return nil, reconcileErr
}

func (r *SupabaseProjectReconciler) reconcileImplementationSecrets(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	generated, names, err := secrets.GenerateSecrets(project)
	if err != nil {
		return err
	}
	// Preserve create-once generated values and adopt existing resources after
	// a project is recreated with the same name.
	for _, generatedSecret := range generated {
		if err := r.createOrUpdateSecret(ctx, project, generatedSecret); err != nil {
			return err
		}
	}
	if project.Spec.Powersync != nil {
		if err := r.reconcilePowersyncSecrets(ctx, project, &names); err != nil {
			return err
		}
	}
	if err := r.reconcileEmailHookSecret(ctx, project); err != nil {
		return err
	}
	names.EmailHook = project.Status.SecretNames.EmailHook
	project.Status.SecretNames = names
	return nil
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

	name := secrets.EmailHookSecretName(project)
	existing := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: project.Namespace}, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return r.failEmailHookSecret(ctx, project, "EmailHookSecretUnavailable", err)
		}
		generated, _, err := secrets.GenerateEmailHookSecret(project)
		if err != nil {
			return r.failEmailHookSecret(ctx, project, "EmailHookSecretGenerationFailed", err)
		}
		if err := r.createOrUpdateSecret(ctx, project, generated); err != nil {
			return r.failEmailHookSecret(ctx, project, "EmailHookSecretCreateFailed", err)
		}
	} else if err := secrets.ValidateEmailHookSecret(existing); err != nil {
		return r.failEmailHookSecret(ctx, project, "InvalidEmailHookSecret", err)
	}

	project.Status.SecretNames.EmailHook = name
	r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionTrue, "SecretsReady", "All secrets, including the email hook secret, are ready")
	return r.updateProjectStatus(ctx, project)
}

func (r *SupabaseProjectReconciler) failEmailHookSecret(ctx context.Context, project *supabasev1alpha1.SupabaseProject, reason string, reconcileErr error) error {
	r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionFalse, reason, reconcileErr.Error())
	if err := r.updateProjectStatus(ctx, project); err != nil {
		return err
	}
	return reconcileErr
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

func (r *SupabaseProjectReconciler) reconcileJWKSConfigMap(ctx context.Context, project *supabasev1alpha1.SupabaseProject, jwks string) error {
	configMap := configmaps.BuildJWKSConfigMap(project, jwks)
	if err := r.createOrUpdateConfigMap(ctx, project, configMap); err != nil {
		r.setCondition(project, supabasev1alpha1.ConditionTypeSecretsReady, metav1.ConditionFalse, "JWKSConfigMapFailed", err.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return statusErr
		}
		return err
	}
	return nil
}

// reconcileCNPGCluster ensures the CNPG Cluster exists
func (r *SupabaseProjectReconciler) reconcileCNPGCluster(ctx context.Context, project *supabasev1alpha1.SupabaseProject) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling CNPG Cluster")

	desired := cnpg.BuildCluster(project, &project.Status.SecretNames)
	// CNPG admission defaults persisted Clusters before they reach this
	// reconciler. Apply the same defaults to desired state so retained Clusters
	// converge instead of being rewritten on every reconciliation.
	desired.Default()

	// Check if cluster exists
	existing := &cnpgv1.Cluster{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// CNPG clusters are durable project data. They deliberately do not
			// carry a SupabaseProject controller owner reference.
			log.Info("Creating CNPG Cluster", "name", desired.Name)
			if err := r.Create(ctx, desired); err != nil {
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

	// A project recreation or an upgrade from an older operator may leave a
	// SupabaseProject owner reference behind. Remove only that reference and
	// preserve any owner references belonging to another controller.
	if instance := existing.Labels[common.LabelInstance]; instance != project.Name {
		return ctrl.Result{}, fmt.Errorf("CNPG Cluster %s has instance label %q, expected %q", existing.Name, instance, project.Name)
	}
	changed := removeSupabaseProjectOwnerReference(existing, project.Name)
	if mergeLabels(existing, desired.Labels) {
		changed = true
	}

	if recoveryBootstrapIntentChanged(existing, desired) {
		if changed {
			if err := r.Update(ctx, existing); err != nil {
				return ctrl.Result{}, fmt.Errorf("updating CNPG cluster metadata: %w", err)
			}
		}
		err := fmt.Errorf("CNPG bootstrap configuration is immutable after cluster creation")
		r.setCondition(project, supabasev1alpha1.ConditionTypeDatabaseReady, metav1.ConditionFalse, "BootstrapImmutable", err.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, err
	}

	originalSpec := existing.Spec.DeepCopy()
	mutableChanged, reconcileErr := reconcileClusterMutableFields(existing, desired)
	if reconcileErr != nil {
		// Mutable reconciliation may have changed fields before discovering an
		// invalid storage shrink. Restore the original spec and persist only the
		// durable metadata repair (owner/labels).
		existing.Spec = *originalSpec
		if changed {
			if err := r.Update(ctx, existing); err != nil {
				return ctrl.Result{}, fmt.Errorf("updating CNPG cluster metadata: %w", err)
			}
		}
		r.setCondition(project, supabasev1alpha1.ConditionTypeDatabaseReady, metav1.ConditionFalse, reconcileErrReason(reconcileErr), reconcileErr.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, reconcileErr
	}
	changed = changed || mutableChanged
	if changed {
		if err := r.Update(ctx, existing); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating CNPG cluster: %w", err)
		}
		return ctrl.Result{RequeueAfter: RequeueDelay}, nil
	}

	return ctrl.Result{}, nil
}

// recoveryBootstrapIntentChanged reports changes to all creation-time
// recovery inputs. CNPG treats bootstrap and the external source plugin as
// immutable once a cluster exists, so silently copying a new desired value
// would make status lie about how the database was initialized.
func recoveryBootstrapIntentChanged(existing, desired *cnpgv1.Cluster) bool {
	if existing == nil || desired == nil {
		return false
	}
	existingRecovery, existingHasRecovery := recoveryBootstrap(existing.Spec.Bootstrap)
	desiredRecovery, desiredHasRecovery := recoveryBootstrap(desired.Spec.Bootstrap)
	if existingHasRecovery != desiredHasRecovery {
		return true
	}
	// InitDB is the other mutually exclusive creation-time bootstrap mode. Keep
	// comparing its owned fields as the previous immutable guard did; otherwise
	// an existing initdb cluster could silently accept a changed database owner,
	// password Secret, or post-init SQL reference.
	if existing.Spec.Bootstrap != nil && desired.Spec.Bootstrap != nil && existing.Spec.Bootstrap.InitDB != nil && desired.Spec.Bootstrap.InitDB != nil {
		existingInitDB := existing.Spec.Bootstrap.InitDB
		desiredInitDB := desired.Spec.Bootstrap.InitDB
		if existingInitDB.Database != desiredInitDB.Database ||
			existingInitDB.Owner != desiredInitDB.Owner ||
			!apiequality.Semantic.DeepEqual(existingInitDB.Secret, desiredInitDB.Secret) ||
			!apiequality.Semantic.DeepEqual(existingInitDB.PostInitApplicationSQLRefs, desiredInitDB.PostInitApplicationSQLRefs) {
			return true
		}
	}
	if existingHasRecovery && !apiequality.Semantic.DeepEqual(existingRecovery, desiredRecovery) {
		return true
	}

	// The barman external-cluster plugin carries immutable source identity such
	// as its plugin name, serverName, and barmanObjectName. Compare only these
	// explicit identity fields; operational or future plugin parameters remain
	// available for CNPG/plugin reconciliation.
	existingSource := recoveryExternalCluster(existing.Spec.ExternalClusters, recoverySourceName(existingRecovery))
	desiredSource := recoveryExternalCluster(desired.Spec.ExternalClusters, recoverySourceName(desiredRecovery))
	if (existingSource == nil) != (desiredSource == nil) {
		return true
	}
	if existingSource != nil && !apiequality.Semantic.DeepEqual(recoveryExternalClusterIdentity(existingSource), recoveryExternalClusterIdentity(desiredSource)) {
		return true
	}
	return false
}

func recoveryBootstrap(bootstrap *cnpgv1.BootstrapConfiguration) (*cnpgv1.BootstrapRecovery, bool) {
	if bootstrap == nil || bootstrap.Recovery == nil {
		return nil, false
	}
	return bootstrap.Recovery, true
}

func recoverySourceName(recovery *cnpgv1.BootstrapRecovery) string {
	if recovery == nil {
		return ""
	}
	return recovery.Source
}

func recoveryExternalCluster(clusters []cnpgv1.ExternalCluster, sourceName string) *cnpgv1.ExternalCluster {
	if sourceName == "" {
		return nil
	}
	for i := range clusters {
		if clusters[i].Name == sourceName {
			return clusters[i].DeepCopy()
		}
	}
	return nil
}

type recoveryExternalClusterIdentityFields struct {
	Name             string
	PluginName       string
	BarmanObjectName string
	ServerName       string
}

func recoveryExternalClusterIdentity(source *cnpgv1.ExternalCluster) recoveryExternalClusterIdentityFields {
	identity := recoveryExternalClusterIdentityFields{}
	if source == nil {
		return identity
	}
	identity.Name = source.Name
	if plugin := source.PluginConfiguration; plugin != nil {
		identity.PluginName = plugin.Name
		identity.BarmanObjectName = plugin.Parameters["barmanObjectName"]
		identity.ServerName = plugin.Parameters["serverName"]
	}
	return identity
}

func reconcileErrReason(err error) string {
	if strings.Contains(err.Error(), "storage shrink") {
		return "StorageShrink"
	}
	return "InvalidDatabaseConfiguration"
}

// reconcileClusterMutableFields updates the fields exposed by SupabaseProject.
// CNPG defaults, runtime state, and fields outside the explicit project
// projection are intentionally left in place.
func reconcileClusterMutableFields(existing, desired *cnpgv1.Cluster) (bool, error) {
	changed := false
	// Validate and apply storage first. This is the only mutable operation that
	// can fail; doing it before the remaining assignments keeps this helper
	// transactional for invalid shrink requests.
	storageChanged, err := reconcileStorage(&existing.Spec.StorageConfiguration, &desired.Spec.StorageConfiguration)
	if err != nil {
		return false, err
	}
	changed = changed || storageChanged

	if existing.Spec.Instances != desired.Spec.Instances {
		existing.Spec.Instances = desired.Spec.Instances
		changed = true
	}
	if desired.Spec.ImageName != "" && existing.Spec.ImageName != desired.Spec.ImageName {
		existing.Spec.ImageName = desired.Spec.ImageName
		changed = true
	}
	// Resource requirements are operator-owned desired state. A zero value is
	// meaningful: clearing the SupabaseProject field must remove a stale
	// requirement rather than treating it as a CNPG default/foreign field.
	if !apiequality.Semantic.DeepEqual(existing.Spec.Resources, desired.Spec.Resources) {
		existing.Spec.Resources = desired.Spec.Resources
		changed = true
	}
	if !apiequality.Semantic.DeepEqual(existing.Spec.EnableSuperuserAccess, desired.Spec.EnableSuperuserAccess) && desired.Spec.EnableSuperuserAccess != nil {
		existing.Spec.EnableSuperuserAccess = desired.Spec.EnableSuperuserAccess
		changed = true
	}

	// The PostgreSQL projection is complete: defaults and the current project
	// declaration replace any retained or directly edited values.
	postgres := &existing.Spec.PostgresConfiguration
	desiredPostgres := desired.Spec.PostgresConfiguration
	if !apiequality.Semantic.DeepEqual(postgres.Parameters, desiredPostgres.Parameters) {
		postgres.Parameters = desiredPostgres.Parameters
		changed = true
	}
	if !apiequality.Semantic.DeepEqual(postgres.PgHBA, desiredPostgres.PgHBA) {
		postgres.PgHBA = desiredPostgres.PgHBA
		changed = true
	}
	if !apiequality.Semantic.DeepEqual(postgres.AdditionalLibraries, desiredPostgres.AdditionalLibraries) {
		postgres.AdditionalLibraries = desiredPostgres.AdditionalLibraries
		changed = true
	}

	// The API currently owns only the anti-affinity toggle (derived from the
	// requested instance count). Preserve CNPG defaults and any user/controller
	// affinity fields that are outside this operator's contract.
	if !apiequality.Semantic.DeepEqual(existing.Spec.Affinity.EnablePodAntiAffinity, desired.Spec.Affinity.EnablePodAntiAffinity) {
		existing.Spec.Affinity.EnablePodAntiAffinity = desired.Spec.Affinity.EnablePodAntiAffinity
		changed = true
	}
	if desired.Spec.Managed != nil {
		if existing.Spec.Managed == nil {
			existing.Spec.Managed = &cnpgv1.ManagedConfiguration{}
		}
		if !apiequality.Semantic.DeepEqual(existing.Spec.Managed.Roles, desired.Spec.Managed.Roles) {
			existing.Spec.Managed.Roles = desired.Spec.Managed.Roles
			changed = true
		}
	}

	plugins, pluginsChanged := reconcileBarmanPlugin(existing.Spec.Plugins, desired.Spec.Plugins)
	if pluginsChanged {
		existing.Spec.Plugins = plugins
		changed = true
	}

	return changed, nil
}

func reconcileBarmanPlugin(existing, desired []cnpgv1.PluginConfiguration) ([]cnpgv1.PluginConfiguration, bool) {
	const name = cnpg.BarmanCloudPluginName
	var desiredPlugin *cnpgv1.PluginConfiguration
	for i := range desired {
		if desired[i].Name == name {
			copy := desired[i]
			desiredPlugin = &copy
			break
		}
	}
	result := make([]cnpgv1.PluginConfiguration, 0, len(existing)+1)
	found := false
	changed := false
	for _, plugin := range existing {
		if plugin.Name != name {
			result = append(result, plugin)
			continue
		}
		if desiredPlugin == nil {
			changed = true
			continue
		}
		result = append(result, *desiredPlugin)
		found = true
		if !apiequality.Semantic.DeepEqual(plugin, *desiredPlugin) {
			changed = true
		}
	}
	if desiredPlugin != nil && !found {
		result = append(result, *desiredPlugin)
		changed = true
	}
	return result, changed
}

func reconcileStorage(existing, desired *cnpgv1.StorageConfiguration) (bool, error) {
	changed := false
	if desired.Size != "" {
		desiredQuantity, err := resource.ParseQuantity(desired.Size)
		if err != nil {
			return false, fmt.Errorf("storage size %q is invalid: %w", desired.Size, err)
		}
		if existing.Size != "" {
			existingQuantity, err := resource.ParseQuantity(existing.Size)
			if err != nil {
				return false, fmt.Errorf("existing storage size is invalid: %w", err)
			}
			if desiredQuantity.Cmp(existingQuantity) < 0 {
				return false, fmt.Errorf("storage shrink from %s to %s is not supported", existing.Size, desired.Size)
			}
		}
		if existing.Size != desired.Size {
			existing.Size = desired.Size
			changed = true
		}
	}
	if desired.StorageClass != nil && !apiequality.Semantic.DeepEqual(existing.StorageClass, desired.StorageClass) {
		existing.StorageClass = desired.StorageClass
		changed = true
	}
	if desired.ResizeInUseVolumes != nil && !apiequality.Semantic.DeepEqual(existing.ResizeInUseVolumes, desired.ResizeInUseVolumes) {
		existing.ResizeInUseVolumes = desired.ResizeInUseVolumes
		changed = true
	}
	if desired.PersistentVolumeClaimTemplate != nil && !apiequality.Semantic.DeepEqual(existing.PersistentVolumeClaimTemplate, desired.PersistentVolumeClaimTemplate) {
		existing.PersistentVolumeClaimTemplate = desired.PersistentVolumeClaimTemplate
		changed = true
	}
	return changed, nil
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

// reconcileServices deploys all Supabase services from the validated project
// credential projection.
func (r *SupabaseProjectReconciler) reconcileServices(ctx context.Context, project *supabasev1alpha1.SupabaseProject, credentials *secrets.ProjectCredentials) error {
	log := logf.FromContext(ctx)
	log.Info("Reconciling Supabase services")

	secretNames := &project.Status.SecretNames
	credentialHash := credentials.PodTemplateHash

	// Deploy Auth (GoTrue)
	if err := r.reconcileAuth(ctx, project, secretNames, credentialHash); err != nil {
		return r.failCoreServices(ctx, project, err)
	}

	// Deploy REST (PostgREST)
	if err := r.reconcileRest(ctx, project, secretNames, credentialHash); err != nil {
		return r.failCoreServices(ctx, project, err)
	}

	// Deploy Studio
	if err := r.reconcileStudio(ctx, project, secretNames, credentialHash); err != nil {
		return r.failCoreServices(ctx, project, err)
	}

	// Deploy Meta (postgres-meta)
	// postgres-meta consumes no project credential material, so credential
	// rotation must not roll this deployment.
	if err := r.reconcileMeta(ctx, project, secretNames); err != nil {
		return r.failCoreServices(ctx, project, err)
	}

	// Deploy Envoy gateway
	if err := r.reconcileGateway(ctx, project, credentialHash); err != nil {
		return r.failCoreServices(ctx, project, err)
	}

	if !coreServicesReady(project) {
		project.Status.Phase = supabasev1alpha1.PhaseProvisioning
		r.setCondition(project, supabasev1alpha1.ConditionTypeReady, metav1.ConditionFalse, "CoreServicesPending", "Waiting for all core service deployments to become ready")
		if err := r.updateProjectStatus(ctx, project); err != nil {
			return err
		}
	}

	return nil
}

// failCoreServices records the aggregate failure state before returning the
// original component reconciliation error to the controller runtime.
func (r *SupabaseProjectReconciler) failCoreServices(ctx context.Context, project *supabasev1alpha1.SupabaseProject, reconcileErr error) error {
	project.Status.Phase = supabasev1alpha1.PhaseProvisioning
	r.setCondition(project, supabasev1alpha1.ConditionTypeReady, metav1.ConditionFalse, "CoreServicesFailed", fmt.Sprintf("Core service reconciliation failed: %v", reconcileErr))
	if err := r.updateProjectStatus(ctx, project); err != nil {
		logf.FromContext(ctx).Error(err, "persisting core service failure status")
	}
	return reconcileErr
}

// reconcileServiceComponent is a generic helper for reconciling a service component (deployment + service)
func (r *SupabaseProjectReconciler) reconcileServiceComponent(ctx context.Context, project *supabasev1alpha1.SupabaseProject, config serviceReconcileConfig) error {
	log := logf.FromContext(ctx)
	log.Info(fmt.Sprintf("Reconciling %s service", config.name))

	// Create deployment
	deployment := config.buildDeployment()
	if config.credentialHash != "" {
		applyProjectCredentialsHash(deployment, config.credentialHash)
	}
	log.V(1).Info(fmt.Sprintf("Built %s deployment", config.name), config.logFields...)
	if err := r.createOrUpdateDeployment(ctx, project, deployment); err != nil {
		config.setStatus(supabasev1alpha1.ServiceStatus{})
		r.setCondition(project, config.conditionType, metav1.ConditionFalse, "DeploymentFailed", err.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return statusErr
		}
		return err
	}

	// Create service
	service := config.buildService()
	if err := r.createOrUpdateService(ctx, project, service); err != nil {
		config.setStatus(supabasev1alpha1.ServiceStatus{})
		r.setCondition(project, config.conditionType, metav1.ConditionFalse, "ServiceFailed", err.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return statusErr
		}
		return err
	}

	ready, availableReplicas, err := r.deploymentStatus(ctx, deployment)
	if err != nil {
		config.setStatus(supabasev1alpha1.ServiceStatus{})
		r.setCondition(project, config.conditionType, metav1.ConditionFalse, "DeploymentStatusFailed", err.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return statusErr
		}
		return err
	}

	config.setStatus(supabasev1alpha1.ServiceStatus{Ready: ready, AvailableReplicas: availableReplicas})
	if !ready {
		r.setCondition(project, config.conditionType, metav1.ConditionFalse, "DeploymentPending", fmt.Sprintf("Waiting for %s deployment to become ready", config.name))
		return nil
	}
	r.setCondition(project, config.conditionType, metav1.ConditionTrue, "Ready", fmt.Sprintf("%s service is running", config.name))
	return nil
}

func applyProjectCredentialsHash(deployment *appsv1.Deployment, hash string) {
	if deployment == nil || hash == "" {
		return
	}
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	deployment.Spec.Template.Annotations[projectCredentialsHashAnnotation] = hash
}

// reconcileAuth deploys the Auth service
func (r *SupabaseProjectReconciler) reconcileAuth(ctx context.Context, project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus, credentialHash string) error {
	if err := r.validateGoTrueEnvSources(ctx, project); err != nil {
		return r.failAuthGoTrueEnv(ctx, project, err)
	}
	deployment, err := r.buildAuthDeployment(ctx, project, secretNames)
	if err != nil {
		return err
	}
	config := newServiceReconcileConfig(
		"Auth",
		supabasev1alpha1.ConditionTypeAuthReady,
		func() *appsv1.Deployment { return deployment },
		func() *corev1.Service { return services.BuildAuthService(project) },
		func(status supabasev1alpha1.ServiceStatus) { project.Status.Services.Auth = status },
	)
	config.logFields = []any{
		"image", fmt.Sprintf("%s:%s", "supabase/gotrue", project.Spec.Auth.ImageTag),
		"replicas", project.Spec.Auth.Replicas,
		"hasProviders", project.Spec.Auth.Providers != nil,
		"hasEmailHook", project.Spec.Auth.EmailHook != nil && project.Spec.Auth.EmailHook.Enabled,
	}
	config.credentialHash = credentialHash
	return r.reconcileServiceComponent(ctx, project, config)
}

func (r *SupabaseProjectReconciler) validateGoTrueEnvSources(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	for _, configured := range project.Spec.Auth.GoTrueEnv {
		if deployments.IsOperatorOwnedGoTrueEnv(configured.Name) {
			return fmt.Errorf("GoTrue environment variable %q is operator-owned", configured.Name)
		}
		if configured.Value != nil {
			continue
		}
		valueFrom := configured.ValueFrom
		if valueFrom == nil {
			return fmt.Errorf("GoTrue environment variable %q must select exactly one Secret or ConfigMap key", configured.Name)
		}
		if (valueFrom.SecretKeyRef == nil) == (valueFrom.ConfigMapKeyRef == nil) {
			return fmt.Errorf("GoTrue environment variable %q must select exactly one Secret or ConfigMap key", configured.Name)
		}
		if ref := valueFrom.SecretKeyRef; ref != nil {
			if err := r.validateGoTrueEnvSecretKey(ctx, project.Namespace, ref); err != nil {
				return fmt.Errorf("GoTrue environment variable %q: %w", configured.Name, err)
			}
			continue
		}
		if err := r.validateGoTrueEnvConfigMapKey(ctx, project.Namespace, valueFrom.ConfigMapKeyRef); err != nil {
			return fmt.Errorf("GoTrue environment variable %q: %w", configured.Name, err)
		}
	}
	return nil
}

func (r *SupabaseProjectReconciler) validateGoTrueEnvSecretKey(ctx context.Context, namespace string, ref *supabasev1alpha1.GoTrueEnvKeySelector) error {
	if ref.Name == "" || ref.Key == "" {
		return fmt.Errorf("secret reference must include a name and key")
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, secret); err != nil {
		if ref.Optional != nil && *ref.Optional && apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("getting secret %q: %w", ref.Name, err)
	}
	_, exists := secret.Data[ref.Key]
	if !exists {
		_, exists = secret.StringData[ref.Key]
	}
	if !exists && (ref.Optional == nil || !*ref.Optional) {
		return fmt.Errorf("secret %q is missing key %q", ref.Name, ref.Key)
	}
	return nil
}

func (r *SupabaseProjectReconciler) validateGoTrueEnvConfigMapKey(ctx context.Context, namespace string, ref *supabasev1alpha1.GoTrueEnvKeySelector) error {
	if ref.Name == "" || ref.Key == "" {
		return fmt.Errorf("config map reference must include a name and key")
	}
	configMap := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, configMap); err != nil {
		if ref.Optional != nil && *ref.Optional && apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("getting config map %q: %w", ref.Name, err)
	}
	if _, exists := configMap.Data[ref.Key]; !exists && (ref.Optional == nil || !*ref.Optional) {
		return fmt.Errorf("config map %q is missing key %q", ref.Name, ref.Key)
	}
	return nil
}

func (r *SupabaseProjectReconciler) failAuthGoTrueEnv(ctx context.Context, project *supabasev1alpha1.SupabaseProject, reconcileErr error) error {
	project.Status.Services.Auth = supabasev1alpha1.ServiceStatus{Ready: false}
	r.setCondition(project, supabasev1alpha1.ConditionTypeAuthReady, metav1.ConditionFalse, "GoTrueEnvSourceInvalid", reconcileErr.Error())
	if err := r.updateProjectStatus(ctx, project); err != nil {
		return err
	}
	return reconcileErr
}

func (r *SupabaseProjectReconciler) buildAuthDeployment(ctx context.Context, project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) (*appsv1.Deployment, error) {
	deployment := deployments.BuildAuthDeployment(project, secretNames)
	hook := project.Spec.Auth.EmailHook
	if hook == nil || !hook.Enabled {
		return deployment, nil
	}

	secretName := secretNames.EmailHook
	if secretName == "" {
		secretName = secrets.EmailHookSecretName(project)
	}
	hookSecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: project.Namespace}, hookSecret); err != nil {
		return nil, fmt.Errorf("getting email hook secret %s: %w", secretName, err)
	}
	if err := secrets.ValidateEmailHookSecret(hookSecret); err != nil {
		return nil, err
	}

	hash := sha256.Sum256(hookSecret.Data[secrets.EmailHookSecretKey])
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	deployment.Spec.Template.Annotations[emailHookSecretHashAnnotation] = hex.EncodeToString(hash[:])
	return deployment, nil
}

// reconcileRest deploys the REST service
func (r *SupabaseProjectReconciler) reconcileRest(ctx context.Context, project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus, credentialHash string) error {
	config := newServiceReconcileConfig(
		"REST",
		supabasev1alpha1.ConditionTypeRestReady,
		func() *appsv1.Deployment { return deployments.BuildRestDeployment(project, secretNames) },
		func() *corev1.Service { return services.BuildRestService(project) },
		func(status supabasev1alpha1.ServiceStatus) { project.Status.Services.Rest = status },
	)
	config.logFields = []any{
		"image", fmt.Sprintf("%s:%s", "postgrest/postgrest", project.Spec.Rest.ImageTag),
		"schemas", project.Spec.Rest.Schemas,
	}
	config.credentialHash = credentialHash
	return r.reconcileServiceComponent(ctx, project, config)
}

// reconcileStudio deploys the Studio service
func (r *SupabaseProjectReconciler) reconcileStudio(ctx context.Context, project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus, credentialHash string) error {
	config := newServiceReconcileConfig(
		"Studio",
		supabasev1alpha1.ConditionTypeStudioReady,
		func() *appsv1.Deployment { return deployments.BuildStudioDeployment(project, secretNames) },
		func() *corev1.Service { return services.BuildStudioService(project) },
		func(status supabasev1alpha1.ServiceStatus) { project.Status.Services.Studio = status },
	)
	config.logFields = []any{
		"image", fmt.Sprintf("%s:%s", "supabase/studio", project.Spec.Studio.ImageTag),
		"publicURL", project.Spec.Studio.PublicURL,
	}
	config.credentialHash = credentialHash
	return r.reconcileServiceComponent(ctx, project, config)
}

// reconcileMeta deploys the Meta service
func (r *SupabaseProjectReconciler) reconcileMeta(ctx context.Context, project *supabasev1alpha1.SupabaseProject, secretNames *supabasev1alpha1.SecretNamesStatus) error {
	config := newServiceReconcileConfig(
		"Meta",
		supabasev1alpha1.ConditionTypeMetaReady,
		func() *appsv1.Deployment { return deployments.BuildMetaDeployment(project, secretNames) },
		func() *corev1.Service { return services.BuildMetaService(project) },
		func(status supabasev1alpha1.ServiceStatus) { project.Status.Services.Meta = status },
	)
	config.logFields = []any{
		"image", fmt.Sprintf("%s:%s", "supabase/postgres-meta", project.Spec.Meta.ImageTag),
	}
	return r.reconcileServiceComponent(ctx, project, config)
}

// reconcileGateway deploys the Envoy API gateway and its managed assets.
func (r *SupabaseProjectReconciler) reconcileGateway(ctx context.Context, project *supabasev1alpha1.SupabaseProject, credentialHash string) error {
	log := logf.FromContext(ctx)
	log.Info("Reconciling Envoy gateway service")

	// Create Envoy config and public-key ConfigMaps.
	envoyConfig := configmaps.BuildEnvoyConfigMap(project)
	log.V(1).Info("Built Envoy ConfigMap", "name", envoyConfig.Name)
	if err := r.createOrUpdateConfigMap(ctx, project, envoyConfig); err != nil {
		project.Status.Services.Gateway = supabasev1alpha1.ServiceStatus{}
		r.setCondition(project, supabasev1alpha1.ConditionTypeGatewayReady, metav1.ConditionFalse, "ConfigMapFailed", err.Error())
		if statusErr := r.updateProjectStatus(ctx, project); statusErr != nil {
			return statusErr
		}
		return err
	}

	config := newServiceReconcileConfig(
		"Gateway",
		supabasev1alpha1.ConditionTypeGatewayReady,
		func() *appsv1.Deployment { return deployments.BuildGatewayDeployment(project) },
		func() *corev1.Service { return services.BuildGatewayService(project) },
		func(status supabasev1alpha1.ServiceStatus) { project.Status.Services.Gateway = status },
	)
	config.credentialHash = credentialHash
	config.logFields = []any{
		"image", fmt.Sprintf("%s:%s", defaults.EnvoyImage, project.Spec.Gateway.ImageTag),
		"replicas", project.Spec.Gateway.Replicas,
	}
	if err := r.reconcileServiceComponent(ctx, project, config); err != nil {
		return err
	}

	// Update API endpoint in status after the common service reconciliation.
	project.Status.Endpoints.API = fmt.Sprintf("%s:8000", common.GatewayName(project))
	return nil
}

// Helper methods

const supabaseProjectKind = "SupabaseProject"

// removeSupabaseProjectOwnerReference removes only the owner reference for
// this operator's SupabaseProject API type and name. The UID is intentionally
// ignored so a recreated same-name project can repair a stale reference, while
// same-name owners from another API group remain untouched.
func removeSupabaseProjectOwnerReference(object metav1.Object, projectName string) bool {
	if projectName == "" {
		return false
	}
	owners := object.GetOwnerReferences()
	if len(owners) == 0 {
		return false
	}
	filtered := owners[:0]
	changed := false
	for _, owner := range owners {
		ownerGroupVersion, err := schema.ParseGroupVersion(owner.APIVersion)
		if err == nil && ownerGroupVersion.Group == supabasev1alpha1.GroupVersion.Group && owner.Kind == supabaseProjectKind && owner.Name == projectName {
			changed = true
			continue
		}
		filtered = append(filtered, owner)
	}
	if changed {
		object.SetOwnerReferences(filtered)
	}
	return changed
}

func mergeLabels(object metav1.Object, desired map[string]string) bool {
	if len(desired) == 0 {
		return false
	}
	labels := object.GetLabels()
	if labels == nil {
		labels = make(map[string]string, len(desired))
		object.SetLabels(labels)
	}
	changed := false
	for key, value := range desired {
		if labels[key] != value {
			labels[key] = value
			changed = true
		}
	}
	return changed
}

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

	// Secret exists - don't overwrite data (preserve generated passwords), but
	// ensure an implementation Secret is owned by the current project after a
	// same-name project recreation.
	ownerRefs := append([]metav1.OwnerReference(nil), existing.OwnerReferences...)
	if err := controllerutil.SetControllerReference(project, existing, r.Scheme); err != nil {
		return err
	}
	if !apiequality.Semantic.DeepEqual(ownerRefs, existing.OwnerReferences) {
		return r.Update(ctx, existing)
	}
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

	ownerRefs := append([]metav1.OwnerReference(nil), existing.OwnerReferences...)
	if err := controllerutil.SetControllerReference(project, existing, r.Scheme); err != nil {
		return err
	}
	ownerChanged := !apiequality.Semantic.DeepEqual(ownerRefs, existing.OwnerReferences)
	labelsChanged := mergeLabels(existing, configMap.Labels)
	// Update existing
	if apiequality.Semantic.DeepEqual(existing.Data, configMap.Data) && !ownerChanged && !labelsChanged {
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
	ownerRefs := append([]metav1.OwnerReference(nil), existing.OwnerReferences...)
	if err := controllerutil.SetControllerReference(project, existing, r.Scheme); err != nil {
		return err
	}

	desired := deployment.DeepCopy()
	actual := existing.DeepCopy()
	normalizePodTemplateDefaults(&desired.Spec.Template)
	normalizePodTemplateDefaults(&actual.Spec.Template)
	ownerChanged := !apiequality.Semantic.DeepEqual(ownerRefs, existing.OwnerReferences)
	if apiequality.Semantic.DeepEqual(actual.Spec.Replicas, desired.Spec.Replicas) &&
		apiequality.Semantic.DeepEqual(actual.Spec.Template, desired.Spec.Template) && !ownerChanged {
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
	ownerRefs := append([]metav1.OwnerReference(nil), existing.OwnerReferences...)
	if err := controllerutil.SetControllerReference(project, existing, r.Scheme); err != nil {
		return err
	}

	// Update existing (preserve ClusterIP)
	ownerChanged := !apiequality.Semantic.DeepEqual(ownerRefs, existing.OwnerReferences)
	if apiequality.Semantic.DeepEqual(existing.Spec.Ports, service.Spec.Ports) &&
		apiequality.Semantic.DeepEqual(existing.Spec.Selector, service.Spec.Selector) && !ownerChanged {
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

// reconcileDurableMetadataProtection runs before any externally supplied
// credential or backup validation. Durable resources must not retain a
// SupabaseProject owner reference that could garbage-collect the database on
// an upgrade, but only deterministic, correctly labelled resources are safe to
// touch. Foreign and unlabeled same-name resources are left untouched for the
// normal adoption guard to reject later.
func (r *SupabaseProjectReconciler) reconcileDurableMetadataProtection(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	durables := []struct {
		name   string
		object client.Object
	}{
		{name: cnpg.ClusterName(project), object: &cnpgv1.Cluster{}},
		{name: cnpg.ObjectStoreName(project), object: &barmancloudv1.ObjectStore{}},
		{name: cnpg.RecoveryObjectStoreName(project), object: &barmancloudv1.ObjectStore{}},
		{name: cnpg.ScheduledBackupName(project), object: &cnpgv1.ScheduledBackup{}},
	}
	for _, durable := range durables {
		key := types.NamespacedName{Name: durable.name, Namespace: project.Namespace}
		if err := r.Get(ctx, key, durable.object); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("getting durable resource %s: %w", key.String(), err)
		}
		if durable.object.GetLabels()[common.LabelInstance] != project.Name {
			continue
		}
		if !removeSupabaseProjectOwnerReference(durable.object, project.Name) {
			continue
		}
		if err := r.Update(ctx, durable.object); err != nil {
			return fmt.Errorf("removing SupabaseProject owner from durable resource %s: %w", key.String(), err)
		}
	}
	return nil
}

// validateRecoveryBootstrapIntent checks immutable recovery state before
// backup/recovery reconciliation can update an ObjectStore. This preserves
// the source identity used by an existing CNPG cluster, including the
// explicit plugin fields serverName and barmanObjectName.
func (r *SupabaseProjectReconciler) validateRecoveryBootstrapIntent(ctx context.Context, project *supabasev1alpha1.SupabaseProject) error {
	desired := cnpg.BuildCluster(project, &project.Status.SecretNames)
	existing := &cnpgv1.Cluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("getting CNPG cluster for recovery validation: %w", err)
	}
	recoveryEnabled := project.Spec.Database.Recovery != nil && project.Spec.Database.Recovery.Enabled
	// Bootstrap mode is immutable for every existing cluster. Compare it even
	// when recovery is being disabled so cleanup cannot remove the source
	// ObjectStore before CNPG reports the recovery-to-initdb change.
	if recoveryBootstrapIntentChanged(existing, desired) {
		return fmt.Errorf("CNPG recovery bootstrap configuration is immutable after cluster creation")
	}

	// A recovered cluster's source ObjectStore location is also creation-time
	// input. Its operational credential Secret may rotate, so compare only the
	// destination and endpoint before the immutable cluster check runs.
	if recoveryEnabled {
		desiredStore := cnpg.BuildRecoveryObjectStore(project)
		currentStore := &barmancloudv1.ObjectStore{}
		err := r.Get(ctx, types.NamespacedName{Name: desiredStore.Name, Namespace: desiredStore.Namespace}, currentStore)
		if err == nil {
			if recoveryObjectStoreIdentityChanged(currentStore, desiredStore) {
				return fmt.Errorf("CNPG recovery source ObjectStore configuration is immutable after cluster creation")
			}
		} else if !apierrors.IsNotFound(err) {
			return fmt.Errorf("getting recovery source ObjectStore for validation: %w", err)
		}
	}
	return nil
}

func recoveryObjectStoreIdentityChanged(existing, desired *barmancloudv1.ObjectStore) bool {
	if existing == nil || desired == nil {
		return false
	}
	existingConfig := existing.Spec.Configuration
	desiredConfig := desired.Spec.Configuration
	return existingConfig.EndpointURL != desiredConfig.EndpointURL ||
		existingConfig.DestinationPath != desiredConfig.DestinationPath
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
	if err := r.createOrUpdateObjectStore(ctx, objectStore); err != nil {
		return r.failBackup(ctx, project, "ObjectStoreFailed", err)
	}

	// Build and create/update ScheduledBackup
	scheduledBackup := cnpg.BuildScheduledBackup(project)
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
		if scheduledBackup.Labels[common.LabelInstance] != project.Name {
			return fmt.Errorf("refusing to delete ScheduledBackup %s with instance label %q", scheduledBackupName, scheduledBackup.Labels[common.LabelInstance])
		}
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
		if objectStore.Labels[common.LabelInstance] != project.Name {
			return fmt.Errorf("refusing to delete ObjectStore %s with instance label %q", objectStoreName, objectStore.Labels[common.LabelInstance])
		}
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
		if objectStore.Labels[common.LabelInstance] != project.Name {
			return fmt.Errorf("refusing to delete recovery ObjectStore %s with instance label %q", objectStoreName, objectStore.Labels[common.LabelInstance])
		}
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
	if objectStore == nil {
		return nil
	}
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

	projectName := objectStore.Labels[common.LabelInstance]
	if projectName == "" {
		return fmt.Errorf("ObjectStore %s has no project instance label", existing.Name)
	}
	if instance := existing.Labels[common.LabelInstance]; instance != projectName {
		return fmt.Errorf("ObjectStore %s has instance label %q, expected %q", existing.Name, instance, projectName)
	}
	changed := removeSupabaseProjectOwnerReference(existing, projectName)
	if mergeLabels(existing, objectStore.Labels) {
		changed = true
	}
	desiredConfiguration := objectStore.Spec.Configuration
	currentConfiguration := existing.Spec.Configuration
	configurationChanged := !apiequality.Semantic.DeepEqual(currentConfiguration.BarmanCredentials, desiredConfiguration.BarmanCredentials) ||
		currentConfiguration.EndpointURL != desiredConfiguration.EndpointURL ||
		currentConfiguration.DestinationPath != desiredConfiguration.DestinationPath ||
		(desiredConfiguration.Wal != nil && !apiequality.Semantic.DeepEqual(currentConfiguration.Wal, desiredConfiguration.Wal)) ||
		(desiredConfiguration.Data != nil && !apiequality.Semantic.DeepEqual(currentConfiguration.Data, desiredConfiguration.Data))
	if configurationChanged || existing.Spec.RetentionPolicy != objectStore.Spec.RetentionPolicy {
		changed = true
	}

	// Update only fields owned by this controller. The ObjectStore CRD defaults
	// instanceSidecarConfiguration, so preserve that API-managed field.
	currentConfiguration.BarmanCredentials = desiredConfiguration.BarmanCredentials
	currentConfiguration.EndpointURL = desiredConfiguration.EndpointURL
	currentConfiguration.DestinationPath = desiredConfiguration.DestinationPath
	if desiredConfiguration.Wal != nil {
		currentConfiguration.Wal = desiredConfiguration.Wal
	}
	if desiredConfiguration.Data != nil {
		currentConfiguration.Data = desiredConfiguration.Data
	}
	existing.Spec.Configuration = currentConfiguration
	existing.Spec.RetentionPolicy = objectStore.Spec.RetentionPolicy
	if !changed {
		return nil
	}
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update ObjectStore: %w", err)
	}
	return nil
}

// createOrUpdateScheduledBackup creates or updates a ScheduledBackup resource
func (r *SupabaseProjectReconciler) createOrUpdateScheduledBackup(ctx context.Context, scheduledBackup *cnpgv1.ScheduledBackup) error {
	if scheduledBackup == nil {
		return nil
	}
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

	projectName := scheduledBackup.Labels[common.LabelInstance]
	if projectName == "" {
		return fmt.Errorf("ScheduledBackup %s has no project instance label", existing.Name)
	}
	if instance := existing.Labels[common.LabelInstance]; instance != projectName {
		return fmt.Errorf("ScheduledBackup %s has instance label %q, expected %q", existing.Name, instance, projectName)
	}
	changed := removeSupabaseProjectOwnerReference(existing, projectName)
	if mergeLabels(existing, scheduledBackup.Labels) {
		changed = true
	}
	ownedSpecChanged := existing.Spec.Schedule != scheduledBackup.Spec.Schedule ||
		!apiequality.Semantic.DeepEqual(existing.Spec.Cluster, scheduledBackup.Spec.Cluster) ||
		existing.Spec.BackupOwnerReference != scheduledBackup.Spec.BackupOwnerReference ||
		existing.Spec.Method != scheduledBackup.Spec.Method ||
		!apiequality.Semantic.DeepEqual(existing.Spec.PluginConfiguration, scheduledBackup.Spec.PluginConfiguration)
	if ownedSpecChanged {
		changed = true
	}

	// Update only the fields this operator owns, preserving CNPG defaults and
	// any caller-managed scheduling knobs.
	existing.Spec.Schedule = scheduledBackup.Spec.Schedule
	existing.Spec.Cluster = scheduledBackup.Spec.Cluster
	existing.Spec.BackupOwnerReference = scheduledBackup.Spec.BackupOwnerReference
	existing.Spec.Method = scheduledBackup.Spec.Method
	existing.Spec.PluginConfiguration = scheduledBackup.Spec.PluginConfiguration
	if !changed {
		return nil
	}
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

	apiReady, apiAvailable, err := r.deploymentStatus(ctx, apiDeployment)
	if err != nil {
		return ctrl.Result{}, err
	}
	replicationReady, replicationAvailable, err := r.deploymentStatus(ctx, replDeployment)
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
	for _, ownedResource := range resources {
		if err := r.deletePowerSyncOwnedResource(ctx, project, ownedResource); err != nil {
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

func (r *SupabaseProjectReconciler) deletePowerSyncOwnedResource(ctx context.Context, project *supabasev1alpha1.SupabaseProject, object client.Object) error {
	if err := r.Get(ctx, client.ObjectKeyFromObject(object), object); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !metav1.IsControlledBy(object, project) {
		return nil
	}
	return client.IgnoreNotFound(r.Delete(ctx, object))
}

func (r *SupabaseProjectReconciler) deploymentStatus(ctx context.Context, desired *appsv1.Deployment) (bool, int32, error) {
	if desired == nil {
		return false, 0, nil
	}
	existing := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing); err != nil {
		if apierrors.IsNotFound(err) {
			return false, 0, nil
		}
		return false, 0, err
	}
	return deploymentIsReady(existing), existing.Status.AvailableReplicas, nil
}

func deploymentIsReady(deployment *appsv1.Deployment) bool {
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

func coreServicesReady(project *supabasev1alpha1.SupabaseProject) bool {
	serviceStatus := project.Status.Services
	return serviceStatus.Auth.Ready &&
		serviceStatus.Rest.Ready &&
		serviceStatus.Studio.Ready &&
		serviceStatus.Meta.Ready &&
		serviceStatus.Gateway.Ready
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
	ownerRefs := append([]metav1.OwnerReference(nil), existing.OwnerReferences...)
	if err := controllerutil.SetControllerReference(project, existing, r.Scheme); err != nil {
		return err
	}

	desired := cronJob.DeepCopy()
	actual := existing.DeepCopy()
	normalizeCronJobDefaults(&desired.Spec)
	normalizeCronJobDefaults(&actual.Spec)
	ownerChanged := !apiequality.Semantic.DeepEqual(ownerRefs, existing.OwnerReferences)
	labelsChanged := mergeLabels(existing, cronJob.Labels)
	if apiequality.Semantic.DeepEqual(actual.Spec, desired.Spec) && !ownerChanged && !labelsChanged {
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
	ownerRefs := append([]metav1.OwnerReference(nil), existing.OwnerReferences...)
	if err := controllerutil.SetControllerReference(project, existing, r.Scheme); err != nil {
		return false, err
	}
	ownerChanged := !apiequality.Semantic.DeepEqual(ownerRefs, existing.OwnerReferences)
	labelsChanged := mergeLabels(existing, job.Labels)

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
	if ownerChanged || labelsChanged {
		if err := r.Update(ctx, existing); err != nil {
			return false, fmt.Errorf("updating CDC permissions Job metadata: %w", err)
		}
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

// mapProjectCredentialsSecretToProjects maps an externally managed credential
// Secret to only the projects in the same namespace that reference it. The
// Secret is intentionally not owned by any project.
func (r *SupabaseProjectReconciler) mapProjectCredentialsSecretToProjects(ctx context.Context, object client.Object) []reconcile.Request {
	projects := &supabasev1alpha1.SupabaseProjectList{}
	if err := r.List(ctx, projects, client.InNamespace(object.GetNamespace())); err != nil {
		logf.FromContext(ctx).Error(err, "listing SupabaseProjects for credential Secret", "secret", object.GetName())
		return nil
	}
	requests := make([]reconcile.Request, 0)
	for i := range projects.Items {
		project := &projects.Items[i]
		if project.Spec.ProjectCredentialsSecret == object.GetName() {
			requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Name: project.Name, Namespace: project.Namespace}})
		}
	}
	return requests
}

// mapDurableResourceToProject maps retained CNPG and backup resources by their
// deterministic name and instance label. It never scans or adopts resources
// from another namespace/project.
func (r *SupabaseProjectReconciler) mapDurableResourceToProject(ctx context.Context, object client.Object) []reconcile.Request {
	instance := object.GetLabels()[common.LabelInstance]
	if instance == "" {
		return nil
	}
	project := &supabasev1alpha1.SupabaseProject{}
	key := types.NamespacedName{Name: instance, Namespace: object.GetNamespace()}
	if err := r.Get(ctx, key, project); err != nil {
		if !apierrors.IsNotFound(err) {
			logf.FromContext(ctx).Error(err, "getting project for durable resource", "resource", object.GetName())
		}
		return nil
	}
	belongs := false
	switch typed := object.(type) {
	case *cnpgv1.Cluster:
		belongs = typed.Name == cnpg.ClusterName(project)
	case *cnpgv1.ScheduledBackup:
		belongs = typed.Name == cnpg.ScheduledBackupName(project)
	case *barmancloudv1.ObjectStore:
		belongs = typed.Name == cnpg.ObjectStoreName(project) || typed.Name == cnpg.RecoveryObjectStoreName(project)
	default:
		return nil
	}
	if !belongs {
		return nil
	}
	return []reconcile.Request{{NamespacedName: key}}
}

// SetupWithManager sets up the controller with the Manager.
func (r *SupabaseProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&supabasev1alpha1.SupabaseProject{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.mapPowerSyncConfigMapToProjects)).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.mapProjectCredentialsSecretToProjects)).
		Owns(&corev1.Service{}).
		Owns(&appsv1.Deployment{}).
		Owns(&batchv1.Job{}).
		Owns(&batchv1.CronJob{}).
		Owns(&cnpgv1.Publication{}).
		Watches(&cnpgv1.Cluster{}, handler.EnqueueRequestsFromMapFunc(r.mapDurableResourceToProject)).
		Watches(&cnpgv1.ScheduledBackup{}, handler.EnqueueRequestsFromMapFunc(r.mapDurableResourceToProject)).
		Watches(&barmancloudv1.ObjectStore{}, handler.EnqueueRequestsFromMapFunc(r.mapDurableResourceToProject)).
		Named("supabaseproject").
		Complete(r)
}
