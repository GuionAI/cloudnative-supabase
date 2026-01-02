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

	// ImagePullSecrets for all deployments
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

// DatabaseSpec defines PostgreSQL configuration via CNPG
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

	// AdditionalRoles beyond the standard Supabase roles (e.g., sequin_replication)
	// Uses CNPG RoleConfiguration directly for full compatibility
	// +optional
	AdditionalRoles []cnpgv1.RoleConfiguration `json:"additionalRoles,omitempty"`
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

	// DestinationPath is the S3/R2 bucket path (s3://bucket/path/)
	// Required when backup is enabled
	// +optional
	DestinationPath string `json:"destinationPath,omitempty"`

	// EndpointURL for S3-compatible storage
	// +optional
	EndpointURL string `json:"endpointURL,omitempty"`

	// S3CredentialsSecret references a secret with ACCESS_KEY_ID and SECRET_ACCESS_KEY
	// Required when backup is enabled
	// +optional
	S3CredentialsSecret string `json:"s3CredentialsSecret,omitempty"`
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
	// Image tag for supabase/gotrue
	// +kubebuilder:default="v2.184.0"
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
	// ImageTag for postgrest/postgrest
	// +kubebuilder:default="v12.2.3"
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
	// ImageTag for supabase/studio
	// +kubebuilder:default="2024.12.09-sha-434634f"
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
	// ImageTag for supabase/postgres-meta
	// +kubebuilder:default="v0.84.2"
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
	// ImageTag for kong
	// +kubebuilder:default="2.8.1"
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
