# CDC & Search Services Quick Start

This guide covers enabling optional CDC (Change Data Capture) and search services in your SupabaseProject.

## Overview

Three optional services can be enabled by adding their spec section to your SupabaseProject:

| Service | Purpose | Trigger |
|---------|---------|---------|
| **Sequin** | CDC/event streaming | `spec.sequin` present |
| **Powersync** | Offline-first sync for mobile/web | `spec.powersync` present |
| **Meilisearch** | Full-text search engine | `spec.meilisearch` present |

All services are optional. Omitting a section means that service is not deployed.

## Minimal Setup

Enable all three services with defaults:

```yaml
apiVersion: supabase.guion.dev/v1alpha1
kind: SupabaseProject
metadata:
  name: my-app
  namespace: my-app
spec:
  database:
    instances: 1
    storage:
      size: 10Gi

  auth:
    siteURL: https://app.example.com
    externalURL: https://auth.example.com

  # CDC + Search - just add the section to enable
  sequin: {}
  powersync: {}
  meilisearch: {}
```

This gives you:
- Sequin with bundled Redis (2Gi AOF persistence)
- Powersync with default sync rules and daily compaction
- Meilisearch with 10Gi storage

All secrets, database roles, and CDC publications are auto-configured.

## Sequin Configuration

### With Bundled Redis (default)

```yaml
sequin: {}
```

The operator deploys a single-replica Redis StatefulSet with:
- AOF persistence (`--appendonly yes`)
- 2Gi default storage
- Non-root security context

### With External Redis

```yaml
sequin:
  redis:
    external:
      host: redis.infra.svc
      port: 6379
```

### Full Configuration

```yaml
sequin:
  image:
    registry: ghcr.io
    repository: guionai/sequin
    tag: flicknote
  replicas: 2
  resources:
    requests:
      memory: 512Mi
      cpu: 200m
    limits:
      memory: 1Gi
      cpu: 1
  redis:
    external:
      host: redis.infra.svc
      port: 6379
```

### Bundled Redis Customization

```yaml
sequin:
  redis:
    resources:
      requests:
        memory: 256Mi
      limits:
        memory: 512Mi
    storage:
      size: 5Gi
      storageClass: fast-ssd
```

## Powersync Configuration

### Default Setup

```yaml
powersync: {}
```

Creates:
- **API deployment** (1 replica) - client-facing sync endpoint
- **Replication deployment** (1 replica) - CDC processing
- **Compact CronJob** - daily at 3am
- **Default sync rules** - `SELECT * FROM public.*`

### Custom Sync Rules (inline)

```yaml
powersync:
  syncRules:
    inline: |
      bucket_definitions:
        user_data:
          parameters: SELECT token_parameters.user_id as user_id
          data:
            - SELECT * FROM todos WHERE user_id = bucket.user_id
```

### External Sync Rules ConfigMap

```yaml
powersync:
  syncRules:
    configMapRef: my-sync-rules
```

The referenced ConfigMap must have a `sync_rules.yaml` key.

### Full Configuration

```yaml
powersync:
  image:
    repository: journeyapps/powersync-service
    tag: "1.18.2"
  api:
    replicas: 3
    resources:
      requests:
        memory: 512Mi
        cpu: 200m
      limits:
        memory: 1Gi
        cpu: 2
    nodeOptions: "--max-old-space-size=512"
  replication:
    resources:
      requests:
        memory: 1Gi
      limits:
        memory: 2Gi
    nodeOptions: "--max-old-space-size=960"
  compact:
    enabled: true
    schedule: "0 2 * * *"
```

## Meilisearch Configuration

### Default Setup

```yaml
meilisearch: {}
```

Creates a StatefulSet with 10Gi persistent storage and auto-generated master key.

### With Existing Master Key

```yaml
meilisearch:
  masterKeySecretRef: my-meili-key
```

The secret must have a `masterKey` key.

### Full Configuration

```yaml
meilisearch:
  image:
    repository: getmeili/meilisearch
    tag: v1.12.0
  replicas: 1
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

## Auto-Generated Resources

### Secrets

| Secret | Keys | Generated When |
|--------|------|----------------|
| `<name>-sequin` | `secretKeyBase`, `vaultKey`, `apiToken` | `spec.sequin` present |
| `<name>-sequin-password` | `username`, `password` | `spec.sequin` present |
| `<name>-sequin-replication-password` | `username`, `password` | `spec.sequin` or `spec.powersync` present |
| `<name>-powersync-storage-password` | `username`, `password` | `spec.powersync` present |
| `<name>-meilisearch-master-key` | `masterKey` | `spec.meilisearch` present (unless `masterKeySecretRef` set) |

Secrets are only generated if they don't already exist in the cluster, preventing regeneration on operator restart.

### Database Roles

| Role | Created When | Capabilities |
|------|-------------|-------------|
| `sequin` | `spec.sequin` present | Login, owns `sequin` database |
| `sequin_replication` | `spec.sequin` or `spec.powersync` | Login, replication, bypassrls |
| `powersync_storage` | `spec.powersync` present | Login |

### CDC Permissions Job

When Sequin or Powersync is enabled, a Kubernetes Job runs after the database is ready to grant CDC permissions:
- `USAGE` on `public` schema to `sequin_replication`
- `SELECT` on future tables in `public` schema
- `CREATE` on database for Sequin migrations

## Status Conditions

Monitor deployment progress via status conditions:

```bash
kubectl get supabaseproject my-app -o jsonpath='{.status.conditions}' | jq .
```

| Condition | Description |
|-----------|-------------|
| `CDCReady` | CDC permissions applied |
| `SequinReady` | Sequin deployment available |
| `PowersyncReady` | Powersync deployments available |
| `MeilisearchReady` | Meilisearch StatefulSet ready |

## Troubleshooting

### Sequin not starting
Check that the bundled Redis or external Redis is reachable:
```bash
kubectl logs deploy/<name>-sequin
```

### Powersync replication errors
Verify CDC permissions were applied:
```bash
kubectl get job -l app.kubernetes.io/component=cdc-permissions
kubectl logs job/<name>-cdc-permissions
```

### Meilisearch data persistence
Meilisearch uses a StatefulSet with PVC. Data survives pod restarts. Check PVC status:
```bash
kubectl get pvc -l app.kubernetes.io/component=meilisearch
```
