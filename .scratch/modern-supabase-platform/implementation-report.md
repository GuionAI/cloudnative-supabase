# Modern Supabase Platform implementation report

## Run identity

- Feature branch: `modern-supabase-platform`
- Starting `HEAD`: `9995056` (`feat(auth): allow custom GoTrue environment (#18)`)
- Implementation commit: `f3fd3b4` (`feat(platform): implement modern Supabase platform`)
- Review-fix commit: `5492aa6` (`fix(platform): harden modern Supabase reconciliation`)
- Worker pane: `w5P:p6`
- Owner pane: `w5P:p1`
- Scope: the complete `spec.md` and tickets 01–05, in dependency order

## Delivered acceptance criteria

- **01 — ES256 project credentials:** replaced the legacy JWT/secret API with the required five-field external project credential Secret; validation covers the private P-256 ES256 signing JWK, opaque key prefixes, role JWT claims/signatures, missing and malformed fields, and safe status errors. A public-only JWKS is derived for verifiers. GoTrue receives a create-once independent fallback secret, ES256-only methods, normalized issuer, audience, and configurable access-token lifetime. PostgREST, Studio, and PowerSync receive only their permitted material. External Secret watches preserve the last valid workloads on invalid rotation and hash valid rotations into affected pod templates.
- **02 — Envoy opaque gateway:** removed Kong and legacy/hybrid paths, added the gateway-neutral CRD/status/service/deployment contract, pinned Envoy `1.39.0`, GoTrue `2.189.0`, and PostgREST `14.12`, and added attributed Envoy assets from upstream commit `95ca3024398080ff18c9abcd1c6c8beae73fd9e1`. The gateway accepts only configured `sb_publishable_*`/`sb_secret_*` values and translates them to the corresponding internal role JWTs; private signing material and the GoTrue fallback remain excluded.
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

All commands completed successfully after the final Envoy and controller changes:

```text
make generate manifests
make test test-tanka test-delivery
go test ./...
go build ./...
go vet ./...
make lint                 # 0 issues
git diff --cached --check
cmp -s config/crd/bases/supabase.guion.dev_supabaseprojects.yaml \
  charts/cloudnative-supabase/crds/supabase.guion.dev_supabaseprojects.yaml
```

The tests include controller/envtest coverage, complete credential fixtures,
ES256 acceptance and HS256 rejection, public-JWKS projection, least-privilege
pod wiring, Envoy asset parsing/rendering, rotation/watch mapping, CNPG mutable
reconciliation and failure cases, retained-resource ownership, and same-name
readoption. No live cluster, image deployment, migration, or code-review pass
was performed; those activities are outside this implementation request.

## Changed-LOC variance

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
limiting the listener to the managed Auth/REST/GraphQL/Meta/Studio profile. It
restores opaque-key admission and query/header translation, ES256 role-token
mapping, genuine-user bearer preservation and synthesis, GraphQL and OAuth
routes, vhost CORS/preflight handling, and service-role-only `/pg/` access.
Legacy JWT-shaped keys and unused service routes remain absent. Envoy admin is
loopback-only and the pod exposes only a public direct-response health route;
the rendered credential-bearing config is kept in a pod-local volume.

The CRD now rejects operator-owned GoTrue JWT/security names while preserving
provider-specific settings, with regenerated config and chart CRD copies.
Recovery validation compares bootstrap and barman external-source identity
(including plugin parameters such as `serverName`) and prevents source
ObjectStore identity rewrites. CNPG mutable resources converge on both set and
clear, foreign/defaulted fields survive, and durable adoption/cleanup requires
the exact project instance label. A first reconcile repairs only the matching
project owner reference before credential or backup validation, preserving
foreign owners even when validation fails.

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
