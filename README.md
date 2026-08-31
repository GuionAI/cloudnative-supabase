# CloudNative Supabase

A Kubernetes operator that deploys [Supabase](https://supabase.com) using [CloudNativePG](https://cloudnative-pg.io) as the PostgreSQL backend.

## Overview

CloudNative Supabase provides a single `SupabaseProject` Custom Resource that manages the complete Supabase stack:

- **Database**: CloudNativePG PostgreSQL cluster with managed roles and replication
- **Auth**: GoTrue authentication service
- **REST**: PostgREST API service
- **Studio**: Supabase Studio dashboard
- **Meta**: postgres-meta database introspection service
- **Kong**: API gateway with declarative routing
- **PowerSync**: optional offline-first sync with edition 3 Sync Streams

## Features

- Single CRD deploys entire Supabase stack
- All services always enabled (Auth, REST, Studio, Meta, Kong)
- **Backup to S3-compatible storage** (AWS S3, Cloudflare R2, MinIO, etc.)
- **Point-in-time recovery (PITR)** from existing backups
- Auto-generates JWT secrets and database passwords
- Integrates with CNPG for production-grade PostgreSQL
- Owner references for automatic cleanup
- Status conditions for observability

## Prerequisites

- A Kubernetes version supported by your CloudNativePG release
- [CloudNativePG operator](https://cloudnative-pg.io/documentation/current/installation_upgrade/) installed
- [CNPG Barman Cloud Plugin](https://github.com/cloudnative-pg/plugin-barman-cloud) (for backup/recovery features)
- Helm 3.8+
- [Tanka](https://tanka.dev/install/) (for the repository-owned Guion deployment)
- (Optional) [Reloader](https://github.com/stakater/Reloader) - for automatic pod restarts on secret/configmap changes

## Installation

### Install with Helm

```bash
# Replace PUBLISHED_VERSION with a version listed in GitHub Releases.
VERSION="PUBLISHED_VERSION"
helm install cloudnative-supabase \
  oci://ghcr.io/guionai/charts/cloudnative-supabase \
  --namespace cloudnative-supabase-system \
  --create-namespace \
  --version "${VERSION}"
```

The public controller image is available at
`ghcr.io/guionai/cloudnative-supabase` and does not require registry credentials.
The OCI chart becomes installable after a tagged release is published and its
GHCR package has been made public.

### Deploy the Guion operator with Tanka

The self-contained environment in [`tanka/`](tanka/) renders this repository's
chart and CRD without Jsonnet dependencies:

```bash
TANKA_IMAGE=sha-COMMIT make tanka-show
TANKA_IMAGE=sha-COMMIT make tanka-diff
TANKA_IMAGE=sha-COMMIT make tanka-apply
```

This installs only the shared operator in `cnsupa-system`. Application-specific
`SupabaseProject` resources remain owned by their application repositories.

### Install from source

```bash
make deploy IMG=ghcr.io/guionai/cloudnative-supabase:latest
```

## Usage

### Minimal Example

```yaml
apiVersion: supabase.guion.dev/v1alpha1
kind: SupabaseProject
metadata:
  name: my-project
  namespace: my-namespace
spec:
  database:
    instances: 1
    storage:
      size: 10Gi

  auth:
    siteURL: https://app.example.com
    externalURL: https://auth.example.com
```

### Full Example

```yaml
apiVersion: supabase.guion.dev/v1alpha1
kind: SupabaseProject
metadata:
  name: my-project
  namespace: my-namespace
spec:
  database:
    instances: 2
    storage:
      size: 10Gi
      storageClass: local-path
    enableSuperuserAccess: false
    backup:
      enabled: true
      schedule: "0 2 * * *"  # 2 AM daily
      retentionPolicy: "30d"
      destinationPath: s3://my-bucket/backups/my-project/
      endpointURL: https://s3.us-east-1.amazonaws.com
      s3CredentialsSecret: my-s3-credentials
      walCompression: gzip
      dataCompression: gzip

  auth:
    siteURL: https://app.example.com
    externalURL: https://auth.example.com
    autoConfirmEmail: true
    goTrueEnv:
      - name: GOTRUE_EXTERNAL_PHONE_ENABLED
        valueFrom:
          configMapKeyRef:
            name: auth-settings
            key: external-phone-enabled
      - name: GOTRUE_SMS_TWILIO_AUTH_TOKEN
        valueFrom:
          secretKeyRef:
            name: twilio
            key: auth-token
    providers:
      google:
        enabled: true
        skipNonceCheck: true
      apple:
        enabled: true
      secretRef: auth-providers

  rest:
    schemas:
      - public

  studio:
    publicURL: https://studio.example.com
    organizationName: My Org
    projectName: My Project

  # meta and kong use defaults
```

### Recovery Example

To restore from a backup to a point in time:

```yaml
apiVersion: supabase.guion.dev/v1alpha1
kind: SupabaseProject
metadata:
  name: my-project-restored
  namespace: my-namespace
spec:
  database:
    instances: 1
    storage:
      size: 10Gi
    recovery:
      enabled: true
      serverName: my-project  # Original cluster name
      targetTime: "2026-01-01T12:00:00Z"
      destinationPath: s3://my-bucket/backups/my-project/
      endpointURL: https://s3.us-east-1.amazonaws.com
      s3CredentialsSecret: my-s3-credentials

  auth:
    siteURL: https://app.example.com
    externalURL: https://auth.example.com
```

### Check Status

```bash
kubectl get supabaseproject my-project -o yaml
```

Status conditions:

- `Ready` - Overall readiness
- `SecretsReady` - JWT and database secrets generated
- `DatabaseReady` - CNPG cluster is ready
- `BackupReady` - Backup infrastructure configured
- `RecoveryReady` - Recovery infrastructure configured
- `AuthReady` - GoTrue is running
- `RestReady` - PostgREST is running
- `StudioReady` - Studio is running
- `MetaReady` - postgres-meta is running
- `KongReady` - Kong gateway is running
- `PowersyncReady` - optional PowerSync service is running

## Configuration

### Database

| Field | Description | Default |
|-------|-------------|---------|
| `instances` | Number of PostgreSQL instances | 1 |
| `storage.size` | Storage size | Required |
| `storage.storageClass` | Storage class | Cluster default |
| `image` | PostgreSQL image | `ghcr.io/guionai/postgres-pgjwt:17.6` |
| `enableSuperuserAccess` | Enable superuser access | false |

### Backup

| Field | Description | Default |
|-------|-------------|---------|
| `backup.enabled` | Enable scheduled backups | false |
| `backup.schedule` | Cron schedule for backups | `0 0 * * *` |
| `backup.retentionPolicy` | Backup retention policy | `30d` |
| `backup.destinationPath` | S3 path (e.g., `s3://bucket/path/`) | Required |
| `backup.endpointURL` | S3 endpoint URL | - |
| `backup.s3CredentialsSecret` | Secret with ACCESS_KEY_ID and SECRET_ACCESS_KEY | Required |
| `backup.walCompression` | WAL compression (gzip, bzip2, snappy, none) | - |
| `backup.dataCompression` | Data compression (gzip, bzip2, snappy, none) | - |

### Recovery (PITR)

| Field | Description | Default |
|-------|-------------|---------|
| `recovery.enabled` | Enable point-in-time recovery | false |
| `recovery.serverName` | Original cluster name in backup | Required |
| `recovery.targetTime` | Recovery target time (RFC 3339) | Latest |
| `recovery.destinationPath` | S3 path to backup source | Required |
| `recovery.endpointURL` | S3 endpoint URL | - |
| `recovery.s3CredentialsSecret` | Secret with S3 credentials | Required |

**Note**: Backup and recovery cannot both be enabled simultaneously.

### Auth (GoTrue)

| Field | Description | Default |
|-------|-------------|---------|
| `siteURL` | Application URL | Required |
| `externalURL` | Auth service URL | Required |
| `autoConfirmEmail` | Skip email confirmation | false |
| `enableAnonymousUsers` | Allow sign-in without email or another identity | false |
| `goTrueEnv` | Add or override any `GOTRUE_*` variable from one Secret/ConfigMap key | - |
| `providers.secretRef` | Secret with OAuth credentials | - |

Anonymous users still use the `authenticated` PostgreSQL role and receive an
`is_anonymous` JWT claim. Add abuse controls before enabling anonymous sign-ins
on a publicly advertised service.

`auth.goTrueEnv` is the escape hatch for `GOTRUE_*` configuration not yet
modeled by a dedicated field. Each item supplies exactly one Secret or
ConfigMap key, so credentials cannot be stored in the custom resource. A
same-named entry replaces the value generated from the higher-level Auth
fields.

### REST (PostgREST)

| Field | Description | Default |
|-------|-------------|---------|
| `schemas` | Exposed schemas | `[public]` |

### Studio

| Field | Description | Default |
|-------|-------------|---------|
| `publicURL` | Studio public URL | - |
| `organizationName` | Organization name in UI | Default Organization |
| `projectName` | Project name in UI | Default Project |

### PowerSync

PowerSync is optional. Exactly one of `syncRules.inline` or
`syncRules.configMapRef` is required when it is enabled. The content must use
edition 3 Sync Streams; the operator does not install a broad default stream.

```yaml
spec:
  powersync:
    api:
      replicas: 1
    syncRules:
      inline: |
        config:
          edition: 3
        streams:
          notes:
            auto_subscribe: true
            query: SELECT id, title FROM notes WHERE user_id = auth.user_id()
```

The operator creates the two database roles, grants CDC access, creates the
`powersync` publication, and runs separate API and replication deployments.

## Generated Secrets

The operator auto-generates these secrets:

| Secret | Keys |
|--------|------|
| `{name}-jwt` | `secret`, `anonKey`, `serviceKey` |
| `{name}-supabase-admin-password` | `username`, `password` |
| `{name}-authenticator-password` | `username`, `password` |
| `{name}-auth-admin-password` | `username`, `password` |
| `{name}-powersync-storage-password` | `username`, `password` |
| `{name}-powersync-replication-password` | `username`, `password` |

To use an existing JWT secret, set `spec.jwt.secretRef`.

## Development

```bash
# Run tests
go test ./... -v

# Generate manifests
make generate manifests

# Run locally
make run

# Build image
make docker-build IMG=my-registry/cloudnative-supabase:dev
```

## Uninstall

```bash
# Delete SupabaseProject resources
kubectl delete supabaseproject --all -A

# Uninstall operator
make undeploy

# Remove CRDs
make uninstall
```

## License

Copyright 2026 GuionAI.

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for details.
