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
- Auto-generates JWT secrets and database passwords
- Integrates with CNPG for production-grade PostgreSQL
- Owner references for automatic cleanup
- Status conditions for observability

## Prerequisites

- Kubernetes v1.11.3+
- [CloudNativePG operator](https://cloudnative-pg.io/documentation/current/installation_upgrade/) installed
- kubectl v1.11.3+

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
    instances: 1
    storage:
      size: 10Gi
      storageClass: local-path
    enableSuperuserAccess: false

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

### Check Status

```bash
kubectl get supabaseproject my-project -o yaml
```

Status conditions:

- `Ready` - Overall readiness
- `SecretsReady` - JWT and database secrets generated
- `DatabaseReady` - CNPG cluster is ready
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
