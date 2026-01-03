# CLAUDE.md

This file provides guidance to Claude Code when working with code in this repository.

## Project Overview

CloudNative Supabase is a Kubernetes operator that deploys Supabase using CloudNativePG (CNPG) as the PostgreSQL backend. It creates a single `SupabaseProject` CRD that manages:

- CNPG PostgreSQL cluster with managed roles
- GoTrue (Auth service) - always enabled
- PostgREST (REST API) - always enabled
- Studio (Dashboard) - always enabled
- postgres-meta (Database introspection) - always enabled
- Kong (API Gateway) - always enabled

**Note**: All services are always deployed. There are no `enabled` flags - the operator deploys the full Supabase stack.

## Project Structure

```
cloudnative-supabase/
├── api/v1alpha1/                    # CRD types
│   └── supabaseproject_types.go     # SupabaseProject spec/status
├── cmd/main.go                      # Operator entrypoint
├── internal/
│   ├── controller/                  # Reconciliation controller
│   │   └── supabaseproject_controller.go
│   └── resources/                   # Kubernetes resource builders
│       ├── cnpg/cluster.go          # CNPG Cluster builder
│       ├── configmaps/              # ConfigMap builders (init SQL, Kong config)
│       ├── deployments/             # Deployment builders (auth, rest, studio, meta, kong)
│       ├── secrets/secrets.go       # Secret generation
│       └── services/services.go     # Service builders
├── pkg/crypto/                      # JWT and password generation
│   ├── jwt.go                       # HS256 JWT tokens
│   └── password.go                  # Secure random passwords
└── config/
    ├── crd/bases/                   # Generated CRD YAML
    ├── manager/                     # Operator deployment
    ├── rbac/                        # RBAC rules
    └── samples/                     # Example SupabaseProject CR
```

## Commands

```bash
# Build
go build ./...

# Run tests
go test ./pkg/... -v           # Unit tests
go test ./internal/... -v      # Controller tests (requires envtest)

# Generate CRD and code
make generate manifests

# Run locally (requires kubeconfig)
make run

# Build and push Docker image
make docker-build docker-push IMG=ghcr.io/guionai/cloudnative-supabase:latest

# Install CRDs
make install

# Deploy operator
make deploy IMG=ghcr.io/guionai/cloudnative-supabase:latest
```

## Architecture

### Reconciliation Flow

The controller uses a phased approach:

1. **Secrets Phase** - Generate JWT secret + DB role passwords (or sync from existing)
2. **InitSQL Phase** - Create ConfigMap with database initialization SQL
3. **CNPG Cluster Phase** - Create CNPG Cluster with managed roles
4. **Wait Database Phase** - Wait for CNPG to report ready instances
5. **Services Phase** - Deploy Auth, REST, Studio, Meta, Kong

### Secret Management

The operator generates these secrets if not provided:

| Secret | Keys | Purpose |
|--------|------|---------|
| `{name}-jwt` | secret, anonKey, serviceKey | JWT signing + pre-generated tokens |
| `{name}-supabase-admin-password` | username, password | DB owner role |
| `{name}-authenticator-password` | username, password | PostgREST role |
| `{name}-auth-admin-password` | username, password | GoTrue role |

**Important**: The controller checks if secrets exist in the cluster (not just status) to prevent regenerating secrets on operator restart.

### CNPG Managed Roles

| Role | Purpose |
|------|---------|
| `supabase_admin` | DB owner, createdb, createrole, bypassrls |
| `authenticator` | PostgREST role switcher |
| `supabase_auth_admin` | Auth service admin |
| `anon` | Anonymous API access (non-login) |
| `authenticated` | Authenticated user role (non-login) |
| `service_role` | Service role with bypassrls (non-login) |

## Key Patterns

### Owner References
All created resources have owner references to the SupabaseProject, enabling garbage collection on delete.

### Condition Updates
Use `r.setCondition()` to update status conditions. Always update status after setting conditions on error paths.

### Resource Builders
Builder functions in `internal/resources/` take the project and return Kubernetes resources. They should be pure functions with no side effects.

## Known Issues

See [GitHub Issue #1](https://github.com/GuionAI/cloudnative-supabase/issues/1) for the comprehensive code review findings and action items.

## Releasing

To release a new version:

```bash
# 1. Ensure changes are pushed to main
git push

# 2. Create and push a version tag
git tag v0.1.X && git push origin v0.1.X
```

The GitHub Actions release workflow (`.github/workflows/release.yaml`) will automatically:
- Run tests
- Build and push Docker image to `ghcr.io/guionai/cloudnative-supabase:<version>`
- Create a GitHub Release with auto-generated release notes
- Update Helm chart version in `charts/cloudnative-supabase/`

## Dependencies

- CloudNativePG (CNPG) operator must be installed in the cluster
- Requires Kubernetes 1.11.3+
