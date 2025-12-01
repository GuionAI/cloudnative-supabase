# Supabase Chart Changelog

This chart is maintained locally with modifications from the upstream [supabase-community/supabase-kubernetes](https://github.com/supabase-community/supabase-kubernetes).

## Changes from Upstream

### Remove unused services (2025-12-01)
- **Removed** Analytics (Logflare) service completely:
  - `templates/analytics/` directory
  - `analytics` section from values.yaml
  - `secret.analytics` from values.yaml
  - Analytics route from Kong config
  - LOGFLARE_URL/LOGFLARE_API_KEY from Studio deployment
- **Removed** Vector service completely:
  - `templates/vector/` directory
  - `vector` section from values.yaml
- **Removed** Functions (Edge Runtime) service completely:
  - `templates/functions/` directory
  - `functions` section from values.yaml
  - Functions route from Kong config
- **Removed** Minio service completely:
  - `templates/minio/` directory
  - `minio` section from values.yaml
- **Removed** Imgproxy service completely:
  - `templates/imgproxy/` directory
  - `imgproxy` section from values.yaml
- **Updated** documentation (SECRETS.md, README.md, CLAUDE.md)

### Move JWT settings to cnpg-cluster chart (2025-12-01)
- **Removed** JWT SQL from db-init hook (now handled by cnpg-cluster bootstrap.postInitApplicationSQL)
- **Removed** `dbInit.jwtExp` from values.yaml
- **Removed** `JWT_SECRET` and `JWT_EXP` env vars from db-init job
- **Note**: db-init hook now only sets up grants for API roles

### Add Database Init Hook (2025-11-30)
- **Added** `templates/hooks/db-init-job.yaml` - Post-install hook for grants setup
- **Added** `templates/hooks/db-init-configmap.yaml` - Init script for grants
- **Added** `dbInit` configuration section in values.yaml:
  - `host`: Database host (default: supabase-pg-rw)
  - `port`: Database port (default: 5432)
  - `database`: Database name (default: supabase)

### Remove Built-in PostgreSQL and Sequin Init (2025-11-30)
- **Removed** `templates/db/` directory - PostgreSQL deployment, service, volume, helpers, initdb/migration configs
- **Removed** `templates/hooks/` directory - Sequin CDC init job and configmap
- **Removed** `templates/secrets/db.yaml` - DB credentials secret template
- **Removed** `templates/test/db.yaml` - DB test pod
- **Removed** `db:` and `sequin:` configuration sections from values.yaml
- **Removed** `db.enabled` conditionals from all deployment templates (auth, analytics, functions, meta, realtime, rest, storage)
- **Removed** `db` route from vector config
- **Removed** `docker/` and `ci/` directories (no longer needed without built-in DB)
- **Note**: PostgreSQL is now managed externally by CloudNativePG operator

### Kong Ingress Defaults (2025-11-26)
- **Changed** `kong.ingress.className` from `nginx` to `traefik`
- **Added** `kong.ingress.annotations` with cert-manager cluster-issuer
- **Changed** `kong.ingress.tls` to enable TLS with placeholder host
- **Changed** `kong.ingress.hosts` placeholder from `example.com` to `kong.example.com`

### Default Values and Studio Secret Support (2025-11-26)
- **Added** pinned image tags for all services (replacing `latest`):
  - db: `15.8.1.085`
  - studio: `2025.11.10-sha-5291fe3`
  - auth: `v2.182.1`
  - rest: `v13.0.7`
  - meta: `v0.93.1`
  - kong: `2.8.1`
  - analytics: `1.22.6`
  - vector: `0.28.1-alpine`
- **Changed** default enabled flags for unused services:
  - storage: `false` (was true)
  - imgproxy: `false` (was true)
  - functions: `false` (was true)
  - realtime: `false` (was true)
- **Added** studio environment defaults for internal service communication:
  - `POSTGRES_HOST: "supabase-supabase-db"`
  - `POSTGRES_PORT: "5432"`
  - `POSTGRES_DB: "postgres"`
- **Added** `studio.secret.db.secretRef` support in values.yaml for POSTGRES_PASSWORD (default: `supabase-db`)
- **Modified** `templates/studio/deployment.yaml` to inject POSTGRES_PASSWORD from external secret when `studio.secret.db.secretRef` is set

### Initial Import (2025-11-26)
- Imported chart from upstream for local management

## Upstream Version
- Based on upstream version: `0.1.3`
- Upstream repository: https://github.com/supabase-community/supabase-kubernetes
