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

1. Repair owner metadata on deterministically named, correctly labelled
   durable resources before validating external input.
2. Validate the externally managed `projectCredentialsSecret` bundle.
3. Create-once implementation Secrets (database role passwords, GoTrue
   fallback, optional email hook and PowerSync credentials).
4. Validate immutable recovery intent, then create init SQL and public JWKS
   ConfigMaps.
5. Reconcile independent recovery and steady-state backup resources.
6. Create or reconcile the CNPG Cluster, assigning the complete generated
   PostgreSQL projection (parameters, platform HBA rules, preload libraries,
   and managed roles) from the current SupabaseProject while preserving
   fields outside that projection, rejecting bootstrap mutation and storage
   shrink.
7. Wait for ready database instances.
8. Reconcile Auth, REST, Studio, Meta, Envoy, then optional PowerSync.

When backup and recovery are both enabled, their deterministic ObjectStore
names and configured destination paths must differ. A credentials Secret may be
shared; prefer least-privilege IAM with recovery read access and backup write
access instead of requiring separate Secret objects. Recovery destination,
endpoint, server identity, bootstrap source, and target are immutable after
creation, while its operational credential reference may rotate.

Invalid project credentials stop before dependent workloads are changed.
Valid credential rotation updates a deterministic non-secret pod-template hash
on Auth, REST, Studio, and Envoy so Kubernetes performs only the required
rollouts. Meta and PowerSync consume no bundle value directly and do not roll.
Hashes are not credentials.

`SupabaseProject` is the single source of truth for the generated CNPG
PostgreSQL projection. Callers change `database.parameters` and
`database.additionalRoles` on the project; direct edits to those managed
Cluster fields, platform HBA rules, or preload libraries are unsupported and
are overwritten on the next reconcile. No historical ownership ledger is
consulted or persisted.

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

`publishableKey` and `secretKey` use Supabase's canonical self-hosted opaque-key
format: their role-specific prefix is followed by exactly 22 unpadded
Base64URL random characters, a fixed underscore separator, and exactly 8
unpadded Base64URL checksum characters (total lengths 46 and 41 respectively).
The checksum is the first 8 Base64URL characters of SHA-256 over the literal
`supabase-self-hosted|<complete prefix plus random segment>` context, matching
the pinned self-hosted v0.7.0 generation and rotation scripts. Validation is
strict with no legacy format. Existing projects must rotate both opaque keys
and their matching consumers before deploying the strict validator; this does
not rotate ES256 signing keys or invalidate sessions.

`auth.accessTokenExpirationSeconds` defaults to 3600. Security-owned GoTrue
JWT environment names are rejected in `goTrueEnv`; provider-specific settings
remain supported.

For a new or rotated external credential bundle, use
`fish hack/project-credentials-wizard.fish`. It atomically generates and
production-validates all five fields before handing them to the human through
the `copy` clipboard function. Use the complete output from one run; ad hoc or
field-by-field generators can create internally inconsistent bundles.

## Durable ownership

Never set a SupabaseProject controller owner on the CNPG Cluster, backup or
recovery ObjectStores, or ScheduledBackup. On adoption remove only a matching
SupabaseProject owner reference and preserve foreign metadata. Runtime
Deployments, Services, ConfigMaps, Jobs, CronJobs, and publications remain
owned and are garbage-collectable. Durable events are mapped by namespace,
instance label, and deterministic resource names rather than owner watches.
Same-name resources without the exact project instance label are never adopted
or deleted by cleanup. Envoy's admin listener is loopback-only because its
config dump contains rendered credentials; probes use its public internal
health route instead.

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
