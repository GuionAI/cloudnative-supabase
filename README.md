# CloudNative Supabase

Deploy Supabase on Kubernetes using Flux CD and CloudNativePG (CNPG).

## Overview

This repository provides a GitOps-ready deployment of Supabase using:
- **CloudNativePG (CNPG)** - Kubernetes-native PostgreSQL operator
- **Flux CD** - GitOps continuous delivery
- **SOPS** - Secrets encryption

## Features

- Full Supabase stack (Auth, Storage, Realtime, PostgREST, Studio, Kong)
- CNPG-managed PostgreSQL with:
  - Automated role management
  - TLS encryption
  - Logical replication support
  - LoadBalancer for external access
- Environment-based overlays (base + environment patches)
- Secret templates with generation scripts

## Quick Start

### Prerequisites

1. Kubernetes cluster (K3s, EKS, GKE, etc.)
2. Flux CD installed: `flux install`
3. SOPS with age encryption configured
4. kubectl access to your cluster

### 1. Fork/Clone this Repository

```bash
git clone https://github.com/GuionAI/cloudnative-supabase.git
cd cloudnative-supabase
```

### 2. Generate Secrets

```bash
cd flux/secrets/templates

# Generate CNPG role passwords
./generate-secrets.sh supabase
# Output: cnpg-secrets.yaml

# Encrypt with your age key
sops --encrypt --age YOUR_AGE_PUBLIC_KEY \
  --encrypted-regex '^(data|stringData)$' \
  cnpg-secrets.yaml > ../example/cnpg-secrets.enc.yaml

rm cnpg-secrets.yaml
```

### 3. Create Supabase Helm Values Secret

```bash
# Copy and edit the template
cp supabase-helm-values.yaml.example ../example/supabase-helm-values.yaml

# Edit with your JWT secrets (generate at supabase.com/docs/guides/self-hosting#api-keys)
# Then encrypt
sops --encrypt --age YOUR_AGE_PUBLIC_KEY \
  --encrypted-regex '^(data|stringData)$' \
  ../example/supabase-helm-values.yaml > ../example/supabase-helm-values.enc.yaml

rm ../example/supabase-helm-values.yaml
```

### 4. Update secrets kustomization

```bash
# Edit flux/secrets/example/kustomization.yaml
cat > ../example/kustomization.yaml <<EOF
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - cnpg-secrets.enc.yaml
  - supabase-helm-values.enc.yaml
EOF
```

### 5. Create SOPS Age Secret in Cluster

```bash
# Create the sops-age secret with your private key
kubectl create secret generic sops-age \
  --namespace=flux-system \
  --from-file=age.agekey=/path/to/your/age.key
```

### 6. Bootstrap Flux

```bash
flux bootstrap github \
  --owner=YOUR_ORG \
  --repository=cloudnative-supabase \
  --branch=main \
  --path=flux/clusters/example
```

## Directory Structure

```
cloudnative-supabase/
├── charts/
│   └── supabase/              # Supabase Helm chart
├── flux/
│   ├── infrastructure/        # CNPG operator
│   │   └── cnpg/
│   ├── sources/               # Helm repositories
│   ├── database/
│   │   ├── base/              # CNPG Cluster (configurable)
│   │   └── example/           # Example environment overlay
│   ├── supabase/
│   │   ├── base/              # Supabase HelmRelease
│   │   └── example/           # Example environment overlay
│   ├── namespace/
│   │   └── example/           # Namespace resource
│   ├── secrets/
│   │   ├── templates/         # Secret generation scripts
│   │   └── example/           # Encrypted secrets (gitignored until encrypted)
│   └── clusters/
│       └── example/           # Flux Kustomizations
└── docs/
```

## Consuming from Another Repository

To use cloudnative-supabase from your own project:

### 1. Add GitRepository Source

```yaml
# flux/sources/cloudnative-supabase.yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: cloudnative-supabase
  namespace: flux-system
spec:
  interval: 1h
  url: https://github.com/GuionAI/cloudnative-supabase
  ref:
    branch: main
```

### 2. Reference Infrastructure

```yaml
# flux/clusters/dev/infrastructure.yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: cloudnative-supabase-infrastructure
  namespace: flux-system
spec:
  sourceRef:
    kind: GitRepository
    name: cloudnative-supabase
  path: ./flux/infrastructure
  # ...
```

### 3. Add Custom Roles via Patches

```yaml
# flux/clusters/dev/database.yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: database
  namespace: flux-system
spec:
  sourceRef:
    kind: GitRepository
    name: cloudnative-supabase
  path: ./flux/database/base
  patches:
    - patch: |
        - op: add
          path: /spec/managed/roles/-
          value:
            name: my_custom_role
            ensure: present
            login: true
            passwordSecret:
              name: my-custom-role-password
      target:
        kind: Cluster
        name: supabase-pg
```

## CNPG Managed Roles

The following roles are created by CNPG:

| Role | Purpose | Secret |
|------|---------|--------|
| supabase_admin | Database owner | cnpg-supabase-admin-password |
| authenticator | PostgREST role switcher | cnpg-authenticator-password |
| supabase_auth_admin | Auth service | cnpg-supabase-auth-password |
| supabase_storage_admin | Storage service | cnpg-supabase-storage-password |
| supabase_realtime_admin | Realtime service | cnpg-supabase-realtime-password |
| pgbouncer | Connection pooling | cnpg-pgbouncer-password |
| anon | RLS policy role (non-login) | N/A |
| authenticated | RLS policy role (non-login) | N/A |
| service_role | RLS policy role (non-login) | N/A |

## Extension Points

Consumers can extend this deployment by:

1. **Adding roles** - Patch the CNPG Cluster to add custom roles
2. **Adding databases** - Create CNPG Database CRDs in your repo
3. **Custom initialization** - Deploy your own db-init jobs
4. **Ingress configuration** - Configure Kong ingress in Supabase HelmRelease

## License

MIT
