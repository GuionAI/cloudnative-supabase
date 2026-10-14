# Opt-in PostgreSQL 18 image with PGroonga

`Dockerfile.postgres-pgroonga` builds a separate CNPG operand image from the
pinned CNPG `18.3-202603230821-standard-trixie` digest. It installs the PGDG
PostgreSQL 18 PGroonga package `4.0.9-1` and its Groonga runtime dependencies.
It preserves the CNPG image's UID 26, PostgreSQL binary path and startup
conventions. It does not change the operator's default PostgreSQL image.
The main and release workflows publish this image separately as
`ghcr.io/guionai/cloudnative-supabase-postgres-pgroonga`, with `latest` and
full commit SHA tags on main, and semantic version tags on releases. PR checks
build and smoke the image without publishing it.

Build and test locally with a container engine:

```bash
podman build --arch amd64 -f Dockerfile.postgres-pgroonga -t cnsupa-postgres-pgroonga:chatgpt-mcp-pg .
hack/test-pgroonga-image.sh
```

The root smoke command preserves the archive fixture and also runs a streaming
fixture. Each runs two PostgreSQL directories/Unix socket ports in one networkless,
disposable container, with no real database or Kubernetes access. Set
`CONTAINER_TOOL=docker` for Docker and `PGROONGA_IMAGE` for a locally loaded image.
Each fixture has a bounded runtime and cleans up only its container-local state.

The exact published image tested for production MCP readiness is PostgreSQL 18.3,
PGroonga 4.0.9 and Groonga 16.1.2:

```bash
PGROONGA_IMAGE=ghcr.io/guionai/cloudnative-supabase-postgres-pgroonga:18.3@sha256:d555bf68fad60626664e22cf90390fc8936b42092d504f7f02a592ec70b92626 \
  hack/test-pgroonga-image.sh
```

On 2026-10-14, this published image passed five complete observable streaming
trials after preparing a valid, quiesced source snapshot: first indexed read,
streamed insert/update/delete, Chinese/English search and owner RLS/snippets,
same-postmaster promotion, immediate writes, controlled crash/restart and clean
restart. A native flush worker was present on the standby, recorded separately
as a lifecycle observation; its presence alone did not establish a correctness
failure. No operator, image, extension module, or topology repair is part of this
readiness work.

## Project configuration

After publishing the image, select an immutable SHA tag or digest via the
existing project image field. The tested uniform configuration adds the two
PGroonga preloads and GUCs on every instance:

```yaml
spec:
  database:
    image: ghcr.io/guionai/cloudnative-supabase-postgres-pgroonga:sha-<full-commit-sha>
    additionalPreloadLibraries:
      - pgroonga_wal_resource_manager
      - pgroonga_crash_safer
    parameters:
      pgroonga.enable_wal_resource_manager: "on"
      pgroonga.enable_crash_safe: "on"
```

`additionalPreloadLibraries` appends to the operator's existing
`pg_stat_statements`, `pgaudit` and `auto_explain` preloads. CNPG manages
`shared_preload_libraries`, so setting that name in `database.parameters`
cannot reliably replace the list. Keep `pgroonga.enable_row_level_security`
at its safe default `on`: PGroonga then limits logs for tables with RLS
policies. Do not set it to `off` for private notes.

The image only provides extension files. A database migration owned by the
application must run `CREATE EXTENSION pgroonga`, grant schema usage as needed,
and create business indexes. `additionalExtensions` does not install or
create extensions. Use the same image on every node and on recovery targets
before starting WAL replay. Apply the project image and preload settings
before enabling PGroonga indexes; changing preload libraries requires a
PostgreSQL restart.

## Physical backup and recovery

PGroonga keeps Groonga files alongside PostgreSQL data. Follow its documented
base-backup procedure: quiesce writes to indexed tables, run
`SELECT pgroonga_command('io_flush')`, and keep writes stopped until
`pg_basebackup` finishes. All source, standby and promoted nodes must use the
same image and extension/runtime versions.

The streaming fixture uses `pg_basebackup -X stream -c fast -R` with all five
preloads and both PGroonga GUCs `on` on both nodes. After its deliberate source
restart, it matches native crash-safer READY to a live flush child of the current
source postmaster before `io_flush` and base backup. SQL readiness alone does
not establish that native startup recovery has stopped changing Groonga files.

A diagnostic syscall trace identified the source crash-safer creating
`pgrn.000010D.c` during native index recovery while backup startup overlapped its
preparation. That traced run passed. Attribution of an earlier untraced candidate
missing-file error to this overlap remains an inference. The initial raw fixture
failed five of five trials, but correctly prepared snapshots passed search-only
checks on both published and candidate images. All those outcomes are retained
with spec #3922; raw failures are not evidence that the published image fails the
correctly prepared observable contract. A candidate runtime patch was evaluated
and left out of delivery because the final baseline did not demonstrate a need.

The streaming assertions inspect actual index plans and results under each owner,
check stale insert/update/delete matches, and preserve postmaster PID/start time
across `pg_promote`. The first primary write follows promotion immediately.
Native READY plus live flush-child/parent identity establish that crash-safer is
ready on the promoted primary; indexed data and owner isolation are checked after
both an immediate crash/restart and a clean restart. The child may already be
active before promotion. Source readiness and replay/worker polling never defer
the first standby indexed read or the immediate promoted write.

The original archive fixture remains unchanged, including its reduced standby
settings and explicit restart into primary settings. Its coverage complements
the uniform-settings streaming fixture. The
[upstream streaming guide](https://pgroonga.github.io/reference/streaming-replication-wal-resource-manager.html)
omits crash-safer on a standby; that guidance is context, not proof that uniform
settings fail the behavior tested here. The operator continues to project uniform
settings, and the existing two-node topology is unchanged.

### Archive recovery and derived indexes

PGroonga's pinned
[redo implementation](https://github.com/pgroonga/pgroonga/blob/4.0.9/src/pgroonga-wal-resource-manager.c#L874)
skips custom WAL when PostgreSQL's `StandbyMode` is false. Retained exact-image
PostgreSQL evidence referenced by spec #3922 from fse (`native-restore-reindex-fixture.sh`,
lines 64–119, and `native-restore-reindex.log`) demonstrated `recovery.signal`
restoring a post-backup row while forced PGroonga index search missed it.
Targeted `REINDEX INDEX notes_body_pgroonga` restored the match and owner
isolation. Promoted writes and a later clean restart passed after restoring
primary settings. That fixture used reduced preloads/GUCs during archive replay
and an explicit restart into primary settings; it proves neither uniform-settings
promotion nor unattended Barman recovery. It is reused evidence, not a duplicate
test in this repository.

For a recovered application database, keep search traffic closed until derived
indexes have been rebuilt and checked. Once PostgreSQL has completed recovery
and is writable, run targeted `REINDEX INDEX <schema>.<index>` for each affected
PGroonga index as an authorized database operator. Verify restored rows separately
from forced indexed Chinese/English matches, absence of stale update/delete
matches, and owner RLS results/snippets under each application role before
reopening traffic. A healthy PostgreSQL row count alone is insufficient.
Rebuild only derived indexes; this is not a replacement for verifying the restored
application data. Do not run `REINDEX` while the server is still in recovery.

The disposable fixture exercises image/PostgreSQL behavior only. It does not
exercise CNPG controller election, fencing, service routing, or automatic
failover, nor actual CNPG/Barman restores. Quiesced `io_flush`/basebackup evidence
does not certify continuously writable online backups. Merged fse spec #3920 /
PR174 configures production MCP/Web, but this readiness work neither deploys it
nor creates any production indexes, roles, credentials, or databases.

Sources: [PGroonga Debian installation](https://pgroonga.github.io/install/debian.html),
[WAL resource manager replication](https://pgroonga.github.io/reference/streaming-replication-wal-resource-manager.html),
[CNPG PostgreSQL preload configuration](https://cloudnative-pg.io/documentation/current/postgresql_conf/).
