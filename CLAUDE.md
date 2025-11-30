# CLAUDE.md

This file provides guidance to Claude Code when working with this repository.

## Project Overview

CloudNative Supabase - A GitOps-ready Supabase deployment using Flux CD and CloudNativePG (CNPG).

## Architecture

### Core Components

1. **CNPG Operator** (`flux/infrastructure/cnpg/`)
   - Deploys CloudNativePG operator to manage PostgreSQL
   - Installed in `cnpg-system` namespace

2. **CNPG Cluster** (`flux/database/base/cluster.yaml`)
   - PostgreSQL 16.4 with pgjwt extension
   - Managed roles for Supabase services
   - Logical replication enabled for CDC tools
   - LoadBalancer for external access

3. **Supabase Helm Chart** (`charts/supabase/`)
   - Full Supabase stack: Auth, Storage, Realtime, REST, Studio, Kong
   - Per-service database credentials from CNPG secrets

### Directory Structure

```
flux/
├── infrastructure/     # CNPG operator (HelmRelease)
├── sources/           # Helm repositories
├── database/
│   ├── base/          # CNPG Cluster definition
│   └── example/       # Example overlay with namespace
├── supabase/
│   ├── base/          # Supabase HelmRelease
│   └── example/       # Example overlay with namespace
├── namespace/
│   └── example/       # Namespace resource
├── secrets/
│   ├── templates/     # Secret generation scripts
│   └── example/       # Encrypted secrets location
└── clusters/
    └── example/       # Flux Kustomizations
```

## Key Files

| File | Purpose |
|------|---------|
| `flux/database/base/cluster.yaml` | CNPG Cluster with managed roles |
| `flux/supabase/base/helmrelease.yaml` | Supabase HelmRelease |
| `flux/secrets/templates/generate-secrets.sh` | CNPG password generator |
| `charts/supabase/values.yaml` | Supabase chart defaults |

## CNPG Managed Roles

Core Supabase roles (consumers can add more via patches):

- `supabase_admin` - Database owner
- `authenticator` - PostgREST role switcher
- `supabase_auth_admin` - Auth service
- `supabase_storage_admin` - Storage service
- `supabase_realtime_admin` - Realtime service
- `pgbouncer` - Connection pooling
- `anon`, `authenticated`, `service_role` - RLS policy roles

## Extension Pattern

Consumers extend via Flux Kustomization patches:

```yaml
patches:
  - patch: |
      - op: add
        path: /spec/managed/roles/-
        value:
          name: custom_role
          login: true
          passwordSecret:
            name: custom-role-password
    target:
      kind: Cluster
      name: supabase-pg
```

## Common Tasks

### Adding a New Role
1. Consumer creates a K8s secret with password
2. Consumer patches CNPG Cluster to add role
3. CNPG automatically creates the role

### Modifying Supabase Configuration
1. Edit `flux/supabase/base/helmrelease.yaml` for base changes
2. Or patch via consumer's Kustomization for environment-specific changes

### Updating CNPG Version
Edit `flux/infrastructure/cnpg/helmrelease.yaml`:
```yaml
spec:
  chart:
    spec:
      version: "0.26.1"  # Update this
```

## Important Notes

1. **No app-specific schemas** - This repo only provides the Supabase platform. App tables/schemas should be in consumer repos.

2. **No Database CRDs** - Consumers create their own CNPG Database resources for their databases.

3. **Secrets are templates** - The `flux/secrets/templates/` contains examples only. Real secrets should be encrypted with SOPS.

4. **PostgreSQL 16.4** - Uses custom image `ghcr.io/guionai/postgres-pgjwt:16.4` with pgjwt extension.
