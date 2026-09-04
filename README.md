# CloudNative Supabase

CloudNative Supabase is a Kubernetes operator that manages a focused,
rebuildable Supabase platform on CloudNativePG (CNPG). A `SupabaseProject`
creates PostgreSQL, GoTrue Auth, PostgREST, Studio, postgres-meta, and an
Envoy API gateway. PowerSync is optional.

This branch is an intentional breaking release. It has one authentication
architecture: opaque `sb_publishable_*` and `sb_secret_*` API credentials,
ES256 user sessions, and public-key verification. Kong, HS256 project JWTs,
and the legacy `jwt`/`secrets` CRD fields are removed.

## Architecture

Envoy listens on the `<project>-api-gw` Service at port 8000. It accepts only
the two configured opaque API keys and translates them to pre-signed `anon`
and `service_role` ES256 role tokens before routing to Auth, REST, Meta, or
Studio. GoTrue owns user-session signing; PostgREST and PowerSync verify with
public JWKS. No verifier receives a symmetric signing secret.

The Envoy admin API (including credential-bearing `config_dump`) is bound to
the pod loopback interface. Liveness and readiness use the harmless public
`/_internal/health` route instead, so other pods can reach a useful probe but
cannot inspect rendered gateway configuration.

The managed profile deliberately excludes Storage, Realtime, Functions,
Analytics, and other upstream services that this operator does not deploy.

The Envoy assets are adapted from the official self-hosted Supabase assets at
upstream commit
[`95ca3024398080ff18c9abcd1c6c8beae73fd9e1`](https://github.com/supabase/supabase/commit/95ca3024398080ff18c9abcd1c6c8beae73fd9e1).
Pinned images are Envoy `envoyproxy/envoy:v1.39.0`, GoTrue
`supabase/gotrue:v2.189.0`, and PostgREST `postgrest/postgrest:v14.12`.

## Project credentials

Every project must reference one externally managed Secret in its namespace:

```yaml
spec:
  projectCredentialsSecret: my-project-credentials
```

The Secret contains exactly these string fields:

| Key | Meaning |
| --- | --- |
| `signingKeys` | JSON array containing exactly one signing-capable P-256 ES256 private JWK with a non-empty `kid` |
| `publishableKey` | Opaque `sb_publishable_*` client credential |
| `secretKey` | Opaque `sb_secret_*` backend credential, distinct from the publishable key |
| `anonRoleJwt` | ES256 JWT for role `anon`, audience `authenticated` |
| `serviceRoleJwt` | ES256 JWT for role `service_role`, audience `authenticated` |

The operator validates all five fields and their signatures before touching a
dependent workload. It derives a public-only JWKS ConfigMap for PostgREST and
generates an independent, create-once GoTrue fallback secret. Validation
errors set `SecretsReady=False` without putting credential contents in status.
The fallback value is used only by GoTrue; it is not part of the signing-key
array or public JWKS.

The operator does not integrate with Infisical. A typical deployment stores
the five values at one Infisical project/environment path and uses the
Infisical Kubernetes operator to synchronize them into the orphaned Secret
above. Keep `signingKeys` as its JSON string; do not wrap the five values in a
second JSON document or store the derived JWKS in Infisical.

For example, an Infisical `InfisicalStaticSecret` can target the same
namespace with `creationPolicy: Orphan` (the auth objects and credentials are
created separately):

```yaml
apiVersion: secrets.infisical.com/v1beta1
kind: InfisicalStaticSecret
metadata:
  name: example-project-credentials-sync
  namespace: supabase
spec:
  infisicalAuthRef:
    name: infisical-auth
    namespace: supabase
  sources:
    - projectId: <infisical-project-id>
      environmentSlug: dev
      secretPath: /supabase/example
  targets:
    - name: example-project-credentials
      namespace: supabase
      kind: Secret
      creationPolicy: Orphan
```

That path contains exactly `signingKeys`, `publishableKey`, `secretKey`,
`anonRoleJwt`, and `serviceRoleJwt`; the resulting Secret remains independent
of the `SupabaseProject` owner lifecycle.

## Example

```yaml
apiVersion: supabase.guion.dev/v1alpha1
kind: SupabaseProject
metadata:
  name: example
  namespace: supabase
spec:
  projectCredentialsSecret: example-project-credentials
  database:
    instances: 1
    storage:
      size: 20Gi
      storageClass: local-path
  auth:
    siteURL: https://app.example.com
    externalURL: https://auth.example.com
    accessTokenExpirationSeconds: 3600
  gateway:
    replicas: 1
```

All core services are always deployed. `rest`, `studio`, `meta`, and
`gateway` fields customize images, replicas, and resources; omission uses the
operator defaults. `auth.goTrueEnv` remains available for provider settings,
but JWT keys, fallback secret, key ID, issuer, audience, lifetime, valid
methods, and role settings are operator-owned and cannot be overridden.

## Recovery and steady-state backup

Recovery and backup are independent. A new cluster can recover from one
object store and immediately archive WAL and schedule backups to another:

```yaml
spec:
  database:
    storage: {size: 100Gi}
    recovery:
      enabled: true
      serverName: source-cluster
      destinationPath: s3://recovery-bucket/source
      s3CredentialsSecret: recovery-s3
    backup:
      enabled: true
      destinationPath: s3://backup-bucket/example
      s3CredentialsSecret: backup-s3
      schedule: "0 0 2 * * *"
      retentionPolicy: 30d
```

Recovery bootstrap is creation-time state and cannot be changed after the
CNPG Cluster exists. Supported mutable settings (instances, image, resources,
superuser access, PostgreSQL parameters, managed roles, backup plugin, and
storage expansion) continue to converge. Storage shrink is rejected.

## Lifecycle and deletion

The CNPG Cluster, recovery/backup ObjectStores, and ScheduledBackup are
durable resources and are not owned by `SupabaseProject`. Deleting a project
therefore garbage-collects runtime services and generated configuration while
retaining the database and backup infrastructure. Recreating the same project
name adopts those retained resources without replacing the database. Durable
resources are mapped back to projects by namespace, deterministic name, and
an exact instance label; missing or foreign labels are never adopted or
deleted.

Explicit deletion of retained resources belongs to the migration runbook. For
preserved-project cutovers, keep the old database for the agreed minimum
72-hour observation period before deleting it.

## Status and endpoints

The status conditions include `SecretsReady`, `DatabaseReady`,
`BackupReady`, `RecoveryReady`, `AuthReady`, `RestReady`, `StudioReady`,
`MetaReady`, `GatewayReady`, and optional PowerSync/CDC conditions. The API
endpoint is `<project>-api-gw:8000`; the database endpoint is the CNPG
`<project>-pg-rw:5432` Service.

## Development and verification

```bash
go test ./...
go build ./...
make generate manifests
make test test-tanka test-delivery
make lint                 # when the pinned linter is available
```

`make test` uses test-owned envtest binaries. No command above deploys an
operator or mutates a live cluster.

## Breaking upgrade note

There is no compatibility or hybrid mode. Replace manifests using
`spec.jwt`, `spec.secrets`, or `spec.kong` with `projectCredentialsSecret`
and `gateway`, provision the five external credential fields, and plan a
coordinated client cutover to opaque keys and ES256 sessions. Existing
databases can be retained and readopted; callers must not expect old HS256
tokens or Kong routes to continue working.
