# Modern Supabase Platform implementation report

## Run identity

- Feature branch: `modern-supabase-platform`
- Starting `HEAD`: `9995056` (`feat(auth): allow custom GoTrue environment (#18)`)
- Implementation commit: `f3fd3b4` (`feat(platform): implement modern Supabase platform`)
- Review-fix commits: `5492aa6` (`fix(platform): harden modern Supabase reconciliation`),
  `d9901f6` (`fix(platform): retain initdb bootstrap guard`),
  `9d90d95` (`fix(recovery): validate bootstrap intent before cleanup`),
  `993e28d` (`fix(platform): tighten durable and recovery contracts`),
  `8b29173` (`fix(platform): finalize gateway and recovery contracts`),
  `1d029bb` (`fix(platform): clear readiness and remove inert ingress`),
  `84f190f` (`fix(cnpg): track retained field ownership`),
  `0a7e0f2` (`fix(platform): align recovery validation seam`), and
  `eba0d90` (`test(platform): cover production recovery validation`)
- Worker pane: `w5P:p6`
- Owner pane: `w5P:p1`
- Scope: the complete `spec.md` and tickets 01–05, in dependency order

## Delivered acceptance criteria

- **01 — ES256 project credentials:** replaced the legacy JWT/secret API with the required five-field external project credential Secret; validation covers the private P-256 ES256 signing JWK, opaque key prefixes, role JWT claims/signatures, missing and malformed fields, and safe status errors. A public-only JWKS is derived for verifiers. GoTrue receives a create-once independent fallback secret, ES256-only methods, normalized issuer, audience, and configurable access-token lifetime. PostgREST, Studio, and PowerSync receive only their permitted material. External Secret watches preserve the last valid workloads on invalid rotation and hash valid rotations into affected pod templates.
- **02 — Envoy opaque gateway:** removed Kong, legacy/hybrid paths, and the inert GatewaySpec ingress surface; added the gateway-neutral CRD/status/service/deployment contract, pinned Envoy `1.39.0`, GoTrue `2.189.0`, and PostgREST `14.12`, and added attributed Envoy assets from upstream commit `95ca3024398080ff18c9abcd1c6c8beae73fd9e`. The gateway accepts only configured `sb_publishable_*`/`sb_secret_*` values and translates them to the corresponding internal role JWTs; private signing material and the GoTrue fallback remain excluded.
- **03 — Recovery plus steady state:** backup and recovery can be specified together with distinct object stores. CNPG reconciliation converges supported mutable fields while preserving foreign/defaulted fields, expands storage, rejects shrink, and reports immutable recovery bootstrap changes as not-ready desired state.
- **04 — Retained/readopted databases:** CNPG, backup/recovery ObjectStores, and ScheduledBackups are durable and not project-owned; existing project owner references are removed without disturbing unrelated metadata. Runtime resources remain garbage-collectable. Explicit namespace-safe label/name watches and same-name adoption preserve reconciliation after deletion and recreation.
- **05 — Published contract:** README, CLAUDE guidance, samples, glossary/ADRs, generated deepcopy, and both CRD copies describe the same modern ES256/Envoy and retained-database platform. Obsolete Kong/HS256 source and tests were removed.

## Changed paths

- API contract and generated artifacts: `api/v1alpha1/`, `config/crd/`, `charts/cloudnative-supabase/crds/`, and `config/samples/`.
- Reconciliation and lifecycle: `internal/controller/supabaseproject_controller.go` plus controller tests.
- Authentication and credential handling: `pkg/crypto/es256.go`, `internal/resources/secrets/`, `internal/resources/common/`, Auth/REST/Studio/PowerSync builders, and credential/URL tests.
- Gateway: `internal/resources/configmaps/envoy.go`, `internal/resources/deployments/gateway.go`, gateway service wiring, and Envoy tests; Kong builders/config/tests and the old HS256 package were deleted.
- CNPG/backup durability and mutable reconciliation: `internal/resources/cnpg/` and controller coverage.
- Maintainer/user documentation and accepted design records: `README.md`, `CLAUDE.md`, `CONTEXT.md`, and `docs/adr/0001`–`0003`.

## Verification evidence

All commands completed successfully after the final review-fix changes:

```text
make generate manifests
make test test-tanka test-delivery
go test ./...
go build ./...
go vet ./...
make lint                 # 0 issues
git diff --check
cmp -s config/crd/bases/supabase.guion.dev_supabaseprojects.yaml \
  charts/cloudnative-supabase/crds/supabase.guion.dev_supabaseprojects.yaml
```

The tests include controller/envtest coverage, complete credential fixtures,
ES256 acceptance and HS256 rejection, public-JWKS projection, least-privilege
pod wiring, Envoy asset parsing/rendering, rotation/watch mapping and Ready
status invalidation, CNPG mutable reconciliation and failure cases,
retained-resource ownership, and same-name readoption. No live cluster, image
deployment, migration, or code-review pass was performed; those activities are
outside this implementation request.

## Initial implementation Changed-LOC variance

Counts are staged diff additions/deletions against the starting `HEAD`, with
generated deepcopy/CRD files and lockfiles excluded as required:

| Category | Additions | Deletions | Line events |
| --- | ---: | ---: | ---: |
| Product code and gateway assets | 1,950 | 1,100 | 3,050 |
| Behavioural tests and fixtures | 606 | 756 | 1,362 |
| Docs and configuration (including this report) | 389 | 468 | 857 |
| **Non-generated total** | **2,945** | **2,324** | **5,269** |

The estimate in the spec is 1,950–3,150 changed LOC. Required deletion of the
Kong/HS256 implementation and its obsolete tests makes the line-event total
higher, while the non-generated additions remain close to the upper end of the
expected implementation size. Product additions are above the ticket estimate
because the complete controller reconciliation, ES256 validation, and managed
Envoy profile are implemented end to end; test additions are lower because old
legacy fixtures were deleted rather than duplicated.

## Remaining concerns

None within the requested implementation boundary. The report intentionally
does not claim a live Envoy smoke test or deployment; both are explicit
out-of-scope follow-up work.

## Consolidated whole-spec review-fix batch

The review-fix commit preserves the pinned upstream Envoy behavior while
limiting the listener to the managed Auth/REST/Meta/Studio profile. It
restores opaque-key admission and query/header translation, ES256 role-token
mapping, genuine-user bearer preservation and synthesis, OAuth routes,
vhost CORS/preflight handling, and service-role-only `/pg/` access.
Legacy JWT-shaped keys and unused service routes remain absent. Envoy admin is
loopback-only and the pod exposes only a public direct-response health route;
the rendered credential-bearing config is kept in a pod-local volume.

The CRD now rejects operator-owned GoTrue JWT/security names while preserving
provider-specific settings, with regenerated config and chart CRD copies.
Recovery validation compares both creation-time bootstrap modes and the
explicit barman external-source identity fields (plugin, ObjectStore, source
server/name) while leaving unknown plugin parameters unfrozen; recovery
credential Secret references remain rotatable, and source ObjectStore identity
cannot be rewritten. CNPG mutable resources converge
on both set and clear, foreign/defaulted fields survive, and durable
adoption/cleanup requires the exact project instance label. A first reconcile
repairs only the matching project owner reference before credential or backup
validation, preserving foreign owners even when validation fails.

Durable owner cleanup now matches the Supabase API group, `SupabaseProject`
kind, and project name while ignoring API version and UID for same-name project
recreation; same-name owners from another API group remain intact. Admission
rejects simultaneously enabled backup/recovery configurations whose destination
paths are equal, while shared credential Secrets remain valid. Meta and
PowerSync no longer carry unused credential-hash parameters or annotations,
and recovery-source comparison uses the exact bootstrap source name so
unrelated external clusters are preserved.

Credential and implementation-secret failures now clear the aggregate Ready
condition before returning, with safe branch-specific SecretsReady reasons;
invalid rotation tests verify all existing workload specs remain unchanged.
Gateway Deployment, Service, Studio URL wiring, and status endpoints share the
single common `<project>-api-gw` naming helper, and GatewaySpec no longer
contains an unimplemented ingress field or type.

Compatibility-only `syncManagedRoles`, variadic credential/hash seams, and the
unused gateway secret-status argument were removed; gateway reconciliation now
uses the shared service-component path while retaining endpoint/status updates.
Credential tests cover malformed JSON, private-key material, key/signature/
role/audience/expiration/prefix/missing-field failures, and rotation tests
prove invalid updates leave workload specs untouched while valid hashes roll
only credential consumers (not Meta or PowerSync).

Review-fix verification completed after commit:

```text
make generate manifests
make test test-tanka test-delivery
go test ./...
go build ./...
go vet ./...
make lint                 # 0 issues
git diff --check
cmp -s config/crd/bases/supabase.guion.dev_supabaseprojects.yaml \
  charts/cloudnative-supabase/crds/supabase.guion.dev_supabaseprojects.yaml
```

All commands passed; the two CRD copies compare byte-for-byte. No deployment or
code-review pass was performed, per scope.

## Final post-fix Changed-LOC counts

The preceding variance table is the original implementation snapshot and
predates the review-fix commits. The final post-fix counts below are measured
with the final implementation commit range `git diff --numstat 9995056..eba0d90 -- . ':!.scratch/modern-supabase-platform/implementation-report.md'`;
they include all implementation and review-fix changes, exclude generated
CRD/deepcopy artifacts and this report, and classify Go files by product code
versus behavioral tests. This keeps the reported implementation delta stable
when the report itself is revised.

| Category | Additions | Deletions | Line events |
| --- | ---: | ---: | ---: |
| Product code and gateway assets | 2,748 | 1,146 | 3,894 |
| Behavioural tests and fixtures | 1,815 | 776 | 2,591 |
| Docs and configuration | 343 | 466 | 809 |
| **Non-generated implementation total** | **4,906** | **2,388** | **7,294** |

The recovery-order fix in `9d90d95` contributes 18 product-code additions, one
deletion, and 44 behavioral-test additions. The final contract-tightening batch
in `993e28d` contributes 32 product-code additions, 21 deletions, 72
behavioral-test additions, five test deletions, and 10 documentation additions.
The final readiness/Ingress/naming batch in `1d029bb` is included in the totals
above; generated artifacts remain synchronized separately and are intentionally
excluded from these LOC totals.

The retained-Cluster ownership ledger in `84f190f` records only sorted custom
parameter keys and additional-role names in one versioned JSON annotation. It
seeds explicit empty arrays, removes only previously tracked declarations,
preserves foreign/defaulted CNPG fields, adopts current declarations when a
pre-ledger Cluster has no annotation, and fails closed with DatabaseReady=False
for malformed or unsupported annotations before any Cluster update. Focused
tests cover creation, parameter and role removal, missing-ledger adoption,
same-name project recreation, malformed JSON, and unsupported versions.

The final standards-triage fixes in `0a7e0f2` and `eba0d90` corrected the
Infisical example with the official 60-second refresh interval, kept recovery
bootstrap validation at the single pre-backup Reconcile guard, and moved
recovery safety/rotation tests to the production reconciliation seams with a
test-owned valid credential fixture.
