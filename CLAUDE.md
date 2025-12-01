# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

CloudNative Supabase - A GitOps-ready Supabase deployment using Flux CD and CloudNativePG (CNPG).

## Common Commands

```bash
# Validate Helm chart templates
helm template cnpg-cluster charts/cnpg-cluster --set jwt.secret=test
helm template supabase charts/supabase -f charts/supabase/values.example.yaml

# Validate Kustomization overlays
kustomize build flux/supabase/example

# Generate secrets for a namespace
cd flux/secrets/templates && ./generate-secrets.sh <namespace>

# Encrypt secrets with SOPS
sops --encrypt --age <AGE_PUBLIC_KEY> --encrypted-regex '^(data|stringData)$' secret.yaml > secret.enc.yaml
```

## Architecture

### Core Components

1. **CNPG Operator** (`flux/infrastructure/cnpg/`)
   - Deploys CloudNativePG operator to manage PostgreSQL
   - Installed in `cnpg-system` namespace

2. **CNPG Cluster Helm Chart** (`charts/cnpg-cluster/`)
   - PostgreSQL 16.4 with pgjwt extension
   - Managed roles for Supabase services (configurable)
   - Bootstrap database with JWT settings via `postInitApplicationSQL`
   - Additional databases via CNPG Database CRD
   - LoadBalancer for external access

3. **Supabase Helm Chart** (`charts/supabase/`)
   - Full Supabase stack: Auth, Storage, Realtime, REST, Studio, Kong
   - Per-service database credentials from CNPG secrets
   - db-init hook for grants setup

### Directory Structure

```
charts/
├── cnpg-cluster/       # CNPG Cluster + Database Helm chart
└── supabase/           # Supabase services Helm chart
flux/
├── infrastructure/     # CNPG operator (HelmRelease)
├── sources/            # Helm repositories
├── supabase/
│   ├── base/           # Supabase HelmRelease
│   └── example/        # Example overlay with namespace
├── namespace/
│   └── example/        # Namespace resource
├── secrets/
│   ├── templates/      # Secret generation scripts
│   └── example/        # Encrypted secrets location
└── clusters/
    └── example/        # Flux Kustomizations
```

## Key Files

| File | Purpose |
|------|---------|
| `charts/cnpg-cluster/values.yaml` | CNPG Cluster chart defaults (roles, databases, JWT) |
| `charts/supabase/values.yaml` | Supabase chart defaults |
| `flux/supabase/base/helmrelease.yaml` | Supabase HelmRelease |
| `flux/secrets/templates/generate-secrets.sh` | CNPG password and JWT secret generator |
| `charts/supabase/SECRETS.md` | Required secrets documentation |

## CNPG Managed Roles

Core Supabase roles (consumers can add more via patches):

- `supabase_admin` - Database owner
- `authenticator` - PostgREST role switcher
- `supabase_auth_admin` - Auth service
- `supabase_storage_admin` - Storage service
- `supabase_realtime_admin` - Realtime service
- `anon`, `authenticated`, `service_role` - RLS policy roles

## Extension Pattern

Consumers extend via HelmRelease values patches:

```yaml
# Add custom roles
patches:
  - target:
      kind: HelmRelease
      name: cnpg-cluster
    patch: |
      - op: add
        path: /spec/values/additionalRoles/-
        value:
          name: custom_role
          login: true
          passwordSecret:
            name: custom-role-password

# Add additional databases
      - op: add
        path: /spec/values/additionalDatabases/-
        value:
          name: myapp
          owner: supabase_admin
          extensions:
            - name: uuid-ossp
              ensure: present
```

## Common Tasks

### Adding a New Role
1. Create a K8s secret with password (type: `kubernetes.io/basic-auth`)
2. Patch HelmRelease to add role to `additionalRoles`
3. CNPG automatically creates the role

### Adding a New Database
1. Patch HelmRelease to add database to `additionalDatabases`
2. Specify owner, extensions, and schemas
3. CNPG creates the database via Database CRD

### JWT Configuration
JWT settings are applied via `bootstrap.initdb.postInitApplicationSQLRefs`. The JWT secret is referenced via `jwt.secretRef`:

```yaml
jwt:
  secretRef: supabase-jwt  # Secret must have key "secret"
  expSeconds: 3600         # Token expiration (1 hour default)
```

CNPG injects the secret as env var and substitutes `$(JWT_SECRET)` in the SQL.

### Updating CNPG Operator Version
Edit `flux/infrastructure/cnpg/helmrelease.yaml`:
```yaml
spec:
  chart:
    spec:
      version: "0.26.1"  # Update this
```

## Secret Configuration Pattern

All Supabase services use `secretRef` to reference external secrets - the chart never generates secrets. See `charts/supabase/SECRETS.md` for required keys.

CNPG secrets use `kubernetes.io/basic-auth` type with `username` and `password` keys. The `cnpg.io/reload: "true"` label enables automatic credential rotation.

## Important Notes

1. **No app-specific schemas** - This repo only provides the Supabase platform. App tables/schemas should be in consumer repos.

2. **Database management** - The bootstrap database and additional databases are managed via the cnpg-cluster Helm chart. Consumers can add databases via `additionalDatabases` values.

3. **Secrets are templates** - The `flux/secrets/templates/` contains examples only. Real secrets should be encrypted with SOPS.

4. **PostgreSQL 16.4** - Uses custom image `ghcr.io/guionai/postgres-pgjwt:16.4` with pgjwt extension (built from `Dockerfile`).

5. **Namespace placeholders** - Base manifests use `namespace: PLACEHOLDER` which must be patched in overlays.

6. **Bootstrap vs Database CRD** - The main database is created via CNPG `bootstrap.initdb` (enables `postInitApplicationSQL` for JWT). Extensions and schemas are added via Database CRD.
