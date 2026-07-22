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

package v1alpha1

import (
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Phase constants
const (
	// PhasePending indicates the project is waiting to be processed
	PhasePending = "Pending"

	// PhaseProvisioning indicates resources are being created
	PhaseProvisioning = "Provisioning"

	// PhaseRunning indicates all components are running
	PhaseRunning = "Running"

	// PhaseFailed indicates an error occurred
	PhaseFailed = "Failed"

	// PhaseDeleting indicates the project is being deleted
	PhaseDeleting = "Deleting"
)

// CompressionType defines the compression algorithm for backups
// +kubebuilder:validation:Enum=gzip;bzip2;snappy;none
type CompressionType string

const (
	// CompressionTypeGzip uses gzip compression (default)
	CompressionTypeGzip CompressionType = "gzip"
	// CompressionTypeBzip2 uses bzip2 compression
	CompressionTypeBzip2 CompressionType = "bzip2"
	// CompressionTypeSnappy uses snappy compression
	CompressionTypeSnappy CompressionType = "snappy"
	// CompressionTypeNone disables compression
	CompressionTypeNone CompressionType = "none"
)

// Condition type constants
const (
	// ConditionTypeReady indicates overall readiness
	ConditionTypeReady = "Ready"

	// ConditionTypeSecretsReady indicates secrets are generated
	ConditionTypeSecretsReady = "SecretsReady"

	// ConditionTypeDatabaseReady indicates CNPG cluster is ready
	ConditionTypeDatabaseReady = "DatabaseReady"

	// ConditionTypeSchemaInitialized indicates init SQL has run
	ConditionTypeSchemaInitialized = "SchemaInitialized"

	// ConditionTypeAuthReady indicates GoTrue is ready
	ConditionTypeAuthReady = "AuthReady"

	// ConditionTypeRestReady indicates PostgREST is ready
	ConditionTypeRestReady = "RestReady"

	// ConditionTypeStudioReady indicates Studio is ready
	ConditionTypeStudioReady = "StudioReady"

	// ConditionTypeMetaReady indicates postgres-meta is ready
	ConditionTypeMetaReady = "MetaReady"

	// ConditionTypeKongReady indicates Kong is ready
	ConditionTypeKongReady = "KongReady"

	// ConditionTypeBackupReady indicates backup infrastructure is ready
	ConditionTypeBackupReady = "BackupReady"

	// ConditionTypeRecoveryReady indicates recovery infrastructure is ready
	ConditionTypeRecoveryReady = "RecoveryReady"

	// ConditionTypeCDCReady indicates CDC permissions have been applied
	ConditionTypeCDCReady = "CDCReady"

	// ConditionTypePowersyncReady indicates Powersync is ready
	ConditionTypePowersyncReady = "PowersyncReady"
)

// SupabaseProjectSpec defines the desired state of SupabaseProject
type SupabaseProjectSpec struct {
	// Database configuration for CNPG PostgreSQL cluster
	// +required
	Database DatabaseSpec `json:"database"`

	// Secrets configuration for migration support
	// When autoGenerate is false, user must provide all secret references
	// +optional
	Secrets *SecretsSpec `json:"secrets,omitempty"`

	// JWT configuration (auto-generated if not provided)
	// Deprecated: Use secrets.jwt instead
	// +optional
	JWT *JWTSpec `json:"jwt,omitempty"`

	// Auth service configuration (GoTrue)
	// +required
	Auth AuthSpec `json:"auth"`

	// Rest service configuration (PostgREST)
	// +optional
	Rest RestSpec `json:"rest,omitempty"`

	// Studio dashboard configuration
	// +optional
	Studio StudioSpec `json:"studio,omitempty"`

	// Meta service configuration (postgres-meta)
	// +optional
	Meta MetaSpec `json:"meta,omitempty"`

	// Kong API gateway configuration
	// +optional
	Kong KongSpec `json:"kong,omitempty"`

	// Powersync offline-first sync configuration (optional - presence enables Powersync)
	// +optional
	Powersync *PowersyncSpec `json:"powersync,omitempty"`

	// ImagePullSecrets for all deployments
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

// DatabaseSpec defines PostgreSQL configuration via CNPG
// +kubebuilder:validation:XValidation:rule="!(has(self.backup) && self.backup.enabled && has(self.recovery) && self.recovery.enabled)",message="backup and recovery cannot both be enabled"
type DatabaseSpec struct {
	// Instances is the number of PostgreSQL instances
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	Instances int32 `json:"instances"`

	// Storage configuration (uses CNPG StorageConfiguration directly)
	// +required
	Storage cnpgv1.StorageConfiguration `json:"storage"`

	// Image is the PostgreSQL image (default: ghcr.io/cloudnative-pg/postgresql:17)
	// +optional
	Image string `json:"image,omitempty"`

	// Resources for PostgreSQL pods
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// EnableSuperuserAccess allows connecting as postgres superuser
	// +kubebuilder:default=false
	EnableSuperuserAccess bool `json:"enableSuperuserAccess,omitempty"`

	// Parameters for PostgreSQL configuration
	// +optional
	Parameters map[string]string `json:"parameters,omitempty"`

	// AdditionalExtensions beyond the standard Supabase set
	// +optional
	AdditionalExtensions []string `json:"additionalExtensions,omitempty"`

	// Backup configuration
	// +optional
	Backup *BackupSpec `json:"backup,omitempty"`

	// Recovery configuration for point-in-time recovery from backups
	// +optional
	Recovery *RecoverySpec `json:"recovery,omitempty"`

	// AdditionalRoles beyond the roles managed by the operator
	// Uses CNPG RoleConfiguration directly for full compatibility
	// +optional
	AdditionalRoles []cnpgv1.RoleConfiguration `json:"additionalRoles,omitempty"`
}

// S3Config defines S3-compatible storage configuration
// This is embedded in BackupSpec and RecoverySpec to reduce duplication
type S3Config struct {
	// DestinationPath is the S3/R2 bucket path (e.g., s3://bucket-name/path/to/backups)
	// +kubebuilder:validation:Pattern=`^s3://[a-z0-9][a-z0-9.\-]*[a-z0-9](/.*)?$`
	// +optional
	DestinationPath string `json:"destinationPath,omitempty"`

	// EndpointURL for S3-compatible storage (e.g., https://account.r2.cloudflarestorage.com)
	// +optional
	EndpointURL string `json:"endpointURL,omitempty"`

	// S3CredentialsSecret references a secret with ACCESS_KEY_ID and SECRET_ACCESS_KEY
	// +optional
	S3CredentialsSecret string `json:"s3CredentialsSecret,omitempty"`
}

// BackupSpec defines backup configuration
// +kubebuilder:validation:XValidation:rule="!self.enabled || self.destinationPath.size() > 0",message="destinationPath is required when backup is enabled"
// +kubebuilder:validation:XValidation:rule="!self.enabled || self.s3CredentialsSecret.size() > 0",message="s3CredentialsSecret is required when backup is enabled"
type BackupSpec struct {
	// Enabled enables scheduled backups
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// Schedule in cron format (6 fields including seconds)
	// +kubebuilder:default="0 0 2 * * *"
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// RetentionPolicy defines how long to keep backups
	// +kubebuilder:default="30d"
	// +optional
	RetentionPolicy string `json:"retentionPolicy,omitempty"`

	// S3Config embeds S3 storage configuration
	S3Config `json:",inline"`

	// WalCompression algorithm for WAL archiving
	// +kubebuilder:default="gzip"
	// +optional
	WalCompression CompressionType `json:"walCompression,omitempty"`

	// DataCompression algorithm for base backups
	// +kubebuilder:default="gzip"
	// +optional
	DataCompression CompressionType `json:"dataCompression,omitempty"`
}

// RecoverySpec defines PITR recovery configuration
// +kubebuilder:validation:XValidation:rule="!self.enabled || self.serverName.size() > 0",message="serverName is required when recovery is enabled"
// +kubebuilder:validation:XValidation:rule="!self.enabled || self.destinationPath.size() > 0",message="destinationPath is required when recovery is enabled"
// +kubebuilder:validation:XValidation:rule="!self.enabled || self.s3CredentialsSecret.size() > 0",message="s3CredentialsSecret is required when recovery is enabled"
type RecoverySpec struct {
	// Enabled switches bootstrap from initdb to recovery mode
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// ServerName is the original cluster name in the backup
	// Required when recovery is enabled
	// +optional
	ServerName string `json:"serverName,omitempty"`

	// TargetTime for point-in-time recovery in RFC 3339 format
	// Examples: "2024-01-15T10:30:00Z", "2024-01-15T10:30:00+08:00"
	// If empty, recovers to the latest available point
	// +kubebuilder:validation:Pattern=`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`
	// +optional
	TargetTime string `json:"targetTime,omitempty"`

	// S3Config embeds S3 storage configuration
	S3Config `json:",inline"`
}

// JWTSpec defines JWT configuration
type JWTSpec struct {
	// SecretRef references an existing JWT secret
	// If not provided, a secret will be auto-generated
	// +optional
	SecretRef string `json:"secretRef,omitempty"`

	// ExpirationSeconds for generated tokens
	// +kubebuilder:default=3600
	// +optional
	ExpirationSeconds int `json:"expirationSeconds,omitempty"`
}

// SecretsSpec defines secrets configuration for migration support
// +kubebuilder:validation:XValidation:rule="self.autoGenerate || (self.jwt.size() > 0 && self.supabaseAdmin.size() > 0 && self.authenticator.size() > 0 && self.authAdmin.size() > 0)",message="all secret refs are required when autoGenerate is false"
type SecretsSpec struct {
	// AutoGenerate controls whether the operator generates secrets automatically.
	// Set to false when migrating from an existing cluster with pre-existing secrets.
	// +kubebuilder:default=true
	AutoGenerate bool `json:"autoGenerate"`

	// JWT references an existing JWT secret containing 'secret', 'anonKey', and 'serviceKey' keys.
	// Required when autoGenerate is false.
	// +optional
	JWT string `json:"jwt,omitempty"`

	// SupabaseAdmin references an existing secret containing 'username' and 'password' keys
	// for the supabase_admin database role.
	// Required when autoGenerate is false.
	// +optional
	SupabaseAdmin string `json:"supabaseAdmin,omitempty"`

	// Authenticator references an existing secret containing 'username' and 'password' keys
	// for the authenticator database role (used by PostgREST).
	// Required when autoGenerate is false.
	// +optional
	Authenticator string `json:"authenticator,omitempty"`

	// AuthAdmin references an existing secret containing 'username' and 'password' keys
	// for the supabase_auth_admin database role (used by GoTrue).
	// Required when autoGenerate is false.
	// +optional
	AuthAdmin string `json:"authAdmin,omitempty"`
}

// AuthSpec defines GoTrue auth service configuration
type AuthSpec struct {
	// Image tag for supabase/gotrue (defaults to operator version if empty)
	// +optional
	ImageTag string `json:"imageTag,omitempty"`

	// Replicas count
	// +kubebuilder:default=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// SiteURL is the public URL of your application
	// +required
	SiteURL string `json:"siteURL"`

	// ExternalURL is the public URL of the auth service
	// +required
	ExternalURL string `json:"externalURL"`

	// DisableSignup prevents new user registrations
	// +kubebuilder:default=false
	// +optional
	DisableSignup bool `json:"disableSignup,omitempty"`

	// AutoConfirmEmail enables automatic email confirmation
	// +kubebuilder:default=true
	// +optional
	AutoConfirmEmail bool `json:"autoConfirmEmail,omitempty"`

	// Providers configuration for OAuth
	// +optional
	Providers *AuthProvidersSpec `json:"providers,omitempty"`

	// EmailHook for custom email sending
	// +optional
	EmailHook *EmailHookSpec `json:"emailHook,omitempty"`

	// SMTP configuration
	// +optional
	SMTP *SMTPSpec `json:"smtp,omitempty"`

	// Resources for Auth pods
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// AuthProvidersSpec defines OAuth provider configuration
// +kubebuilder:validation:XValidation:rule="((!has(self.google) || !self.google.enabled) && (!has(self.apple) || !self.apple.enabled)) || self.secretRef.size() > 0",message="secretRef is required when Google or Apple provider is enabled"
type AuthProvidersSpec struct {
	// Google OAuth configuration
	// +optional
	Google *GoogleProviderSpec `json:"google,omitempty"`

	// Apple Sign-In configuration
	// +optional
	Apple *AppleProviderSpec `json:"apple,omitempty"`

	// SecretRef for provider credentials (contains env vars like GOTRUE_EXTERNAL_GOOGLE_CLIENT_ID)
	// Required when Google or Apple provider is enabled.
	// +optional
	SecretRef string `json:"secretRef,omitempty"`
}

// GoogleProviderSpec defines Google OAuth configuration
type GoogleProviderSpec struct {
	// Enabled enables Google OAuth
	Enabled bool `json:"enabled"`

	// SkipNonceCheck for Google One Tap
	// +optional
	SkipNonceCheck bool `json:"skipNonceCheck,omitempty"`
}

// AppleProviderSpec defines Apple Sign-In configuration
type AppleProviderSpec struct {
	// Enabled enables Apple Sign-In
	Enabled bool `json:"enabled"`
}

// EmailHookSpec defines email hook configuration
type EmailHookSpec struct {
	// Enabled enables the email hook
	Enabled bool `json:"enabled"`

	// URI is the webhook endpoint for email sending
	URI string `json:"uri"`
}

// SMTPSpec defines SMTP configuration
type SMTPSpec struct {
	// Host is the SMTP server hostname
	Host string `json:"host"`

	// Port is the SMTP server port
	Port int `json:"port"`

	// User is the SMTP username
	User string `json:"user"`

	// SecretRef references a secret containing the password key
	SecretRef string `json:"secretRef"`

	// SenderName is the display name for emails
	SenderName string `json:"senderName"`

	// AdminEmail is the sender email address
	AdminEmail string `json:"adminEmail"`
}

// RestSpec defines PostgREST configuration
type RestSpec struct {
	// ImageTag for postgrest/postgrest (defaults to operator version if empty)
	// +optional
	ImageTag string `json:"imageTag,omitempty"`

	// Replicas count
	// +kubebuilder:default=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Schemas exposed via the API
	// +kubebuilder:default={"public"}
	// +optional
	Schemas []string `json:"schemas,omitempty"`

	// Resources for PostgREST pods
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// StudioSpec defines Supabase Studio configuration
type StudioSpec struct {
	// ImageTag for supabase/studio (defaults to operator version if empty)
	// +optional
	ImageTag string `json:"imageTag,omitempty"`

	// Replicas count
	// +kubebuilder:default=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// PublicURL is the external URL for Studio
	// +optional
	PublicURL string `json:"publicURL,omitempty"`

	// OrganizationName shown in Studio
	// +kubebuilder:default="Default Organization"
	// +optional
	OrganizationName string `json:"organizationName,omitempty"`

	// ProjectName shown in Studio
	// +kubebuilder:default="Default Project"
	// +optional
	ProjectName string `json:"projectName,omitempty"`

	// Resources for Studio pods
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// MetaSpec defines postgres-meta configuration
type MetaSpec struct {
	// ImageTag for supabase/postgres-meta (defaults to operator version if empty)
	// +optional
	ImageTag string `json:"imageTag,omitempty"`

	// Replicas count
	// +kubebuilder:default=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Resources for Meta pods
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// KongSpec defines Kong API gateway configuration
type KongSpec struct {
	// ImageTag for kong (defaults to operator version if empty)
	// +optional
	ImageTag string `json:"imageTag,omitempty"`

	// Replicas count
	// +kubebuilder:default=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Ingress configuration
	// +optional
	Ingress *IngressSpec `json:"ingress,omitempty"`

	// Resources for Kong pods
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// IngressSpec defines ingress configuration
// +kubebuilder:validation:XValidation:rule="!self.enabled || self.host.size() > 0",message="host is required when ingress is enabled"
type IngressSpec struct {
	// Enabled enables ingress creation
	Enabled bool `json:"enabled"`

	// ClassName is the ingress class name
	// +optional
	ClassName string `json:"className,omitempty"`

	// Host is the ingress hostname (required when enabled)
	// +optional
	Host string `json:"host,omitempty"`

	// TLS enables TLS termination
	// +optional
	TLS bool `json:"tls,omitempty"`

	// TLSSecretName is the secret containing TLS certificate
	// +optional
	TLSSecretName string `json:"tlsSecretName,omitempty"`

	// Annotations for the ingress
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ImageSpec defines container image configuration for optional services
type ImageSpec struct {
	// Registry (default: docker.io)
	// +optional
	Registry string `json:"registry,omitempty"`

	// Repository (e.g., journeyapps/powersync-service)
	// +optional
	Repository string `json:"repository,omitempty"`

	// Tag (pinned stable version per service)
	// +optional
	Tag string `json:"tag,omitempty"`

	// PullPolicy (default: IfNotPresent)
	// +kubebuilder:default=IfNotPresent
	// +optional
	PullPolicy corev1.PullPolicy `json:"pullPolicy,omitempty"`
}

// PowersyncSpec defines Powersync offline-first sync configuration
type PowersyncSpec struct {
	// Image configuration (default: journeyapps/powersync-service:1.20.4)
	// +optional
	Image ImageSpec `json:"image,omitempty"`

	// API deployment configuration (client-facing)
	// +optional
	API PowersyncAPISpec `json:"api,omitempty"`

	// Replication deployment configuration (CDC processing)
	// +optional
	Replication PowersyncReplicationSpec `json:"replication,omitempty"`

	// Sync Streams configuration. Exactly one of inline or configMapRef is required.
	// +required
	SyncRules SyncRulesSpec `json:"syncRules"`

	// Compact CronJob configuration
	// +optional
	Compact PowersyncCompactSpec `json:"compact,omitempty"`
}

// PowersyncAPISpec defines Powersync API deployment configuration
type PowersyncAPISpec struct {
	// Replicas (default: 1)
	// +kubebuilder:default=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Resources for Powersync API pods
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// NodeOptions for heap size (default: "--max-old-space-size=150")
	// +optional
	NodeOptions string `json:"nodeOptions,omitempty"`
}

// PowersyncReplicationSpec defines Powersync replication deployment configuration
type PowersyncReplicationSpec struct {
	// Resources for Powersync replication pods
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// NodeOptions for heap size (default: "--max-old-space-size=230")
	// +optional
	NodeOptions string `json:"nodeOptions,omitempty"`
}

// SyncRulesSpec defines the edition 3 Sync Streams configuration for Powersync.
// +kubebuilder:validation:XValidation:rule="has(self.inline) != has(self.configMapRef)",message="exactly one of inline or configMapRef is required"
type SyncRulesSpec struct {
	// Inline Sync Streams YAML, including config.edition: 3.
	// +kubebuilder:validation:MinLength=1
	// +optional
	Inline string `json:"inline,omitempty"`

	// Reference to an external ConfigMap containing sync_rules.yaml.
	// +kubebuilder:validation:MinLength=1
	// +optional
	ConfigMapRef string `json:"configMapRef,omitempty"`
}

// PowersyncCompactSpec defines Powersync compaction CronJob configuration
type PowersyncCompactSpec struct {
	// Enabled (default: true)
	// +kubebuilder:default=true
	// +optional
	Enabled bool `json:"enabled"`

	// Schedule in cron format (default: "0 3 * * *" = 3am daily)
	// +kubebuilder:default="0 3 * * *"
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// Resources for compaction pods
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// SupabaseProjectStatus defines the observed state of SupabaseProject
type SupabaseProjectStatus struct {
	// Phase represents the current lifecycle phase
	// +kubebuilder:validation:Enum=Pending;Provisioning;Running;Failed;Deleting
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions represent the latest available observations
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Database status
	// +optional
	Database DatabaseStatus `json:"database,omitempty"`

	// Services status
	// +optional
	Services ServicesStatus `json:"services,omitempty"`

	// SecretNames contains the names of generated secrets
	// +optional
	SecretNames SecretNamesStatus `json:"secretNames,omitempty"`

	// Endpoints contains service endpoints
	// +optional
	Endpoints EndpointsStatus `json:"endpoints,omitempty"`

	// ObservedGeneration is the last observed generation
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// DatabaseStatus defines database status
type DatabaseStatus struct {
	// Ready indicates if the database cluster is ready
	Ready bool `json:"ready"`

	// Phase of the CNPG cluster
	// +optional
	Phase string `json:"phase,omitempty"`

	// ReadyInstances is the number of ready instances
	// +optional
	ReadyInstances int32 `json:"readyInstances,omitempty"`

	// PrimaryHost is the primary pod hostname
	// +optional
	PrimaryHost string `json:"primaryHost,omitempty"`
}

// ServicesStatus defines status of all services
type ServicesStatus struct {
	// +optional
	Auth ServiceStatus `json:"auth,omitempty"`
	// +optional
	Rest ServiceStatus `json:"rest,omitempty"`
	// +optional
	Studio ServiceStatus `json:"studio,omitempty"`
	// +optional
	Meta ServiceStatus `json:"meta,omitempty"`
	// +optional
	Kong ServiceStatus `json:"kong,omitempty"`
	// +optional
	PowersyncAPI ServiceStatus `json:"powersyncApi,omitempty"`
	// +optional
	PowersyncReplication ServiceStatus `json:"powersyncReplication,omitempty"`
}

// ServiceStatus defines individual service status
type ServiceStatus struct {
	// Ready indicates if the service is ready
	Ready bool `json:"ready"`

	// AvailableReplicas is the number of available replicas
	// +optional
	AvailableReplicas int32 `json:"availableReplicas,omitempty"`
}

// SecretNamesStatus contains generated secret names
type SecretNamesStatus struct {
	// JWT is the name of the JWT secret
	// +optional
	JWT string `json:"jwt,omitempty"`

	// SupabaseAdmin is the name of the supabase_admin password secret
	// +optional
	SupabaseAdmin string `json:"supabaseAdmin,omitempty"`

	// Authenticator is the name of the authenticator password secret
	// +optional
	Authenticator string `json:"authenticator,omitempty"`

	// AuthAdmin is the name of the supabase_auth_admin password secret
	// +optional
	AuthAdmin string `json:"authAdmin,omitempty"`

	// PowersyncStoragePassword is the name of the powersync_storage role password secret
	// +optional
	PowersyncStoragePassword string `json:"powersyncStoragePassword,omitempty"`

	// PowersyncReplicationPassword is the name of the powersync_replication role password secret
	// +optional
	PowersyncReplicationPassword string `json:"powersyncReplicationPassword,omitempty"`
}

// EndpointsStatus contains service endpoints
type EndpointsStatus struct {
	// API is the Kong gateway endpoint (internal)
	// +optional
	API string `json:"api,omitempty"`

	// Database is the PostgreSQL connection endpoint
	// +optional
	Database string `json:"database,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Current phase"
// +kubebuilder:printcolumn:name="Database",type="boolean",JSONPath=".status.database.ready",description="Database ready"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// SupabaseProject is the Schema for the supabaseprojects API
type SupabaseProject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec SupabaseProjectSpec `json:"spec"`

	// +optional
	Status SupabaseProjectStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SupabaseProjectList contains a list of SupabaseProject
type SupabaseProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SupabaseProject `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SupabaseProject{}, &SupabaseProjectList{})
}
