# Sequin + Powersync + Meilisearch Integration Design

**Date:** 2026-02-07
**Status:** Design Complete - Ready for Implementation
**Author:** Claude (via brainstorming session)

## Overview

Extend cloudnative-supabase operator to support three optional add-on services for complete stack deployment:

1. **Sequin** - CDC/event streaming for real-time data pipelines
2. **Powersync** - Offline-first sync for mobile/web applications
3. **Meilisearch** - Fast full-text search engine

**Goal:** Enable complete Supabase + CDC + Search stack deployment in minutes with minimal configuration.

## Design Decisions Summary

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Integration pattern | Operator-managed via optional CRD specs | Single source of truth, proper initialization order |
| Optionality | Optional spec sections (presence = enabled) | Matches existing backup/recovery pattern |
| Redis dependency | Hybrid: external reference or auto-deployed | Flexibility for dev (bundled) vs prod (external) |
| CDC configuration | Fully automatic (roles, publications, schemas) | Minimal user config, reduces misconfiguration |
| Sync rules | Both inline and ConfigMap reference | Matches flicknote-deploy pattern |
| Image versions | Pinned stable defaults with full override | Production reliability, allows customization |
| Resource defaults | Production-ready from flicknote-deploy | Battle-tested values, overridable |
| Health checks | Standard K8s probes, individual conditions | Clear observability, standard patterns |
| Service exposure | ClusterIP only | Cloudflare Tunnel for external access |
| Monitoring | Expose metrics ports, no ServiceMonitors | Stack-agnostic (Prometheus, VictoriaMetrics, etc.) |
| Deployment order | After core Supabase services | Treats CDC as enhancement layer |
| Secret generation | Auto-generate all secrets | Zero-config deployment |

## CRD API Structure

### Top-level Spec Extensions

```go
type SupabaseProjectSpec struct {
    // ... existing fields (Database, Auth, Rest, Studio, Meta, Kong) ...

    // Sequin CDC/event streaming configuration (optional)
    // +optional
    Sequin *SequinSpec `json:"sequin,omitempty"`

    // Powersync offline-first sync configuration (optional)
    // +optional
    Powersync *PowersyncSpec `json:"powersync,omitempty"`

    // Meilisearch full-text search configuration (optional)
    // +optional
    Meilisearch *MeilisearchSpec `json:"meilisearch,omitempty"`
}
```

### SequinSpec

```go
type SequinSpec struct {
    // Image configuration (default: sequin/sequin:v0.13.25)
    // +optional
    Image ImageSpec `json:"image,omitempty"`

    // Replicas (default: 1)
    // +kubebuilder:default=1
    // +optional
    Replicas int32 `json:"replicas,omitempty"`

    // Resources (defaults: 256Mi/100m CPU → 512Mi/500m CPU)
    // +optional
    Resources corev1.ResourceRequirements `json:"resources,omitempty"`

    // Redis configuration - external or bundled
    // +optional
    Redis RedisSpec `json:"redis,omitempty"`

    // Account/user configuration (defaults provided)
    // +optional
    Account *SequinAccountSpec `json:"account,omitempty"`
}

type RedisSpec struct {
    // External Redis reference (if nil, operator deploys minimal Redis)
    // +optional
    External *ExternalRedisSpec `json:"external,omitempty"`

    // Resources for bundled Redis (default: 256Mi/50m → 256Mi/100m)
    // +optional
    Resources corev1.ResourceRequirements `json:"resources,omitempty"`

    // Persistence for bundled Redis
    // +optional
    Storage RedisPersistenceSpec `json:"storage,omitempty"`
}

type ExternalRedisSpec struct {
    // Host of external Redis instance
    // +required
    Host string `json:"host"`

    // Port (default: 6379)
    // +kubebuilder:default=6379
    // +optional
    Port int32 `json:"port,omitempty"`

    // PasswordSecretRef for Redis AUTH (optional)
    // +optional
    PasswordSecretRef string `json:"passwordSecretRef,omitempty"`
}

type RedisPersistenceSpec struct {
    // StorageClass (default: "" = cluster default)
    // +optional
    StorageClass string `json:"storageClass,omitempty"`

    // Size (default: 1Gi)
    // +kubebuilder:default="1Gi"
    // +optional
    Size string `json:"size,omitempty"`
}

type SequinAccountSpec struct {
    // Account name (default: "default")
    // +kubebuilder:default="default"
    // +optional
    Name string `json:"name,omitempty"`

    // Admin user email (default: "admin@example.com")
    // +optional
    Email string `json:"email,omitempty"`
}
```

### PowersyncSpec

```go
type PowersyncSpec struct {
    // Image configuration (default: journeyapps/powersync-service:1.18.2)
    // +optional
    Image ImageSpec `json:"image,omitempty"`

    // API deployment configuration (client-facing)
    // +optional
    API PowersyncAPISpec `json:"api,omitempty"`

    // Replication deployment configuration (CDC processing)
    // +optional
    Replication PowersyncReplicationSpec `json:"replication,omitempty"`

    // Sync rules configuration
    // +optional
    SyncRules SyncRulesSpec `json:"syncRules,omitempty"`

    // Compact CronJob configuration
    // +optional
    Compact PowersyncCompactSpec `json:"compact,omitempty"`
}

type PowersyncAPISpec struct {
    // Replicas (default: 2)
    // +kubebuilder:default=2
    // +optional
    Replicas int32 `json:"replicas,omitempty"`

    // Resources (default: 360Mi/100m → 360Mi/1cpu)
    // +optional
    Resources corev1.ResourceRequirements `json:"resources,omitempty"`

    // NodeOptions for heap size (default: "--max-old-space-size=330")
    // +optional
    NodeOptions string `json:"nodeOptions,omitempty"`
}

type PowersyncReplicationSpec struct {
    // Resources (default: 512Mi/100m → 512Mi/1cpu)
    // +optional
    Resources corev1.ResourceRequirements `json:"resources,omitempty"`

    // NodeOptions for heap size (default: "--max-old-space-size=482")
    // +optional
    NodeOptions string `json:"nodeOptions,omitempty"`
}

type SyncRulesSpec struct {
    // Inline sync rules (YAML string)
    // If both Inline and ConfigMapRef are empty, uses default todolist example
    // +optional
    Inline string `json:"inline,omitempty"`

    // Reference to external ConfigMap containing sync rules
    // Takes precedence over Inline if both provided
    // +optional
    ConfigMapRef string `json:"configMapRef,omitempty"`
}

type PowersyncCompactSpec struct {
    // Enabled (default: true)
    // +kubebuilder:default=true
    // +optional
    Enabled bool `json:"enabled"`

    // Schedule in cron format (default: "0 3 * * *" = 3am daily)
    // +kubebuilder:default="0 3 * * *"
    // +optional
    Schedule string `json:"schedule,omitempty"`

    // Resources (default: 256Mi/100m → 1Gi/500m)
    // +optional
    Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}
```

### MeilisearchSpec

```go
type MeilisearchSpec struct {
    // Image configuration (default: getmeili/meilisearch:v1.11.0)
    // +optional
    Image ImageSpec `json:"image,omitempty"`

    // Replicas (default: 1)
    // +kubebuilder:default=1
    // +optional
    Replicas int32 `json:"replicas,omitempty"`

    // Resources (default: 512Mi/250m → 2Gi/500m)
    // +optional
    Resources corev1.ResourceRequirements `json:"resources,omitempty"`

    // Persistence configuration
    // +optional
    Persistence PersistenceSpec `json:"persistence,omitempty"`

    // MasterKeySecretRef for existing secret (optional)
    // If not provided, operator auto-generates master key
    // +optional
    MasterKeySecretRef string `json:"masterKeySecretRef,omitempty"`
}

type PersistenceSpec struct {
    // StorageClass (default: "" = cluster default)
    // +optional
    StorageClass string `json:"storageClass,omitempty"`

    // Size (default: 10Gi)
    // +kubebuilder:default="10Gi"
    // +optional
    Size string `json:"size,omitempty"`
}
```

### Common ImageSpec

```go
// ImageSpec defines container image configuration
type ImageSpec struct {
    // Registry (default: docker.io)
    // +optional
    Registry string `json:"registry,omitempty"`

    // Repository (e.g., sequin/sequin, guionai/sequin)
    // +optional
    Repository string `json:"repository,omitempty"`

    // Tag (pinned stable version per service)
    // +optional
    Tag string `json:"tag,omitempty"`

    // PullPolicy (default: IfNotPresent)
    // +kubebuilder:default=IfNotPresent
    // +optional
    PullPolicy string `json:"pullPolicy,omitempty"`
}
```

## Database & CDC Auto-Configuration

When `spec.sequin` or `spec.powersync` are present, the operator automatically configures CDC infrastructure.

### Automatic CNPG Roles

Added to `spec.database.additionalRoles` in the CNPG Cluster:

```yaml
# When spec.sequin exists:
- name: sequin
  ensure: present
  login: true
  passwordSecret:
    name: <project>-sequin-password

- name: sequin_replication
  ensure: present
  login: true
  replication: true
  bypassrls: true
  passwordSecret:
    name: <project>-sequin-replication-password

# When spec.powersync exists:
- name: powersync_storage
  ensure: present
  login: true
  passwordSecret:
    name: <project>-powersync-storage-password
```

### Automatic Database Resources

```yaml
# Sequin gets its own database with citext extension
additionalDatabases:
  - name: sequin
    owner: sequin
    extensions:
      - name: citext
        ensure: present

# Powersync schema in main Supabase database
bootstrapDatabase:
  schemas:
    - name: powersync
      owner: powersync_storage

# CDC Publications for change data capture
publications:
  - name: sequin_pub
    publicationName: sequin_pub
    database: supabase
    target:
      objects:
        - tablesInSchema: public

  - name: powersync
    database: supabase
    target:
      objects:
        - tablesInSchema: public
```

### CDC Permissions SQL

**Solution:** dbmate migration Job (mirrors flicknote-deploy pattern)

The operator creates a Kubernetes Job that runs dbmate to apply CDC permissions after CNPG cluster is ready.

**Migration file** (`20260207000001_cdc_grants.sql`):
```sql
-- migrate:up

-- Grant CDC role (sequin_replication) read access to public schema only
-- More restrictive than pg_read_all_data (avoids system schema access)

-- Grant schema usage
GRANT USAGE ON SCHEMA public TO sequin_replication;

-- Grant sequin CREATE ON DATABASE so its migrations can run CREATE SCHEMA IF NOT EXISTS
-- (supabase_admin owns the database, so it can grant this directly)
GRANT CREATE ON DATABASE supabase TO sequin;

-- Grant SELECT on future tables created by supabase_admin in public schema
-- (tables are created by subsequent migrations, so default privileges cover all)
ALTER DEFAULT PRIVILEGES FOR ROLE supabase_admin IN SCHEMA public GRANT SELECT ON TABLES TO sequin_replication;

-- migrate:down

REVOKE USAGE ON SCHEMA public FROM sequin_replication;
ALTER DEFAULT PRIVILEGES FOR ROLE supabase_admin IN SCHEMA public REVOKE SELECT ON TABLES FROM sequin_replication;
REVOKE CREATE ON DATABASE supabase FROM sequin;
```

**Implementation:**
- ConfigMap containing migration file
- Job with dbmate image (`ghcr.io/amacneil/dbmate:2.24`)
- Uses `--migrations-table=cloudnative_supabase_schema_migrations` to avoid conflicts with application's schema_migrations table
- Runs `dbmate up` with `--no-dump-schema` flag
- InitContainer waits for CNPG cluster ready (checks for auth.users table existence)
- Status tracked via `ConditionTypeCDCReady` condition

Reference: `/Users/neil/Code/guion/flick-backend-31/tanka/charts/db-init`

### Secret Generation

Operator auto-generates these secrets if not provided:

| Secret Name | Keys | Purpose |
|-------------|------|---------|
| `<project>-sequin` | `secretKeyBase`, `vaultKey`, `apiToken` | Sequin encryption + API auth |
| `<project>-sequin-password` | `username`, `password` | Sequin database role |
| `<project>-sequin-replication-password` | `username`, `password` | CDC replication role |
| `<project>-powersync-storage-password` | `username`, `password` | Powersync storage role |
| `<project>-meilisearch-master-key` | `masterKey` | Meilisearch admin auth |

Secrets are only generated if they don't already exist (prevents regeneration on operator restart).

## Deployment Resources

### Sequin Resources (when `spec.sequin` present)

1. **Secret**: `<project>-sequin`
   ```yaml
   data:
     secretKeyBase: <base64-encoded-random>  # 64 bytes
     vaultKey: <base64-encoded-random>        # 32 bytes
     apiToken: <base64-encoded-random>        # API token for CLI
   ```

2. **Deployment**: `<project>-sequin`
   ```yaml
   spec:
     replicas: 1  # from spec.sequin.replicas
     template:
       spec:
         containers:
         - name: sequin
           image: sequin/sequin:v0.13.25  # or user override
           env:
           - name: DATABASE_URL
             value: postgres://sequin:<password>@<cnpg-rw>:5432/sequin
           - name: REDIS_URL
             value: redis://<redis-host>:6379
           - name: SECRET_KEY_BASE
             valueFrom:
               secretKeyRef:
                 name: <project>-sequin
                 key: secretKeyBase
           # ... other env vars
           resources:
             requests:
               memory: 256Mi
               cpu: 100m
             limits:
               memory: 512Mi
               cpu: 500m
   ```

3. **Service**: `<project>-sequin`
   ```yaml
   spec:
     type: ClusterIP
     ports:
     - port: 7376
       name: http
     - port: 4000  # metrics
       name: metrics
   ```

4. **Redis** (if `spec.sequin.redis.external` not provided):
   - **StatefulSet**: `<project>-sequin-redis` (1 replica)
   - **Service**: `<project>-sequin-redis` (ClusterIP, port 6379)
   - **PVC**: `redis-data-<project>-sequin-redis-0` (1Gi)

### Powersync Resources (when `spec.powersync` present)

1. **ConfigMap**: `<project>-powersync-config`
   ```yaml
   data:
     config.json: |
       {
         "storage": {
           "type": "postgresql",
           "uri": "postgres://<cnpg-rw>:5432/supabase",
           "username": "powersync_storage",
           "password": "<from-secret>"
         },
         "replication": {
           "connections": [{
             "type": "postgresql",
             "uri": "postgres://<cnpg-rw>:5432/supabase",
             "username": "sequin_replication",
             "password": "<from-secret>",
             "tag": "default"
           }]
         },
         "client_auth": {
           "supabase": true,
           "supabase_jwt_secret": "<from-jwt-secret>",
           "audience": ["authenticated"]
         },
         "sync_rules": {
           "path": "/powersync/sync_rules/sync_rules.yaml"
         }
       }
   ```

2. **ConfigMap**: `<project>-powersync-sync-rules`
   ```yaml
   data:
     sync_rules.yaml: |
       # From spec.powersync.syncRules.inline
       # OR references external ConfigMap
       # OR default todolist example:
       bucket_definitions:
         global:
           data:
             - select _id as id, * from lists
             - select _id as id, * from todos
   ```

3. **Deployment**: `<project>-powersync-api`
   ```yaml
   spec:
     replicas: 2  # from spec.powersync.api.replicas
     template:
       spec:
         containers:
         - name: powersync
           image: journeyapps/powersync-service:1.18.2
           command: ["node", "dist/src/entry-api.js"]
           env:
           - name: NODE_OPTIONS
             value: "--max-old-space-size=330"
           - name: POWERSYNC_CONFIG_PATH
             value: "/powersync/config/config.json"
           volumeMounts:
           - name: config
             mountPath: /powersync/config
           - name: sync-rules
             mountPath: /powersync/sync_rules
           resources:
             requests:
               memory: 360Mi
               cpu: 100m
             limits:
               memory: 360Mi
               cpu: 1
   ```

4. **Deployment**: `<project>-powersync-replication`
   - Same image, different command: `["node", "dist/src/entry-replication.js"]`
   - Resources: 512Mi/100m → 512Mi/1cpu
   - NODE_OPTIONS: `--max-old-space-size=482`

5. **Service**: `<project>-powersync`
   ```yaml
   spec:
     type: ClusterIP
     ports:
     - port: 8080
       name: http
     - port: 9464
       name: metrics
   ```

6. **CronJob**: `<project>-powersync-compact`
   ```yaml
   spec:
     schedule: "0 3 * * *"  # 3am daily
     jobTemplate:
       spec:
         template:
           spec:
             containers:
             - name: compact
               image: journeyapps/powersync-service:1.18.2
               command: ["node", "dist/src/entry-compact.js"]
   ```

### Meilisearch Resources (when `spec.meilisearch` present)

1. **Secret**: `<project>-meilisearch-master-key`
   ```yaml
   data:
     masterKey: <base64-encoded-random>  # 32 bytes
   ```

2. **StatefulSet**: `<project>-meilisearch`
   ```yaml
   spec:
     replicas: 1
     volumeClaimTemplates:
     - metadata:
         name: data
       spec:
         storageClassName: ""  # cluster default
         resources:
           requests:
             storage: 10Gi
     template:
       spec:
         containers:
         - name: meilisearch
           image: getmeili/meilisearch:v1.11.0
           env:
           - name: MEILI_ENV
             value: "production"
           - name: MEILI_NO_ANALYTICS
             value: "true"
           - name: MEILI_EXPERIMENTAL_LOGS_MODE
             value: "json"
           - name: MEILI_MASTER_KEY
             valueFrom:
               secretKeyRef:
                 name: <project>-meilisearch-master-key
                 key: masterKey
           volumeMounts:
           - name: data
             mountPath: /meili_data
           resources:
             requests:
               memory: 512Mi
               cpu: 250m
             limits:
               memory: 2Gi
               cpu: 500m
   ```

3. **Service**: `<project>-meilisearch`
   ```yaml
   spec:
     type: ClusterIP
     ports:
     - port: 7700
       name: http
   ```

### Owner References

All resources have `ownerReferences` pointing to the SupabaseProject for automatic garbage collection on delete.

## Status & Observability

### New Status Conditions

```go
const (
    // ... existing conditions (Ready, DatabaseReady, AuthReady, etc.) ...

    // Sequin conditions
    ConditionTypeSequinReady         = "SequinReady"
    ConditionTypeSequinDatabaseReady = "SequinDatabaseReady"

    // Powersync conditions
    ConditionTypePowersyncReady        = "PowersyncReady"
    ConditionTypePowersyncStorageReady = "PowersyncStorageReady"

    // Meilisearch condition
    ConditionTypeMeilisearchReady = "MeilisearchReady"
)
```

### Extended ServicesStatus

```go
type ServicesStatus struct {
    // Existing services
    Auth   ServiceStatus `json:"auth,omitempty"`
    Rest   ServiceStatus `json:"rest,omitempty"`
    Studio ServiceStatus `json:"studio,omitempty"`
    Meta   ServiceStatus `json:"meta,omitempty"`
    Kong   ServiceStatus `json:"kong,omitempty"`

    // New services
    Sequin               ServiceStatus `json:"sequin,omitempty"`
    PowersyncAPI         ServiceStatus `json:"powersyncApi,omitempty"`
    PowersyncReplication ServiceStatus `json:"powersyncReplication,omitempty"`
    Meilisearch          ServiceStatus `json:"meilisearch,omitempty"`
}
```

### Health Checks

All deployments use standard Kubernetes readiness/liveness probes:

**Sequin:**
```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 7376
  initialDelaySeconds: 30
  periodSeconds: 10
readinessProbe:
  httpGet:
    path: /health
    port: 7376
  initialDelaySeconds: 10
  periodSeconds: 5
```

**Powersync API/Replication:**
```yaml
livenessProbe:
  httpGet:
    path: /api/health
    port: 8080
  initialDelaySeconds: 30
readinessProbe:
  httpGet:
    path: /api/health
    port: 8080
  initialDelaySeconds: 10
```

**Meilisearch:**
```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 7700
  initialDelaySeconds: 30
readinessProbe:
  httpGet:
    path: /health
    port: 7700
  initialDelaySeconds: 10
```

### Status Update Logic

Operator watches Deployment/StatefulSet status and updates conditions:

1. **SequinDatabaseReady**: True after Sequin database + roles created in CNPG
2. **SequinReady**: True when Sequin deployment has `availableReplicas >= 1`
3. **PowersyncStorageReady**: True after powersync schema + publications created
4. **PowersyncReady**: True when both API and Replication deployments have `availableReplicas >= 1`
5. **MeilisearchReady**: True when StatefulSet has `readyReplicas >= 1`

**Overall Ready condition:** True when all enabled services are ready (including CDC/search if specs present).

### Metrics Exposure

All services expose Prometheus metrics on dedicated ports (no ServiceMonitor resources created):

| Service | Metrics Port | Endpoint |
|---------|--------------|----------|
| Sequin | 4000 | `/metrics` |
| Powersync | 9464 | `/metrics` |
| Meilisearch | 7700 | `/metrics` |
| Redis (bundled) | 9121 | `/metrics` (via redis-exporter sidecar, optional) |

Users add ServiceMonitor/PodMonitor resources based on their monitoring stack.

## Reconciliation Flow

### Updated Controller Logic

```go
func (r *SupabaseProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    project := &v1alpha1.SupabaseProject{}
    // ... fetch project ...

    // Phase 1: Secrets
    if err := r.reconcileSecrets(ctx, project); err != nil {
        return ctrl.Result{}, err
    }
    // Generates:
    // - JWT secret (existing)
    // - Database passwords (existing)
    // - Sequin secrets (if spec.sequin != nil)
    // - Powersync passwords (if spec.powersync != nil)
    // - Meilisearch master key (if spec.meilisearch != nil)

    // Phase 2: InitSQL ConfigMap
    if err := r.reconcileInitSQL(ctx, project); err != nil {
        return ctrl.Result{}, err
    }
    // Includes standard Supabase init SQL
    // TODO: Add CDC permissions SQL (pending Task #1 research)

    // Phase 3: Backup/Recovery (if enabled)
    if err := r.reconcileBackup(ctx, project); err != nil {
        return ctrl.Result{}, err
    }

    // Phase 4: CNPG Cluster
    if err := r.reconcileCNPGCluster(ctx, project); err != nil {
        return ctrl.Result{}, err
    }
    // Extends cluster with:
    // - Sequin roles + database (if spec.sequin)
    // - Powersync roles + schema (if spec.powersync)
    // - Publications (if CDC enabled)

    // Phase 5: Wait for Database Ready
    if !r.isDatabaseReady(ctx, project) {
        r.setCondition(project, ConditionTypeDatabaseReady, metav1.ConditionFalse, "Waiting", "Database not ready")
        return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
    }
    r.setCondition(project, ConditionTypeDatabaseReady, metav1.ConditionTrue, "Ready", "Database ready")

    // Phase 6: Core Services
    if err := r.reconcileCoreServices(ctx, project); err != nil {
        return ctrl.Result{}, err
    }
    // Deploys Auth, REST, Studio, Meta, Kong

    // Phase 7: CDC Services (after core services - flicknote-deploy pattern)
    if project.Spec.Sequin != nil {
        if err := r.reconcileSequin(ctx, project); err != nil {
            r.setCondition(project, ConditionTypeSequinReady, metav1.ConditionFalse, "Error", err.Error())
            return ctrl.Result{}, err
        }
    }

    if project.Spec.Powersync != nil {
        if err := r.reconcilePowersync(ctx, project); err != nil {
            r.setCondition(project, ConditionTypePowersyncReady, metav1.ConditionFalse, "Error", err.Error())
            return ctrl.Result{}, err
        }
    }

    // Phase 8: Search Service
    if project.Spec.Meilisearch != nil {
        if err := r.reconcileMeilisearch(ctx, project); err != nil {
            r.setCondition(project, ConditionTypeMeilisearchReady, metav1.ConditionFalse, "Error", err.Error())
            return ctrl.Result{}, err
        }
    }

    // Phase 9: Update Overall Status
    if err := r.updateStatus(ctx, project); err != nil {
        return ctrl.Result{}, err
    }

    return ctrl.Result{}, nil
}
```

### Phase 7 Detail: reconcileSequin

```go
func (r *SupabaseProjectReconciler) reconcileSequin(ctx context.Context, project *v1alpha1.SupabaseProject) error {
    // 1. Deploy Redis (if external not configured)
    if project.Spec.Sequin.Redis.External == nil {
        if err := r.reconcileSequinRedis(ctx, project); err != nil {
            return fmt.Errorf("failed to deploy Redis: %w", err)
        }
    }

    // 2. Deploy Sequin deployment
    deployment := r.buildSequinDeployment(project)
    if err := r.createOrUpdate(ctx, deployment); err != nil {
        return fmt.Errorf("failed to deploy Sequin: %w", err)
    }

    // 3. Deploy Sequin service
    service := r.buildSequinService(project)
    if err := r.createOrUpdate(ctx, service); err != nil {
        return fmt.Errorf("failed to create Sequin service: %w", err)
    }

    // 4. Update status
    if r.isDeploymentReady(ctx, deployment) {
        r.setCondition(project, ConditionTypeSequinReady, metav1.ConditionTrue, "Ready", "Sequin is ready")
    } else {
        r.setCondition(project, ConditionTypeSequinReady, metav1.ConditionFalse, "Pending", "Waiting for Sequin")
    }

    return nil
}
```

### Key Implementation Details

- **Conditional deployment:** Only create resources if spec section exists (`if project.Spec.Sequin != nil`)
- **Secret reuse:** Check if secrets exist before generating (prevent regeneration on restart)
- **CNPG cluster updates:** Merge new roles/databases/publications into existing cluster spec
- **Error handling:** Set appropriate conditions on failure, return error for retry
- **Requeue logic:** Use `RequeueAfter` for waiting on dependencies (e.g., database ready)

## Example Usage

### Minimal Example (All Defaults)

```yaml
apiVersion: supabase.guion.ai/v1alpha1
kind: SupabaseProject
metadata:
  name: my-app
  namespace: apps-dev
spec:
  database:
    instances: 1
    storage:
      size: 10Gi

  auth:
    siteURL: https://my-app.example.com
    externalURL: https://my-app.example.com/auth

  # Enable Sequin with all defaults (auto-deployed Redis)
  sequin: {}

  # Enable Powersync with all defaults (todolist sync rules)
  powersync: {}

  # Enable Meilisearch with all defaults (10Gi storage)
  meilisearch: {}
```

**Result:**
- Supabase core stack deployed
- Sequin with bundled Redis (1Gi)
- Powersync with default todolist sync rules
- Meilisearch with 10Gi storage
- All secrets auto-generated
- All CDC roles/publications configured automatically

### Production Example (Custom Configuration)

```yaml
apiVersion: supabase.guion.ai/v1alpha1
kind: SupabaseProject
metadata:
  name: production-app
  namespace: apps-prod
spec:
  database:
    instances: 3
    storage:
      size: 100Gi
      storageClass: longhorn

  auth:
    siteURL: https://app.example.com
    externalURL: https://app.example.com/auth

  # Sequin with external Redis
  sequin:
    replicas: 2
    redis:
      external:
        host: redis.infra-prod.svc
        port: 6379
    resources:
      requests:
        memory: 512Mi
        cpu: 200m
      limits:
        memory: 1Gi
        cpu: 1

  # Powersync with custom sync rules ConfigMap
  powersync:
    api:
      replicas: 3
      resources:
        requests:
          memory: 512Mi
          cpu: 200m
        limits:
          memory: 1Gi
          cpu: 2
    replication:
      resources:
        requests:
          memory: 1Gi
          cpu: 200m
        limits:
          memory: 2Gi
          cpu: 2
    syncRules:
      configMapRef: my-custom-sync-rules
    compact:
      schedule: "0 2 * * *"  # 2am daily

  # Meilisearch with larger storage
  meilisearch:
    replicas: 2
    persistence:
      size: 50Gi
      storageClass: longhorn
    resources:
      requests:
        memory: 1Gi
        cpu: 500m
      limits:
        memory: 4Gi
        cpu: 2
```

### FlickNote Reference Example

Replicates exact flicknote-deploy configuration:

```yaml
apiVersion: supabase.guion.ai/v1alpha1
kind: SupabaseProject
metadata:
  name: flicknote
  namespace: apps-prod
spec:
  database:
    instances: 1
    storage:
      size: 20Gi
      storageClass: local-path

  auth:
    siteURL: https://flicknote.app
    externalURL: https://api.flicknote.app/auth

  # Sequin - FlickNote custom fork
  sequin:
    image:
      registry: ghcr.io
      repository: guionai/sequin
      tag: flicknote
      pullPolicy: IfNotPresent
    replicas: 1
    redis:
      external:
        host: redis.infra-prod.svc
        port: 6379
    resources:
      requests:
        memory: 256Mi
        cpu: 100m
      limits:
        memory: 512Mi
        cpu: 500m

  # Powersync - matches flicknote-deploy values
  powersync:
    image:
      repository: journeyapps/powersync-service
      tag: "1.18.2"
      pullPolicy: IfNotPresent
    api:
      replicas: 2
      resources:
        requests:
          memory: 360Mi
          cpu: 100m
        limits:
          memory: 360Mi
          cpu: 1
      nodeOptions: "--max-old-space-size=330"
    replication:
      resources:
        requests:
          memory: 512Mi
          cpu: 100m
        limits:
          memory: 512Mi
          cpu: 1
      nodeOptions: "--max-old-space-size=482"
    syncRules:
      configMapRef: powersync-sync-rules  # External ConfigMap from Tanka
    compact:
      schedule: "0 3 * * *"

  # Meilisearch - matches flicknote-deploy values
  meilisearch:
    image:
      repository: getmeili/meilisearch
      tag: v1.11.0
    replicas: 1
    persistence:
      size: 10Gi
      storageClass: local-path
    resources:
      requests:
        memory: 512Mi
        cpu: 250m
      limits:
        memory: 2Gi
        cpu: 500m
```

## Open Questions & Research Tasks

### ~~Task #1: CDC Permissions SQL Ordering~~ ✅ RESOLVED

**Status:** ✅ Resolved - using dbmate migration Job

**Solution:** Create a Kubernetes Job that runs dbmate to apply CDC permissions after CNPG cluster is ready.

**Implementation details:**
- ConfigMap with single migration: `20260207000001_cdc_grants.sql`
- Job uses `ghcr.io/amacneil/dbmate:2.24` image
- Custom migrations table: `--migrations-table=cloudnative_supabase_schema_migrations` (avoids conflict with application migrations)
- InitContainer waits for auth.users table (ensures CNPG fully ready)
- Job runs `dbmate up --no-dump-schema`
- Idempotent: dbmate tracks applied migrations, safe to re-run

**Why this approach:**
- ✅ Clear ordering: Runs after CNPG cluster + managed roles ready
- ✅ Mirrors flicknote-deploy pattern (proven in production)
- ✅ Idempotent via dbmate's schema_migrations tracking
- ✅ Status tracking via ConditionTypeCDCReady
- ✅ No conflict with application's dbmate migrations (separate table)

Reference implementation: `/Users/neil/Code/guion/flick-backend-31/tanka/charts/db-init`

### Other Open Questions

1. **Sequin init configuration** - How to inject account/user/API token setup?
   - Current: Uses `configuration` field in Helm chart
   - Need: Operator approach for initial setup

2. **Powersync default sync rules** - Exact YAML to use for default
   - Copy from flicknote-deploy chart's todolist example

3. **Redis password** - Should bundled Redis have auth enabled?
   - flicknote-deploy Redis is passwordless (internal only)
   - Bundled Redis should match (ClusterIP, no auth)

4. **Metrics validation** - Ensure all services expose metrics correctly
   - Test Prometheus scraping on deployed services

## Implementation Phases

### Phase 1: CRD + Basic Sequin
- Define CRD API structs (SequinSpec, PowersyncSpec, MeilisearchSpec)
- Generate CRD YAML (`make generate manifests`)
- Implement secret generation (Sequin secrets)
- Implement Sequin deployment (without bundled Redis)
- Test with external Redis

### Phase 2: Powersync + Auto CDC Config
- Implement Powersync deployments (API, Replication, CronJob)
- Implement sync rules ConfigMap generation
- Extend CNPG cluster builder with CDC roles/publications
- Resolve Task #1 (CDC permissions SQL ordering)
- Test CDC integration end-to-end

### Phase 3: Meilisearch + Bundled Redis
- Implement Meilisearch StatefulSet
- Implement master key generation
- Implement bundled Redis option for Sequin
- Test storage persistence

### Phase 4: Documentation + Polish
- Write CRD reference documentation
- Write quick start guide
- Write FlickNote configuration guide
- Add E2E tests
- Performance testing

## Testing Strategy

### Unit Tests
- Builder functions for all new resources (Sequin, Powersync, Meilisearch)
- Secret generation logic
- CNPG cluster extension logic (roles, publications)

### Integration Tests
- envtest with CNPG CRDs installed
- Test reconciliation flow with mocked CNPG Cluster
- Test status condition updates

### E2E Tests
- Deploy to kind cluster
- Create SupabaseProject with all three specs
- Verify all services reach Ready status
- Test CDC functionality (Sequin replication, Powersync sync)
- Test search functionality (Meilisearch indexing)

### Backward Compatibility Tests
- Ensure existing SupabaseProjects without CDC specs continue working
- Verify no breaking changes to existing API

### Upgrade Tests
- Operator upgrade with existing SupabaseProjects
- Verify secrets not regenerated
- Verify no service disruption

## Next Steps

1. ✅ **Design complete** - Document written
2. ⏳ **Task #1 research** - CDC permissions SQL ordering
3. **Write implementation plan** - Break down Phase 1 into concrete tasks
4. **Prototype Phase 1** - CRD + basic Sequin on new project
5. **Iterate based on feedback** - Adjust design as implementation progresses

## References

- **flicknote-deploy repository:** `/Users/neil/Code/guion/flicknote-deploy`
  - Sequin chart: `charts/sequin/`
  - Powersync chart: `charts/powersync/`
  - Meilisearch config: `flux/apps/base/meilisearch/`

- **Current cloudnative-supabase:**
  - CRD: `api/v1alpha1/supabaseproject_types.go`
  - Controller: `internal/controller/supabaseproject_controller.go`
  - Resource builders: `internal/resources/`

- **External documentation:**
  - [CNPG Documentation](https://cloudnative-pg.io/)
  - [Sequin Documentation](https://sequinstream.com/docs)
  - [Powersync Documentation](https://docs.powersync.com/)
  - [Meilisearch Documentation](https://www.meilisearch.com/docs)
