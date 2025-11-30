# Supabase Chart Changelog

This chart is maintained locally with modifications from the upstream [supabase-community/supabase-kubernetes](https://github.com/supabase-community/supabase-kubernetes).

## Changes from Upstream

### Add Database Init Hook (2025-11-30)
- **Added** `templates/hooks/db-init-job.yaml` - Post-install hook to set JWT settings
- **Added** `templates/hooks/db-init-configmap.yaml` - Init script for JWT settings
- **Added** `dbInit` configuration section in values.yaml:
  - `enabled`: Enable/disable the db-init hook (default: false)
  - `host`: Database host (default: supabase-pg-rw)
  - `port`: Database port (default: 5432)
  - `jwtExp`: JWT expiration in seconds (default: 3600)
- **Note**: Sets `app.settings.jwt_secret` and `app.settings.jwt_exp` on postgres database (required for RLS policies)

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
