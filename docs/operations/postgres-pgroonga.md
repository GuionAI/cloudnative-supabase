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

The smoke fixture runs a networkless, disposable container. It creates only
container-local data, checks Chinese and English OR search, user-scoped RLS
results and snippets, updates and deletes, then a clean restart and physical
backup/WAL recovery. Set `CONTAINER_TOOL=docker` for Docker, and
`PGROONGA_IMAGE` to test a different locally loaded image. Neither command
uses a real database or Kubernetes cluster.

## Project configuration

After publishing the image, select an immutable SHA tag or digest via the
existing project image field. For a single primary, add the two PGroonga
preloads and GUCs:

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

The fixture uses `pg_basebackup -X stream -c fast` while indexed tables are
quiescent. It then commits a new indexed row, flushes, archives its WAL segment,
and restores the backup to that committed LSN. It asserts index scans return
both pre-backup and post-backup matches before any index rebuild.

**The pinned PGroonga 4.0.9 WAL manager requires standby-mode replay.** Its
[redo implementation](https://github.com/pgroonga/pgroonga/blob/4.0.9/src/pgroonga-wal-resource-manager.c#L874)
returns without applying custom records when PostgreSQL's `StandbyMode` is
false. Use `standby.signal` for the validated archive replay path; a plain
`recovery.signal` archive restore can recover PostgreSQL rows while leaving
PGroonga indexes stale. The fixture sets `recovery_target_lsn` and
`recovery_target_action = 'promote'` to exit standby mode after the required
records replay. Merely enabling the WAL-generation GUC does not change the
recovery mode.

Use these settings for each phase, retaining `pg_stat_statements`, `pgaudit`
and `auto_explain` throughout:

| Phase | Additional preload libraries | WAL generation | Crash safer |
| --- | --- | --- | --- |
| Writable primary | `pgroonga_wal_resource_manager`, `pgroonga_crash_safer` | `pgroonga.enable_wal_resource_manager = on` | `pgroonga.enable_crash_safe = on` |
| Standby replay | `pgroonga_wal_resource_manager` | `pgroonga.enable_wal_resource_manager = off` | `pgroonga.enable_crash_safe = off` |
| Promoted writable primary | `pgroonga_wal_resource_manager`, `pgroonga_crash_safer` | `pgroonga.enable_wal_resource_manager = on` | `pgroonga.enable_crash_safe = on` |

PGroonga's module documentation says crash safer must not run on a standby.
After replay and promotion, restore the writable-primary settings and restart
before accepting writes. The fixture asserts both GUCs and all five preloads,
commits another indexed write on the promoted primary, restarts, and verifies
both recovered and subsequent matches through the PGroonga index. No manual
`REINDEX` is part of this recovery procedure.

This fixture validates the image and the explicit standby replay/promotion
sequence. It does not exercise a live CNPG/Barman restore. CNPG v1.28's built-in
archive restore creates `recovery.signal`; selecting this image and these GUCs
does not convert that restore to the validated standby-mode path. Validate and
integrate the required recovery mode before using native archive restore with
PGroonga indexes. The current `SupabaseProject` preload projection also applies
uniformly to CNPG instances and does not distinguish primary and standby
settings. A multi-instance rollout needs a validated role-specific configuration
procedure before deployment.

The work here builds and tests an opt-in image. It does not migrate FlickNote
indexes, switch production to this operator, or complete ChatGPT OAuth.
Cross-repository acceptance requires the `flicknote-cli` PR for spec #2809 to
run its real PG integration suite against this image; neither PR should merge
before that evidence is recorded. The fb OAuth Worker resource and full-grant
changes remain a later delivery unit.

Sources: [PGroonga Debian installation](https://pgroonga.github.io/install/debian.html),
[WAL resource manager replication](https://pgroonga.github.io/reference/streaming-replication-wal-resource-manager.html),
[CNPG PostgreSQL preload configuration](https://cloudnative-pg.io/documentation/current/postgresql_conf/).
