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
`supabase/gotrue:v2.189.0`, PostgREST `postgrest/postgrest:v14.12`, and
PowerSync `journeyapps/powersync-service:1.21.0` when PowerSync is enabled.

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
| `publishableKey` | Canonical `sb_publishable_<22 Base64URL random>_<8 Base64URL checksum>` client credential (46 characters) |
| `secretKey` | Canonical `sb_secret_<22 Base64URL random>_<8 Base64URL checksum>` backend credential (41 characters), distinct from the publishable key |
| `anonRoleJwt` | ES256 JWT for role `anon`, audience `authenticated` |
| `serviceRoleJwt` | ES256 JWT for role `service_role`, audience `authenticated` |

The operator validates all five fields and their signatures before touching a
dependent workload. It derives a public-only JWKS ConfigMap for PostgREST and
generates an independent, create-once GoTrue fallback secret. Validation
errors set `SecretsReady=False` without putting credential contents in status.
The fallback value is used only by GoTrue; it is not part of the signing-key
array or public JWKS.

### Canonical opaque API keys

The opaque values are the self-hosted format used by Supabase. After the
role-specific prefix, each key has exactly 22 unpadded Base64URL characters,
one underscore separator at that fixed boundary, and exactly 8 unpadded
Base64URL checksum characters. The random segment may itself contain
underscores. The checksum is the first 8 Base64URL characters of SHA-256 over
`supabase-self-hosted|<complete prefix plus random segment>`. This literal
context and algorithm match the pinned Supabase self-hosted v0.7.0 scripts:
[`add-new-auth-keys.sh`](https://github.com/supabase/supabase/blob/self-hosted/v0.7.0/docker/utils/add-new-auth-keys.sh#L125-L139)
and
[`rotate-new-api-keys.sh`](https://github.com/supabase/supabase/blob/self-hosted/v0.7.0/docker/utils/rotate-new-api-keys.sh#L60-L76).
The format is also documented in Supabase's
[`self-hosted-auth-keys.mdx`](https://github.com/supabase/supabase/blob/master/apps/docs/content/guides/self-hosting/self-hosted-auth-keys.mdx#L78-L85).
The evidence and enforcement boundary are collected in
[`docs/research/supabase-self-hosted-opaque-api-keys.md`](docs/research/supabase-self-hosted-opaque-api-keys.md).

The operator rejects noncanonical opaque values before reconciling dependent
workloads; there is no legacy or dual-format compatibility mode. Before
deploying an operator image with this strict validator, rotate both opaque
keys for every existing project and atomically update the matching client and
backend consumers. This rotation is independent of the ES256 signing key and
does not invalidate user sessions.

The operator does not integrate with Infisical. A typical deployment stores
the five values at one Infisical project/environment path and uses the
Infisical Kubernetes operator to synchronize them into the orphaned Secret
above. Keep `signingKeys` as its JSON string; do not wrap the five values in a
second JSON document or store the derived JWKS in Infisical.

Generate a complete bundle with the repository wizard instead of assembling
the fields independently:

```bash
go run ./cmd/project-credentials-wizard
```

Run it yourself in a trusted interactive terminal; it refuses redirected input
or output. The wizard generates the values in process memory, validates the
complete bundle with the operator's production validator, and copies each
field through OSC52 without rendering plaintext or writing credentials to
disk. A rerun creates a new atomic bundle; never combine fields from separate
runs.

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
  syncOptions:
    refreshInterval: 60s
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

## Optional PowerSync profile

Adding `spec.powersync` enables the managed PowerSync 1.21.0 profile. The
default image is used by the API Deployment, the singleton replication
Deployment, and the optional compaction CronJob. An explicit
`spec.powersync.image` override remains available and is applied consistently
to all three managed PowerSync commands.

The API and replication containers expose a named `metrics` port on TCP 9464,
and the generated PowerSync configuration sets
`telemetry.prometheus_port: 9464`. The API Service exposes both HTTP 8080 and
metrics 9464; the replication pod exposes its named metrics port for a pod
scrape. Both roles serve `GET /metrics`. Kubernetes startup, readiness, and
liveness probes use filesystem `Exec` actions; the API's existing HTTP health
route remains available to external monitors. Replication uses a Recreate
rollout so two replication processes do not overlap during replacement.

The operator creates one independent, create-once Secret in the project
namespace: `<project>-powersync-api-token`, with the required key `token`.
PowerSync loads that value through a `secretKeyRef`; it is not part of the
five-field project credential bundle and is never copied into a ConfigMap or
project status. Monitoring in the same namespace can mount this Secret and
use the token as `Authorization: Bearer <token>` for the authenticated
`POST /api/admin/v1/diagnostics` endpoint. A pre-existing non-empty token is
preserved. A missing or invalid key blocks the PowerSync secret phase without
printing the value, and a same-name Secret controlled by another resource is
not adopted.

To rotate the internal token, replace only the `token` key using the approved
Secret management path. The operator does not rotate it during reconciliation;
restart or roll out the API and replication processes (and any compaction
process that is running) so they reload the value. A monitor only needs access
to its mounted Secret and does not need Secret-value API permission. Disabling
PowerSync prevents new PowerSync implementation Secrets from being generated;
existing create-once implementation credentials follow the operator's normal
retention behavior.

For an upgrade, validate the development project first. Confirm the generated
images and named ports, scrape `/metrics` for both roles, authenticate a
Diagnostics request, and run an existing-client smoke check. Also confirm that
the database role Secret bytes, edition-3 sync rules, project credential
identity, and application data remain unchanged. This is an operator
verification procedure; the repository does not claim a live deployment.

The 1.20.4-to-1.21.0 change has no new operational PostgreSQL storage
migration script, but that is not a blanket rollback guarantee. Rolling an
image back does not reverse persistent PowerSync state or database changes.
Keep an appropriate backup and use the recovery/runbook decision for the
observed failure instead of assuming an image rollback restores the prior
state. No sync-rule conversion, dual-running path, or automatic data recovery
is provided by this feature.

## CNPG configuration source of truth

`SupabaseProject` is the single supported customization interface for the
generated CNPG PostgreSQL projection. Its current `database.image`,
`database.parameters`, `database.additionalPreloadLibraries`,
additional roles, platform HBA rules, and platform preload libraries are
assigned exactly on every reconcile, so removing a declaration or correcting a
direct edit converges on the project declaration. Direct edits to those
managed CNPG fields are unsupported and are reconciled away; fields outside
this explicit projection retain their existing CNPG/operator behavior.

An opt-in PostgreSQL 18 CNPG image with PGroonga is built separately from the
operator. See the [PGroonga image guide](docs/operations/postgres-pgroonga.md)
for its build command, published image name, required project preload/GUC
settings, application-owned extension and index migration, and physical
backup/recovery limits. The operator's default PostgreSQL image is unchanged.

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

Recovery bootstrap identity is creation-time state and cannot be changed after
the CNPG Cluster exists. Its destination path, endpoint, server name, exact
bootstrap source, and recovery target remain immutable; the recovery
`s3CredentialsSecret` is operational access and can rotate after bootstrap.
Supported mutable settings (instances, image, resources, superuser access,
PostgreSQL parameters, managed roles, backup plugin, and storage expansion)
continue to converge. Storage shrink is rejected.

Backup and recovery always use distinct ObjectStore names and must use distinct
configured `destinationPath` values. They may point at the same credentials
Secret; use least-privilege IAM so recovery can read its source while backup can
write its destination.

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

Each core service condition and its `availableReplicas` value reflects the
observed Deployment, not merely the existence of its Kubernetes objects. A
core service is ready only after the Deployment controller has observed its
current generation and all desired replicas are updated, ready, and available
with no unavailable replicas. During creation or a partial rollout, pending
components remain `Ready=False`, the project phase is `Provisioning`, and the
aggregate `Ready` condition is false. The project reports `Running` with
aggregate `Ready=True` only after the database, every core service, and any
enabled PowerSync workloads are ready; `observedGeneration` advances to the
current project generation at that point.

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
tokens or Kong routes to continue working. Existing projects with
noncanonical opaque keys must complete the two-key rotation and matching
consumer cutover before the strict operator image is rolled out.
