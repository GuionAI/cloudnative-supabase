# CloudNative Supabase maintainer guidance

CloudNative Supabase is a Kubernetes operator that deploys a fixed Supabase
profile on CloudNativePG: PostgreSQL, GoTrue Auth, PostgREST, Studio,
postgres-meta, Envoy, and optional PowerSync. All core services are always
deployed. There is no Kong, HS256, legacy JWT secret, or hybrid mode.

## Repository layout

```
api/v1alpha1/                    # SupabaseProject CRD types
cmd/main.go                      # operator entrypoint
internal/controller/             # phased reconciliation
internal/resources/cnpg/         # Cluster, ObjectStore, ScheduledBackup
internal/resources/configmaps/   # init SQL, Envoy, JWKS, PowerSync
internal/resources/deployments/ # Auth, REST, Studio, Meta, Envoy, PowerSync
internal/resources/secrets/      # generated implementation Secrets
internal/resources/services/     # Service builders
pkg/crypto/                      # ES256 validation and random values
config/crd/bases/                # generated CRD
charts/cloudnative-supabase/crds # chart copy of generated CRD
```

## Reconciliation flow

1. Validate the externally managed `projectCredentialsSecret` bundle.
2. Create-once implementation Secrets (database role passwords, GoTrue
   fallback, optional email hook and PowerSync credentials).
3. Create init SQL and public JWKS ConfigMaps.
4. Reconcile independent recovery and steady-state backup resources.
5. Create or reconcile the CNPG Cluster, preserving foreign/defaulted fields,
   rejecting bootstrap mutation and storage shrink.
6. Wait for ready database instances.
7. Reconcile Auth, REST, Studio, Meta, Envoy, then optional PowerSync.

Invalid project credentials stop before dependent workloads are changed.
Valid credential rotation updates a deterministic non-secret pod-template hash
on every consumer so Kubernetes performs a rollout. Hashes are not
credentials.

## Credential and service boundaries

The external Secret has exactly `signingKeys`, `publishableKey`, `secretKey`,
`anonRoleJwt`, and `serviceRoleJwt`. `signingKeys` is one private P-256 ES256
JWK; the operator derives a public-only JWKS for PostgREST and verification
consumers. GoTrue receives the private array plus its independent fallback
secret and normalized Auth issuer. Envoy receives opaque keys and internal role
tokens. Studio receives opaque keys and the internal role-token variables it
supports. PostgREST receives only public JWKS. PowerSync uses the Auth JWKS URL,
audience `authenticated`, and disabled Supabase HMAC mode; it has no JWT
secret environment variable.

`auth.accessTokenExpirationSeconds` defaults to 3600. Security-owned GoTrue
JWT environment names are rejected in `goTrueEnv`; provider-specific settings
remain supported.

## Durable ownership

Never set a SupabaseProject controller owner on the CNPG Cluster, backup or
recovery ObjectStores, or ScheduledBackup. On adoption remove only a matching
SupabaseProject owner reference and preserve foreign metadata. Runtime
Deployments, Services, ConfigMaps, Jobs, CronJobs, and publications remain
owned and are garbage-collectable. Durable events are mapped by namespace,
instance label, and deterministic resource names rather than owner watches.

## Commands

```bash
go build ./...
go test ./...
make generate manifests
make test test-tanka test-delivery
make lint
```

Regenerate deepcopy and CRD output from API types; do not hand-edit generated
files. Copy the generated CRD to the chart after `make manifests`.

Code review and deployment are separate workflow steps. Do not mutate a live
cluster while implementing or testing this repository.
