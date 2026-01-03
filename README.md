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

- Kubernetes v1.11.3+
- [CloudNativePG operator](https://cloudnative-pg.io/documentation/current/installation_upgrade/) installed
- [CNPG Barman Cloud Plugin](https://github.com/cloudnative-pg/plugin-barman-cloud) (for backup/recovery features)
- kubectl v1.11.3+
- (Optional) [Reloader](https://github.com/stakater/Reloader) - for automatic pod restarts on secret/configmap changes

## Installation

### Install CRDs

```bash
kubectl apply -f https://raw.githubusercontent.com/GuionAI/cloudnative-supabase/main/config/crd/bases/supabase.guion.dev_supabaseprojects.yaml
```

### Deploy Operator

```bash
kubectl apply -f https://raw.githubusercontent.com/GuionAI/cloudnative-supabase/main/dist/install.yaml
```

Or using the Makefile:

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
| `providers.secretRef` | Secret with OAuth credentials | - |

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

## Generated Secrets

The operator auto-generates these secrets:

| Secret | Keys |
|--------|------|
| `{name}-jwt` | `secret`, `anonKey`, `serviceKey` |
| `{name}-supabase-admin-password` | `username`, `password` |
| `{name}-authenticator-password` | `username`, `password` |
| `{name}-auth-admin-password` | `username`, `password` |

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
